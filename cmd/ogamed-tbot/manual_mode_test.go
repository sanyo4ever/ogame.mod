package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alaingilbert/ogame/pkg/device"
	"github.com/alaingilbert/ogame/pkg/wrapper"
	"github.com/labstack/echo/v4"
	"github.com/urfave/cli/v3"
)

func TestManualModeTimeoutFlag(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        int64
		invalid     bool
	}{
		{"default", "", 120, false},
		{"custom", "300", 300, false},
		{"one second", "1", 1, false},
		{"largest supported", "2147483647", 2147483647, false},
		{"zero", "0", 0, true},
		{"negative", "-1", 0, true},
		{"overflow", "2147483648", 0, true},
		{"fraction", "1.5", 0, true},
		{"text", "two minutes", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			app := &cli.Command{Flags: []cli.Flag{manualModeTimeoutFlag()}, Writer: io.Discard, ErrWriter: io.Discard,
				Action: func(_ context.Context, c *cli.Command) error {
					called = true
					if got := c.Int("manual-mode-timeout-seconds"); got != tc.want {
						t.Errorf("timeout=%d, want %d", got, tc.want)
					}
					return nil
				}}
			args := []string{"ogamed"}
			if tc.value != "" {
				args = append(args, "--manual-mode-timeout-seconds="+tc.value)
			}
			err := app.Run(context.Background(), args)
			if (err != nil) != tc.invalid || called == tc.invalid {
				t.Fatalf("err=%v action called=%v", err, called)
			}
		})
	}
}

func TestManualModeTimeoutEnvironment(t *testing.T) {
	for _, value := range []string{"240", "0"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("OGAMED_MANUAL_MODE_TIMEOUT_SECONDS", value)
			called := false
			app := &cli.Command{Flags: []cli.Flag{manualModeTimeoutFlag()}, Writer: io.Discard, ErrWriter: io.Discard,
				Action: func(_ context.Context, c *cli.Command) error {
					called = true
					if c.Int("manual-mode-timeout-seconds") != 240 {
						t.Fatal("environment timeout not loaded")
					}
					return nil
				}}
			err := app.Run(context.Background(), []string{"ogamed"})
			if value == "240" && (err != nil || !called) {
				t.Fatalf("valid timeout rejected: %v", err)
			}
			if value == "0" && (err == nil || called) {
				t.Fatal("invalid environment timeout was accepted")
			}
		})
	}
}

type fakeManualTimer struct {
	delay    time.Duration
	callback func()
	stopped  bool
}

func (f *fakeManualTimer) Stop() bool { wasRunning := !f.stopped; f.stopped = true; return wasRunning }

// Embedding keeps all unrelated game operations unavailable. Only proxy reads,
// posts and transaction references below are implemented; no network is used.
type fakeManualTx struct {
	wrapper.Prioritizable
	refs, releases, gets, posts int
	query, payload              url.Values
	duringRequest               func()
}

func (f *fakeManualTx) Begin() wrapper.Prioritizable { f.refs++; return f }
func (f *fakeManualTx) Done() {
	f.refs--
	if f.refs == 0 {
		f.releases++
	}
	if f.refs < 0 {
		panic("transaction released twice")
	}
}
func (f *fakeManualTx) GetPageContent(q url.Values) ([]byte, error) {
	f.gets++
	f.query = q
	if f.duringRequest != nil {
		f.duringRequest()
	}
	return []byte("<body>fixture</body>"), nil
}
func (f *fakeManualTx) PostPageContent(q, p url.Values) ([]byte, error) {
	f.posts++
	f.query, f.payload = q, p
	if f.duringRequest != nil {
		f.duringRequest()
	}
	return []byte("<body>fixture</body>"), nil
}

func manualFixture(t *testing.T, timeout time.Duration) (*manualMode, *[]*fakeManualTimer, *[]*fakeManualTx) {
	t.Helper()
	timers := []*fakeManualTimer{}
	txs := []*fakeManualTx{}
	m := newManualMode(timeout, func() wrapper.Prioritizable {
		tx := &fakeManualTx{refs: 1}
		txs = append(txs, tx)
		return tx
	})
	m.afterFunc = func(d time.Duration, cb func()) manualModeTimer {
		timer := &fakeManualTimer{delay: d, callback: cb}
		timers = append(timers, timer)
		return timer
	}
	return m, &timers, &txs
}

func TestManualModeExpiryAndManualOff(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(fmt.Sprintf("expire=%v", expire), func(t *testing.T) {
			m, timers, txs := manualFixture(t, 2*time.Minute)
			if !m.Toggle() {
				t.Fatal("manual mode did not start")
			}
			if (*timers)[0].delay != 2*time.Minute {
				t.Fatal("wrong initial deadline")
			}
			if expire {
				(*timers)[0].callback()
			} else if m.Toggle() {
				t.Fatal("manual mode did not stop")
			}
			(*timers)[0].callback() // A callback already queued before Stop must be harmless.
			if (*txs)[0].refs != 0 || (*txs)[0].releases != 1 {
				t.Fatal("session was not released exactly once")
			}
			if tx, _ := m.Acquire(); tx != nil {
				t.Fatal("expired/off session is still available")
			}
		})
	}
}

func TestManualModeRefreshAndNewSessionIgnoreStaleExpiry(t *testing.T) {
	m, timers, txs := manualFixture(t, 5*time.Minute)
	m.Toggle()
	tx, release := m.Acquire()
	if tx == nil || (*timers)[1].delay != 5*time.Minute {
		t.Fatal("request did not refresh custom deadline")
	}
	(*timers)[0].callback()
	if (*txs)[0].refs != 2 {
		t.Fatal("stale pre-refresh timeout released session")
	}
	release()
	m.Toggle()
	m.Toggle()
	(*timers)[1].callback()
	if (*txs)[1].refs != 1 || (*txs)[1].releases != 0 {
		t.Fatal("old session timeout released new session")
	}
	(*timers)[2].callback()
	if (*txs)[1].releases != 1 {
		t.Fatal("new session did not expire")
	}
}

func TestManualModeTimerStartsAfterTransactionAcquired(t *testing.T) {
	m, timers, _ := manualFixture(t, 2*time.Minute)
	m.begin = func() wrapper.Prioritizable {
		if len(*timers) != 0 {
			t.Fatal("queue wait consumed manual timeout")
		}
		return &fakeManualTx{refs: 1}
	}
	m.Toggle()
	if len(*timers) != 1 {
		t.Fatal("timer not started after acquisition")
	}
	m.Toggle()
}

func TestManualModeRequestSurvivesExpiry(t *testing.T) {
	m, timers, txs := manualFixture(t, time.Minute)
	m.Toggle()
	_, release := m.Acquire()
	(*timers)[1].callback()
	if (*txs)[0].refs != 1 || (*txs)[0].releases != 0 {
		t.Fatal("expiry released in-flight request")
	}
	release()
	if (*txs)[0].releases != 1 {
		t.Fatal("request completion did not release session")
	}
}

func TestManualModeRealBotQueueWaitsForInFlightRequest(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(fmt.Sprintf("expire=%v", expire), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bot, err := wrapper.NewWithParams(wrapper.Params{Ctx: ctx, Device: &device.Device{}, Quiet: true})
			if err != nil {
				t.Fatal(err)
			}
			m := newManualMode(2*time.Minute, func() wrapper.Prioritizable { return bot.BeginNamed("manual mode") })
			var timer *fakeManualTimer
			m.afterFunc = func(d time.Duration, cb func()) manualModeTimer {
				timer = &fakeManualTimer{delay: d, callback: cb}
				return timer
			}
			if tx, _ := m.Acquire(); tx != nil {
				t.Fatal("inactive manual mode intercepted a request")
			}
			m.Toggle()
			_, release := m.Acquire()
			completed := make(chan struct{})
			go func() {
				tx := bot.BeginNamed("automated fixture")
				tx.Done()
				close(completed)
			}()
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			for bot.GetTasks().Total == 0 {
				select {
				case <-completed:
					t.Fatal("automated command bypassed manual mode")
				case <-deadline.C:
					t.Fatal("automated command was not queued")
				case <-tick.C:
				}
			}
			if expire {
				timer.callback()
			} else {
				m.Toggle()
			}
			select {
			case <-completed:
				t.Fatal("automated command bypassed in-flight manual request")
			default:
			}
			release()
			select {
			case <-completed:
			case <-deadline.C:
				t.Fatal("automated command did not resume after manual request completed")
			}
		})
	}
}

func TestManualModeProxyHandlersRefreshTimeout(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			m, timers, txs := manualFixture(t, 7*time.Minute)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bot, err := wrapper.NewWithParams(wrapper.Params{Ctx: ctx, Device: &device.Device{}, Quiet: true})
			if err != nil {
				t.Fatal(err)
			}
			e := echo.New()
			toggle := e.NewContext(httptest.NewRequest(http.MethodPost, "/bot/toggle-manual-mode", nil), httptest.NewRecorder())
			toggle.Set("bot", bot)
			toggle.Set("manualMode", m)
			if err := PostToggleManualModeHandler(toggle); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(method, "/game/index.php?page=ingame&component=overview&cp=42", strings.NewReader("fixture=value"))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.Set("bot", bot)
			c.Set("manualMode", m)
			if method == http.MethodGet {
				err = GetFromGameHandler(c)
			} else {
				err = PostToGameHandler(c)
			}
			if err != nil || rec.Code != http.StatusOK {
				t.Fatalf("response=%d err=%v", rec.Code, err)
			}
			if len(*timers) != 2 || (*timers)[1].delay != 7*time.Minute {
				t.Fatal("proxy request did not use configured timeout")
			}
			tx := (*txs)[0]
			if tx.gets+tx.posts != 1 || tx.query.Get("cp") != "42" {
				t.Fatal("proxy request lost origin or did not use session")
			}
			if method == http.MethodPost && tx.payload.Get("fixture") != "value" {
				t.Fatal("POST payload was changed")
			}
			if tx.refs != 1 {
				t.Fatal("proxy request leaked transaction reference")
			}
			if err := PostToggleManualModeHandler(toggle); err != nil {
				t.Fatal(err)
			}
			(*timers)[1].callback()
			if tx.releases != 1 {
				t.Fatal("manual off did not release once")
			}
		})
	}
}

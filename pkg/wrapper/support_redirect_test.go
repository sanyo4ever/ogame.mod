package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alaingilbert/ogame/pkg/device"
	"github.com/alaingilbert/ogame/pkg/gameforge"
	"github.com/alaingilbert/ogame/pkg/httpclient"
)

type supportRedirectTransport func(*http.Request) (*http.Response, error)

func (f supportRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type supportRedirectFixture struct {
	bot        *OGame
	requests   atomic.Int32
	loginCalls atomic.Int32
	logs       bytes.Buffer
}

// Both normal and login-proxy transports are fake. Even an unexpected redirect
// or login attempt cannot access a real server, account, or credentials.
func newSupportRedirectFixture(t *testing.T, status int, location, body string) *supportRedirectFixture {
	t.Helper()
	// Negative cases may invoke the diagnostic HTML writer. Confine any output
	// to a temporary test directory rather than the user's actual home.
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	fixture := &supportRedirectFixture{}
	transport := supportRedirectTransport(func(req *http.Request) (*http.Response, error) {
		fixture.requests.Add(1)
		if req.URL.Host != "support-game.invalid" || req.URL.Path != "/game/index.php" {
			return nil, fmt.Errorf("unexpected fake transport destination: %s", req.URL.Host)
		}
		responseStatus, responseBody := status, body
		header := make(http.Header)
		if req.URL.Query().Get("component") == "queueProbe" {
			responseStatus, responseBody = http.StatusOK, "queue available"
		} else if location != "" {
			header.Set("Location", location)
		}
		return &http.Response{
			StatusCode: responseStatus,
			Status:     fmt.Sprintf("%d %s", responseStatus, http.StatusText(responseStatus)),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Request:    req,
		}, nil
	})
	dev := &device.Device{}
	dev.SetClient(&httpclient.Client{Client: &http.Client{
		Transport: transport,
		Timeout:   time.Second,
		// Also expose redirects from non-support requests to the wrapper so
		// negative route tests check its classification, never a remote page.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	bot, err := NewWithParams(Params{Device: dev, Ctx: ctx, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	fixture.bot = bot
	bot.SetLogger(log.New(&fixture.logs, "", 0))
	bot.loginProxyTransport = transport
	bot.cache.serverURL = "https://support-game.invalid"
	bot.cache.serverData.Version = "13.0.2"
	bot.isLoggedInAtom.Store(true)
	bot.isConnectedAtom.Store(true)
	bot.loginWrapper = func(LoginFn) error {
		fixture.loginCalls.Add(1)
		// A terminal fake error prevents backoff or any real authentication.
		return gameforge.ErrBadCredentials
	}
	return fixture
}

func supportPageValues() url.Values {
	return url.Values{"page": {"ingame"}, "component": {"support"}}
}

func requireSupportNavigationTarget(t *testing.T, body []byte, target string) {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	scripts := doc.Find("script")
	if scripts.Length() != 1 {
		t.Fatalf("expected exactly one navigation script, got %d", scripts.Length())
	}
	// Accept either a location assignment or location.replace; assert the
	// actual JavaScript string value, not its choice of escaping or formatting.
	pattern := regexp.MustCompile(`(?:location(?:\.href)?\s*=\s*|location\.replace\(\s*)("(?:\\.|[^"\\])*")`)
	match := pattern.FindStringSubmatch(scripts.Text())
	if len(match) != 2 {
		t.Fatalf("missing JSON-quoted browser navigation in script: %s", scripts.Text())
	}
	var actual string
	if err := json.Unmarshal([]byte(match[1]), &actual); err != nil {
		t.Fatalf("invalid navigation string: %v", err)
	}
	if actual != target {
		t.Fatalf("redirect target changed: got %q, want %q", actual, target)
	}
}

func requireSupportQueueAvailable(t *testing.T, fixture *supportRedirectFixture) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		body, err := fixture.bot.GetPageContent(url.Values{"page": {"ajax"}, "component": {"queueProbe"}})
		if err == nil && string(body) != "queue available" {
			err = fmt.Errorf("unexpected queue probe response %q", body)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("queued read failed after support response: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("support response left the bot queue blocked")
	}
}

func TestSupportRedirectNavigatesWithoutRelogin(t *testing.T) {
	base := "https://ogame.support.gameforge.com/index.php?fld=en&sso=login&key=fixture-only&origin=s999-en.ogame.gameforge.com"
	for _, tc := range []struct {
		name     string
		status   int
		location string
	}{
		{"302", http.StatusFound, base},
		{"303", http.StatusSeeOther, base},
		{"explicit HTTPS port", http.StatusFound, strings.Replace(base, ".com/index", ".com:443/index", 1)},
		{"case insensitive host", http.StatusFound, strings.Replace(base, "ogame.support.gameforge.com", "OGAME.SUPPORT.GAMEFORGE.COM", 1)},
		{"script escaping and exact query", http.StatusFound, base + "&note=\"quoted\"</script><script>alert(1)</script>&slash=\\&line=\u2028\u2029"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newSupportRedirectFixture(t, tc.status, tc.location, "synthetic redirect body")
			body, err := fixture.bot.GetPageContent(supportPageValues())
			if err != nil {
				t.Fatalf("support navigation failed: %v (requests=%d, login calls=%d)", err, fixture.requests.Load(), fixture.loginCalls.Load())
			}
			requireSupportNavigationTarget(t, body, tc.location)
			if strings.Contains(fixture.logs.String(), "bad status") || strings.Contains(fixture.logs.String(), "fixture-only") {
				t.Fatal("accepted support redirect logged an error or disclosed the SSO key")
			}
			if fixture.requests.Load() != 1 || fixture.loginCalls.Load() != 0 {
				t.Fatalf("support requested/relogged unexpectedly: requests=%d logins=%d", fixture.requests.Load(), fixture.loginCalls.Load())
			}
			if !fixture.bot.isConnectedAtom.Load() || !fixture.bot.isLoggedInAtom.Load() {
				t.Fatal("support navigation changed the authenticated session state")
			}
			requireSupportQueueAvailable(t, fixture)
		})
	}
}

func TestSupportRedirectLegacyScriptResponseUnchanged(t *testing.T) {
	body := `<script>document.location.href='https://ogame.support.gameforge.com/index.php?fld=en&sso=login&key=fixture-only'</script>`
	fixture := newSupportRedirectFixture(t, http.StatusOK, "", body)
	actual, err := fixture.bot.GetPageContent(supportPageValues())
	if err != nil || string(actual) != body {
		t.Fatalf("legacy support response changed: body=%q err=%v", actual, err)
	}
	if fixture.requests.Load() != 1 || fixture.loginCalls.Load() != 0 || !fixture.bot.isConnectedAtom.Load() {
		t.Fatal("legacy support response retried or changed session state")
	}
	requireSupportQueueAvailable(t, fixture)
}

func TestSupportRedirectManualTransaction(t *testing.T) {
	target := "https://ogame.support.gameforge.com/index.php?fld=en&sso=login&key=fixture-only"
	fixture := newSupportRedirectFixture(t, http.StatusFound, target, "synthetic redirect body")
	var body []byte
	var err error
	func() {
		tx := fixture.bot.BeginNamed("manual mode")
		defer tx.Done()
		body, err = tx.GetPageContent(supportPageValues())
	}()
	if err != nil {
		t.Fatalf("manual-mode support navigation failed: %v", err)
	}
	requireSupportNavigationTarget(t, body, target)
	if fixture.requests.Load() != 1 || fixture.loginCalls.Load() != 0 || !fixture.bot.isConnectedAtom.Load() {
		t.Fatal("manual-mode support response retried or changed session state")
	}
	requireSupportQueueAvailable(t, fixture)
}

func TestSupportRedirectDoesNotRelaxSessionRedirects(t *testing.T) {
	valid := "https://ogame.support.gameforge.com/index.php?fld=en&sso=login&key=fixture-only"
	for _, tc := range []struct {
		name, method, page, component, location string
		status                                  int
	}{
		{"lobby login", "GET", "ingame", "support", "https://lobby.gameforge.com/login", http.StatusFound},
		{"other host", "GET", "ingame", "support", "https://other.invalid/index.php", http.StatusFound},
		{"host suffix", "GET", "ingame", "support", "https://ogame.support.gameforge.com.other.invalid/index.php", http.StatusFound},
		{"userinfo", "GET", "ingame", "support", "https://unexpected@ogame.support.gameforge.com/index.php", http.StatusFound},
		{"HTTP", "GET", "ingame", "support", "http://ogame.support.gameforge.com/index.php", http.StatusFound},
		{"other port", "GET", "ingame", "support", "https://ogame.support.gameforge.com:444/index.php", http.StatusFound},
		{"other path", "GET", "ingame", "support", "https://ogame.support.gameforge.com/login", http.StatusFound},
		{"scheme relative", "GET", "ingame", "support", "//ogame.support.gameforge.com/index.php", http.StatusFound},
		{"missing location", "GET", "ingame", "support", "", http.StatusFound},
		{"POST support", "POST", "ingame", "support", valid, http.StatusFound},
		{"different component", "GET", "ingame", "overview", valid, http.StatusFound},
		{"different page", "GET", "componentOnly", "support", valid, http.StatusFound},
		{"301", "GET", "ingame", "support", valid, http.StatusMovedPermanently},
		{"307", "GET", "ingame", "support", valid, http.StatusTemporaryRedirect},
		{"308", "GET", "ingame", "support", valid, http.StatusPermanentRedirect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newSupportRedirectFixture(t, tc.status, tc.location, "synthetic rejected redirect")
			vals := url.Values{"page": {tc.page}, "component": {tc.component}}
			var body []byte
			var err error
			if tc.method == http.MethodPost {
				body, err = fixture.bot.PostPageContent(vals, nil)
			} else {
				body, err = fixture.bot.GetPageContent(vals)
			}
			// The fake login's terminal error proves the original session-expiry
			// recovery path was retained, while avoiding real login or retries.
			if !errors.Is(err, gameforge.ErrBadCredentials) || len(body) != 0 {
				t.Fatalf("unexpected redirect accepted or misclassified: body=%q err=%v", body, err)
			}
			if fixture.requests.Load() != 1 || fixture.loginCalls.Load() != 1 || fixture.bot.isConnectedAtom.Load() {
				t.Fatalf("session recovery changed: requests=%d logins=%d connected=%v", fixture.requests.Load(), fixture.loginCalls.Load(), fixture.bot.isConnectedAtom.Load())
			}
		})
	}
}

func TestSupportRedirectRejectsMalformedLocation(t *testing.T) {
	// net/http rejects these before returning a response to the wrapper. Exercise
	// the adapter's own boundary directly as well, without testing HTTP retry timing.
	for _, location := range []string{
		"https://ogame.support.gameforge.com:bad/index.php",
		"https://ogame.support.gameforge.com/index.php\r\nX-Test: invalid",
	} {
		t.Run(location, func(t *testing.T) {
			if isSupportRedirect(http.MethodGet, supportPageValues(), requestResponse{http.StatusFound, location}) {
				t.Fatal("malformed redirect was accepted for browser navigation")
			}
		})
	}
}

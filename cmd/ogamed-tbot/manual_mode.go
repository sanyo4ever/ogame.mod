package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/alaingilbert/ogame/pkg/wrapper"
	"github.com/urfave/cli/v3"
)

const defaultManualModeTimeoutSeconds int64 = 120

func manualModeTimeoutFlag() *cli.IntFlag {
	return &cli.IntFlag{
		Name:    "manual-mode-timeout-seconds",
		Usage:   "Manual proxy mode inactivity timeout in seconds (1..2147483647)",
		Value:   defaultManualModeTimeoutSeconds,
		Sources: cli.EnvVars("OGAMED_MANUAL_MODE_TIMEOUT_SECONDS"),
		Validator: func(seconds int64) error {
			if seconds < 1 || seconds > 2147483647 {
				return fmt.Errorf("manual-mode-timeout-seconds must be between 1 and 2147483647")
			}
			return nil
		},
	}
}

type manualModeTimer interface{ Stop() bool }

type manualSession struct {
	tx         wrapper.Prioritizable
	timer      manualModeTimer
	generation uint64
}

// manualMode owns the proxy transaction and its inactivity deadline. A timer
// belongs to one session/revision, so an already queued callback cannot end a
// refreshed or subsequently opened manual session.
type manualMode struct {
	mu        sync.Mutex
	timeout   time.Duration
	begin     func() wrapper.Prioritizable
	afterFunc func(time.Duration, func()) manualModeTimer
	active    *manualSession
}

func newManualMode(timeout time.Duration, begin func() wrapper.Prioritizable) *manualMode {
	return &manualMode{timeout: timeout, begin: begin, afterFunc: func(d time.Duration, f func()) manualModeTimer { return time.AfterFunc(d, f) }}
}

func (m *manualMode) Toggle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		m.finishLocked(m.active)
		return false
	}
	// Acquiring the queued bot transaction may take time. Start the inactivity
	// clock only once manual control has actually been acquired.
	m.active = &manualSession{tx: m.begin()}
	m.refreshLocked(m.active)
	return true
}

// Acquire renews the inactivity timeout for a game GET/POST and reserves a
// nested transaction reference until that request completes. Expiry or manual
// off may end the session meanwhile, but cannot unlock an in-flight request.
func (m *manualMode) Acquire() (wrapper.Prioritizable, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.active
	if session == nil {
		return nil, nil
	}
	tx := session.tx.Begin()
	m.refreshLocked(session)
	return tx, tx.Done
}

func (m *manualMode) refreshLocked(session *manualSession) {
	if session.timer != nil {
		session.timer.Stop()
	}
	session.generation++
	generation := session.generation
	session.timer = m.afterFunc(m.timeout, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.active == session && session.generation == generation {
			m.finishLocked(session)
			log.Println("Manual Mode Expired!")
		}
	})
}

func (m *manualMode) finishLocked(session *manualSession) {
	session.timer.Stop()
	m.active = nil
	session.tx.Done()
}

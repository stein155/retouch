package display

// The firmware's standby clock paints over anything we put on the panel. The
// per-tick rewrite in step() repairs that within a second, but a truly
// flicker-free screen needs the clock off while our content is up. The
// firmware exposes GET/POST /clockDisplay with a clockConfig envelope; we
// read the current config, flip userEnable to false while content is shown,
// and post the original config back the moment nothing of ours is visible —
// so the clock keeps working normally whenever there is no message. All of
// it is best-effort: on firmwares without the endpoint nothing changes and
// the rewrite fallback still applies.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var clockHTTP = &http.Client{Timeout: 4 * time.Second}

// suppressClock turns the standby clock off while we own the panel. Only
// acts when the clock is currently user-enabled; remembers the original
// config for restoreClock. Caller must not hold m.mu.
func (m *Manager) suppressClock(ctx context.Context) {
	if m.speaker == "" {
		return
	}
	m.mu.Lock()
	already := m.clockSaved != ""
	m.mu.Unlock()
	if already {
		return
	}
	orig, err := m.clockGet(ctx)
	if err != nil || !strings.Contains(orig, `userEnable="true"`) {
		return // no endpoint, no clock, or already off by user choice
	}
	off := strings.Replace(orig, `userEnable="true"`, `userEnable="false"`, 1)
	if err := m.clockPost(ctx, off); err != nil {
		m.log.Warn("clock suppress failed", "err", err)
		return
	}
	m.mu.Lock()
	m.clockSaved = orig
	m.mu.Unlock()
}

// restoreClock re-enables the clock exactly as it was before suppressClock.
func (m *Manager) restoreClock(ctx context.Context) {
	m.mu.Lock()
	orig := m.clockSaved
	m.clockSaved = ""
	m.mu.Unlock()
	if orig == "" {
		return
	}
	if err := m.clockPost(ctx, orig); err != nil {
		m.log.Warn("clock restore failed", "err", err)
		// keep it cleared: retrying forever with a broken endpoint helps nobody
	}
}

func (m *Manager) clockGet(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+m.speaker+"/clockDisplay", nil)
	resp, err := clockHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errStatus(resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return string(b), err
}

func (m *Manager) clockPost(ctx context.Context, body string) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+m.speaker+"/clockDisplay", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/xml")
	resp, err := clockHTTP.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errStatus(resp.StatusCode)
	}
	return nil
}

type errStatus int

func (e errStatus) Error() string { return fmt.Sprintf("status %d", int(e)) }

package web_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fwRecorder is a fake telnet firewall that records the applied states.
type fwRecorder struct {
	mu    sync.Mutex
	calls []bool
	err   error
}

func (f *fwRecorder) apply(closed bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, closed)
	return nil
}

func (f *fwRecorder) last() (bool, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return false, 0
	}
	return f.calls[len(f.calls)-1], len(f.calls)
}

// The toggle must apply the firewall change immediately, in both directions,
// and keep the marker file in sync so the choice survives a reboot.
func TestCloseTelnetImmediate(t *testing.T) {
	srv, _, dir := newServerSrv(t)
	fw := &fwRecorder{}
	srv.SetTelnetFirewall(fw.apply)
	h := srv.Handler()
	marker := filepath.Join(dir, ".close-telnet")

	rec := do(t, h, "PUT", "/api/settings", `{"closeTelnet":true}`)
	if rec.Code != 200 {
		t.Fatalf("PUT closeTelnet on: %d (%s)", rec.Code, rec.Body)
	}
	if last, n := fw.last(); n != 1 || !last {
		t.Fatalf("firewall calls after enable = %v", fw.calls)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker missing after enable: %v", err)
	}

	rec = do(t, h, "PUT", "/api/settings", `{"closeTelnet":false}`)
	if rec.Code != 200 {
		t.Fatalf("PUT closeTelnet off: %d (%s)", rec.Code, rec.Body)
	}
	if last, n := fw.last(); n != 2 || last {
		t.Fatalf("firewall calls after disable = %v", fw.calls)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker still present after disable: %v", err)
	}
}

// A firewall failure must surface as an error response so the UI reverts the
// toggle instead of reporting a close that never happened.
func TestCloseTelnetFirewallError(t *testing.T) {
	srv, _, _ := newServerSrv(t)
	srv.SetTelnetFirewall((&fwRecorder{err: errors.New("boom")}).apply)
	h := srv.Handler()

	rec := do(t, h, "PUT", "/api/settings", `{"closeTelnet":true}`)
	if rec.Code != 502 {
		t.Fatalf("PUT closeTelnet with failing firewall: %d, want 502", rec.Code)
	}
}

// With the marker already on disk (set before a reboot), Run must re-apply the
// block right away — there is no grace window anymore.
func TestCloseTelnetAppliedAtStartup(t *testing.T) {
	srv, _, dir := newServerSrv(t)
	fw := &fwRecorder{}
	srv.SetTelnetFirewall(fw.apply)
	if err := os.WriteFile(filepath.Join(dir, ".close-telnet"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if last, n := fw.last(); n == 1 && last {
			return
		}
		if time.Now().After(deadline) {
			last, n := fw.last()
			t.Fatalf("firewall not applied at startup; last=%v n=%d", last, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

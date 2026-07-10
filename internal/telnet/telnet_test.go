package telnet

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func newGuard(t *testing.T) (*Guard, *[]bool, string) {
	t.Helper()
	dir := t.TempDir()
	g := New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var calls []bool
	g.SetApplier(func(closed bool) error { calls = append(calls, closed); return nil })
	return g, &calls, filepath.Join(dir, ".close-telnet")
}

func TestSetPersistsAndApplies(t *testing.T) {
	g, calls, marker := newGuard(t)

	if err := g.Set(true); err != nil {
		t.Fatal(err)
	}
	if !g.IsClosed() {
		t.Error("IsClosed false after close")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker missing after close: %v", err)
	}

	if err := g.Set(false); err != nil {
		t.Fatal(err)
	}
	if g.IsClosed() {
		t.Error("IsClosed true after open")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("marker present after open: %v", err)
	}

	want := []bool{true, false}
	if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
		t.Errorf("applier calls = %v, want %v", *calls, want)
	}
}

func TestApplyAtStartup(t *testing.T) {
	g, calls, marker := newGuard(t)

	// No marker: startup is a no-op, firewall untouched.
	if err := g.ApplyAtStartup(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("applier called with no marker: %v", *calls)
	}

	// Marker present (survived a reboot): startup re-applies the block.
	if err := os.WriteFile(marker, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.ApplyAtStartup(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != true {
		t.Fatalf("startup applier calls = %v, want [true]", *calls)
	}
}

func TestCloseFirewallErrorPropagates(t *testing.T) {
	g, _, _ := newGuard(t)
	g.SetApplier(func(bool) error { return errors.New("boom") })
	if err := g.Set(true); err == nil {
		t.Fatal("Set(true) with failing applier returned nil, want error")
	}
}

// A failed firewall apply must NOT leave the marker on disk: IsClosed() would
// then report the port blocked while the DROP rule was never installed.
func TestCloseFirewallFailureDoesNotPersist(t *testing.T) {
	g, _, marker := newGuard(t)
	g.SetApplier(func(bool) error { return errors.New("boom") })
	if err := g.Set(true); err == nil {
		t.Fatal("Set(true) returned nil, want error")
	}
	if g.IsClosed() {
		t.Error("IsClosed() true after the firewall apply failed")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("marker written despite firewall failure: %v", err)
	}
}

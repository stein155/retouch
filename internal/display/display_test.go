package display

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stein155/retouch/oled"
)

func newTestManager(t *testing.T, standby bool) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fb0")
	orig := bytes.Repeat([]byte{7}, oled.Width*oled.Height)
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), path, func(context.Context) bool { return standby },
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return m, path
}

func TestUnavailableIsNoop(t *testing.T) {
	m := New(context.Background(), filepath.Join(t.TempDir(), "missing"), nil,
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if m.Available() {
		t.Fatal("missing fb reported available")
	}
	m.Notify(Content{Text: "x"}, time.Second) // must not panic
	var nilM *Manager
	if nilM.Available() {
		t.Fatal("nil manager available")
	}
	nilM.Notify(Content{}, 0)
	nilM.SetStandby("x", Content{})
	nilM.ClearStandby("x")
}

func TestStandbyShownAndRestored(t *testing.T) {
	m, path := newTestManager(t, true)
	m.SetStandby("afvalwijzer", Content{Icon: "groen", Text: "morgen groen"})
	m.step(context.Background())
	after, _ := os.ReadFile(path)
	if bytes.Equal(after, bytes.Repeat([]byte{7}, oled.Width*oled.Height)) {
		t.Fatal("standby content not drawn")
	}
	m.ClearStandby("afvalwijzer")
	m.step(context.Background())
	after, _ = os.ReadFile(path)
	if !bytes.Equal(after, bytes.Repeat([]byte{7}, oled.Width*oled.Height)) {
		t.Fatal("panel not restored after clear")
	}
}

func TestNotifyBeatsStandbyAndExpires(t *testing.T) {
	m, path := newTestManager(t, true)
	m.SetStandby("afvalwijzer", Content{Icon: "groen", Text: "standby"})
	m.step(context.Background())
	standbyFrame, _ := os.ReadFile(path)

	m.Notify(Content{Icon: "bell", Text: "iemand aan de deur"}, time.Second)
	m.step(context.Background())
	notifyFrame, _ := os.ReadFile(path)
	if bytes.Equal(notifyFrame, standbyFrame) {
		t.Fatal("notification did not take over")
	}

	m.mu.Lock()
	m.notifyUntil = time.Now().Add(-time.Second) // fast-forward expiry
	m.mu.Unlock()
	m.step(context.Background())
	back, _ := os.ReadFile(path)
	if !bytes.Equal(back, standbyFrame) {
		t.Fatal("standby screen not back after notification expired")
	}
}

func TestNotPlayingHidesStandby(t *testing.T) {
	m, path := newTestManager(t, false) // speaker playing
	m.SetStandby("afvalwijzer", Content{Icon: "groen", Text: "standby"})
	m.step(context.Background())
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, bytes.Repeat([]byte{7}, oled.Width*oled.Height)) {
		t.Fatal("standby content drawn while playing")
	}
}

func TestLastOwnerWins(t *testing.T) {
	m, path := newTestManager(t, true)
	m.SetStandby("a", Content{Icon: "groen", Text: "a"})
	m.SetStandby("b", Content{Icon: "bell", Text: "b"})
	m.step(context.Background())
	withB, _ := os.ReadFile(path)
	m.ClearStandby("b")
	m.step(context.Background())
	withA, _ := os.ReadFile(path)
	if bytes.Equal(withA, withB) {
		t.Fatal("clearing the winning owner did not fall back to the other")
	}
	if !bytes.Equal(withB, render(Content{Icon: "bell", Text: "b"})) {
		t.Fatal("most recently set owner was not the one shown")
	}
}

func TestRenderUnknownIconFallsBack(t *testing.T) {
	if len(render(Content{Icon: "nope", Text: "x"})) != oled.Width*oled.Height {
		t.Fatal("bad frame size")
	}
}

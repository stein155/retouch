package update

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestBetaPR(t *testing.T) {
	cases := []struct {
		tag    string
		wantN  int
		wantOK bool
	}{
		{"beta-pr-12", 12, true},
		{"beta-pr-1", 1, true},
		{"beta-pr-007", 7, true},
		{"v1.2.3", 0, false},
		{"beta-pr-", 0, false},
		{"beta-pr-x", 0, false},
		{"beta-pr-12-abc", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := BetaPR(c.tag)
		if ok != c.wantOK || n != c.wantN {
			t.Errorf("BetaPR(%q) = (%d, %v), want (%d, %v)", c.tag, n, ok, c.wantN, c.wantOK)
		}
	}
}

func testManager(t *testing.T, dir string) *Manager {
	t.Helper()
	return New("test", dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestStartNotUpdatable(t *testing.T) {
	// A homeDir without an installed "retouch" binary is not updatable: Start
	// must refuse before ever contacting GitHub.
	m := testManager(t, t.TempDir())
	if _, _, err := m.Start(context.Background(), ""); !errors.Is(err, ErrNotUpdatable) {
		t.Fatalf("Start on non-installed dir: %v, want ErrNotUpdatable", err)
	}
	if err := m.UpdateToLatest(context.Background()); !errors.Is(err, ErrNotUpdatable) {
		t.Fatalf("UpdateToLatest on non-installed dir: %v, want ErrNotUpdatable", err)
	}
	// UpdateInfo off-speaker reports latest == installed without a GitHub call.
	installed, latest, url, updatable, err := m.UpdateInfo(context.Background())
	if err != nil || updatable || installed != "test" || latest != "test" || url != "" {
		t.Fatalf("UpdateInfo = (%q, %q, %q, %v, %v), want (test, test, \"\", false, nil)",
			installed, latest, url, updatable, err)
	}
}

func TestStartBusy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "retouch"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := testManager(t, dir)
	m.mu.Lock() // simulate an install in flight
	defer m.mu.Unlock()
	if _, _, err := m.Start(context.Background(), "v9.9.9"); !errors.Is(err, ErrBusy) {
		t.Fatalf("Start while locked: %v, want ErrBusy", err)
	}
}

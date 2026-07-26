package update

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

func TestRequireSig(t *testing.T) {
	if releasePublicKey == "" {
		t.Skip("signing disabled; requireSig is always false")
	}
	cases := []struct {
		tag      string
		explicit bool
		want     bool
	}{
		{"v1.2.3", false, true},     // stable, auto path: signed
		{"v1.2.3", true, true},      // stable, explicit: signed
		{"beta-pr-12", true, false}, // beta, explicitly chosen: exempt
		{"beta-pr-12", false, true}, // beta on auto/latest path: MUST still require sig
	}
	for _, c := range cases {
		if got := requireSig(c.tag, c.explicit); got != c.want {
			t.Errorf("requireSig(%q, %v) = %v, want %v", c.tag, c.explicit, got, c.want)
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

// checkSpace refuses an install when the partition cannot hold another copy of the binary.
// The speaker this was found on has a ~31 MB NAND partition that already holds the running
// binary plus retouch.old; a download that runs it dry leaves the partition full, and a full
// partition stops ReTouch from starting at all.
func TestCheckSpace(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "retouch")

	// A tiny binary on a normal filesystem: plenty of room, no error.
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkSpace(dir, bin); err != nil {
		t.Errorf("checkSpace with a 1-byte binary and a normal temp dir: %v", err)
	}

	// An unreadable path must not block updates: we would rather try the download than
	// refuse because we could not measure.
	if err := checkSpace(filepath.Join(dir, "does-not-exist"), bin); err != nil {
		t.Errorf("checkSpace on an unmeasurable path should not fail: %v", err)
	}

	// A binary larger than any plausible free space must be refused, and the message has to
	// tell the user what to do about it.
	huge := filepath.Join(dir, "huge")
	if err := os.Truncate(huge, 1<<62); err != nil {
		if f, cerr := os.Create(huge); cerr == nil {
			_ = f.Truncate(1 << 62) // sparse; no bytes actually written
			_ = f.Close()
		}
	}
	if fi, err := os.Stat(huge); err == nil && fi.Size() > 1<<40 {
		err := checkSpace(dir, huge)
		if err == nil {
			t.Error("checkSpace should refuse when the binary cannot possibly fit")
		} else if !strings.Contains(err.Error(), "not enough space") {
			t.Errorf("err = %v, want it to mention the space problem", err)
		}
	}
}

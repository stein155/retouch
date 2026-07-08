package autopair

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stein155/retouch/internal/sim"
	"github.com/stein155/retouch/internal/speaker"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newClient wires a speaker.Client to an httptest server running the simulator's REST
// API. autopair only uses Info + SetMargeAccount (both REST), so the :17000 CLI is not
// needed. speaker.New accepts an explicit "host:port", letting us point it at httptest.
func newClient(t *testing.T, sp *sim.Speaker) *speaker.Client {
	t.Helper()
	ts := httptest.NewServer(sp.Handler())
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	return speaker.New(u.Host) // host:port
}

// New defaults an empty token to DefaultAuthToken and a non-positive interval to 5m,
// while keeping explicitly-supplied values.
func TestNewDefaults(t *testing.T) {
	cases := []struct {
		name         string
		token        string
		interval     time.Duration
		wantToken    string
		wantInterval time.Duration
	}{
		{"empty token defaults", "", time.Minute, DefaultAuthToken, time.Minute},
		{"explicit token kept", "abc", time.Minute, "abc", time.Minute},
		{"zero interval defaults", "t", 0, "t", 5 * time.Minute},
		{"negative interval defaults", "t", -1, "t", 5 * time.Minute},
		{"positive interval kept", "t", 30 * time.Second, "t", 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(speaker.New("127.0.0.1:1"), "acct", tc.token, tc.interval, quietLog())
			if p == nil {
				t.Fatal("New returned nil")
			}
			if p.token != tc.wantToken {
				t.Errorf("token = %q, want %q", p.token, tc.wantToken)
			}
			if p.interval != tc.wantInterval {
				t.Errorf("interval = %v, want %v", p.interval, tc.wantInterval)
			}
		})
	}
}

// With no account id, Run is a no-op and must return immediately (it never touches the
// speaker, so the unreachable client below is fine).
func TestRunNoAccountReturnsImmediately(t *testing.T) {
	p := New(speaker.New("127.0.0.1:1"), "", "", time.Minute, quietLog())
	done := make(chan struct{})
	go func() { p.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run with empty account did not return promptly")
	}
}

// A cancelled context must make Run return promptly even with an account set. We use a
// reachable, already-paired sim so the first check succeeds and Run blocks on the long
// interval; cancelling the context must unblock the select.
func TestRunReturnsOnContextCancel(t *testing.T) {
	sp := sim.New()
	sp.Account = "match-me"
	c := newClient(t, sp)

	p := New(c, "match-me", "tok", time.Hour, quietLog())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// check is the core "is a re-pair needed?" decision against a live speaker.
func TestCheckAlreadyPaired(t *testing.T) {
	sp := sim.New()
	sp.Account = "1234567" // already paired
	c := newClient(t, sp)

	p := New(c, "1234567", "tok", time.Minute, quietLog())
	if !p.check(context.Background()) {
		t.Fatal("check on already-paired speaker should report paired (true)")
	}
	// It must not have changed the account.
	if sp.Account != "1234567" {
		t.Errorf("account mutated on already-paired speaker: %q", sp.Account)
	}
}

func TestCheckUnpairedAsserts(t *testing.T) {
	sp := sim.New()
	sp.Account = "" // unpaired
	c := newClient(t, sp)

	p := New(c, "our-account", "tok", time.Minute, quietLog())

	// First pass: unpaired -> asserts association, returns false (needs confirm).
	if p.check(context.Background()) {
		t.Fatal("check on unpaired speaker should return false (re-asserted, not yet confirmed)")
	}
	if sp.Account != "our-account" {
		t.Fatalf("setMargeAccount not applied: account = %q, want our-account", sp.Account)
	}

	// Second pass: now paired -> returns true.
	if !p.check(context.Background()) {
		t.Error("check after re-assert should report paired (true)")
	}
}

// The factory-reset callback fires once per unpaired episode: on the first
// unpaired observation, not again while re-pairing is confirmed, and afresh
// after a later unpair.
func TestOnFactoryResetFiresOncePerEpisode(t *testing.T) {
	sp := sim.New()
	sp.Account = "" // unpaired from the start (boot after factory reset)
	c := newClient(t, sp)

	p := New(c, "our-account", "tok", time.Minute, quietLog())
	fired := 0
	p.OnFactoryReset(func() { fired++ })

	if p.check(context.Background()) {
		t.Fatal("first check should re-assert and return false")
	}
	if fired != 1 {
		t.Fatalf("fired = %d after first unpaired check, want 1", fired)
	}
	// Confirmed paired: no extra fire.
	if !p.check(context.Background()) {
		t.Fatal("second check should confirm paired")
	}
	if fired != 1 {
		t.Fatalf("fired = %d after confirm, want still 1", fired)
	}
	// A new unpaired episode (another reset) fires again.
	sp.Account = ""
	p.check(context.Background())
	if fired != 2 {
		t.Fatalf("fired = %d after second episode, want 2", fired)
	}
}

// A callback must not fire while the speaker is merely unreachable (no /info):
// unreachable is not "unpaired".
func TestOnFactoryResetNotFiredWhenUnreachable(t *testing.T) {
	p := New(speaker.New("127.0.0.1:1"), "acct", "tok", time.Minute, quietLog())
	fired := 0
	p.OnFactoryReset(func() { fired++ })
	p.check(context.Background())
	if fired != 0 {
		t.Fatalf("fired = %d for unreachable speaker, want 0", fired)
	}
}

func TestCheckUnreachableReturnsFalse(t *testing.T) {
	// Point at a closed port: Info fails, check returns false (will retry).
	p := New(speaker.New("127.0.0.1:1"), "acct", "tok", time.Minute, quietLog())
	if p.check(context.Background()) {
		t.Error("check against unreachable speaker should return false")
	}
}

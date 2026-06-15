// Package autopair keeps the speaker associated with our local marge account.
// After a factory-ish reset or a failed sync the speaker can drop to an unpaired state
// (margeAccountUUID empty), at which point native sources refuse with
// NOT_LOGGED_IN. The pairer watches /info and re-asserts the association by posting
// to :8090/setMargeAccount whenever the speaker is unpaired, then re-checks on a slow
// heartbeat. When the speaker is already paired it does nothing.
package autopair

import (
	"context"
	"log/slog"
	"time"

	"github.com/stein155/retouch/internal/speaker"
)

// DefaultAuthToken is the userAuthToken handed to the speaker; the local stub does not
// validate it, so any stable value works.
const DefaultAuthToken = "stlocal"

// Pairer re-asserts the speaker's marge association.
type Pairer struct {
	speaker  *speaker.Client
	account  string
	token    string
	interval time.Duration
	log      *slog.Logger
}

// New builds a Pairer. account is the marge account UUID to keep the speaker paired to;
// if empty, Run is a no-op (we have no account to assert).
func New(b *speaker.Client, account, token string, interval time.Duration, log *slog.Logger) *Pairer {
	if token == "" {
		token = DefaultAuthToken
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Pairer{speaker: b, account: account, token: token, interval: interval, log: log}
}

// fastRetry is the poll interval used until the speaker is first confirmed paired. The
// agent starts early in the boot sequence, so the speaker's :8090 is often not ready yet
// (or freshly unpaired); we retry quickly rather than wait a full heartbeat.
const fastRetry = 10 * time.Second

// Run polls until the speaker is paired to our account, then settles to the heartbeat
// interval, until ctx is cancelled.
func (p *Pairer) Run(ctx context.Context) {
	if p.account == "" {
		p.log.Info("autopair disabled (no account id)")
		return
	}
	for {
		wait := p.interval
		if !p.check(ctx) {
			wait = fastRetry // speaker unreachable or just (re-)paired — confirm soon
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// check returns true once the speaker is reachable AND paired. When the speaker reports no
// account it asserts the association and returns false, so Run retries on the fast
// interval and confirms on the next pass.
func (p *Pairer) check(ctx context.Context) bool {
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	info, err := p.speaker.Info(c)
	if err != nil {
		p.log.Warn("autopair: read /info (will retry)", "err", err)
		return false
	}
	if info.Account != "" {
		p.log.Debug("autopair: already paired", "account", info.Account)
		return true
	}
	if err := p.speaker.SetMargeAccount(c, p.account, p.token); err != nil {
		p.log.Warn("autopair: setMargeAccount failed (will retry)", "account", p.account, "err", err)
		return false
	}
	p.log.Info("autopair: re-asserted association", "account", p.account)
	return false
}

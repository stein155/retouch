// Package autopair keeps the speaker associated with our local marge account and out of
// the firmware's setup mode.
//
// After a factory-ish reset or a failed sync the speaker can drop to an unpaired state
// (margeAccountUUID empty), at which point native sources refuse with NOT_LOGGED_IN.
// A cold boot has a second failure mode: the firmware tries to reach marge before
// ReTouch is listening, gives up, and parks in setup mode — now_playing reports
// source=SETUP, streaming services (including Spotify Connect) never initialise, and
// the speaker keeps asking for the Bose app.
//
// Both are fixed by the same thing: the firmware's own SETUP state machine, run over its
// WebSocket by internal/setupsession. A bare POST to :8090/setMargeAccount is not enough
// — the firmware only completes setup when the account lands inside a
// SETUP_START … SETUP_LEAVE bracket. The bare POST stays as a fallback for when the
// WebSocket is unreachable (notably off-box, where ReTouch's own :8080 redirect answers
// instead of the firmware).
//
// The pairer watches /info plus /now_playing and re-runs the session whenever the
// speaker is unpaired or in SETUP, then re-checks on a slow heartbeat. When the speaker
// is paired and out of setup it does nothing.
package autopair

import (
	"context"
	"log/slog"
	"time"

	"github.com/stein155/retouch/internal/setupsession"
	"github.com/stein155/retouch/internal/speaker"
)

// SetupSource is the now_playing source the firmware reports while it is parked in
// setup mode, waiting for the SoundTouch app that no longer has a cloud to talk to.
const SetupSource = "SETUP"

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
	onReset  func() // fired once per unpaired episode; see OnFactoryReset
	fired    bool   // onReset already fired for the current unpaired episode

	// Set by WithSetupSession; without it the pairer keeps the old bare-POST behaviour.
	wsHost      string
	runSession  func(context.Context, setupsession.Plan, *slog.Logger) error
	cooldown    time.Duration // minimum gap between two sessions
	lastSession time.Time     // when the last session was attempted
}

// SessionCooldown is the minimum gap between two SETUP sessions. The state machine
// includes SETUP_IDENTIFY_DEVICE_ENTER, which makes the speaker chirp and flash, so a
// speaker that reports SETUP even after a completed session must not be poked every
// fast-retry tick. Between sessions the pairer falls back to the bare setMargeAccount
// POST, which is silent.
const SessionCooldown = 2 * time.Minute

// OnFactoryReset registers a callback fired when the speaker reports no marge
// account — which, once ReTouch has paired it, means the user factory-reset the
// speaker (physical access). The web layer uses this to clear the settings
// password and reopen telnet. Fired once per unpaired episode, before re-pairing.
// Call before Run.
func (p *Pairer) OnFactoryReset(f func()) { p.onReset = f }

// WithSetupSession enables the full SETUP state machine. host is the speaker host to open
// the firmware WebSocket on — "127.0.0.1" on-box, which is also the only host that works
// once the installer's :8080 → web-UI redirect is in place (nat PREROUTING does not touch
// loopback traffic). Call before Run.
//
// The session deliberately sends the MINIMAL pairing payload (account id + token), the same
// shape the installer and the bare POST have always used. The official app also sends
// <boseServer>, <updateServer> and <accountEmail>; setupsession can do that too, but we do
// not, because the effect of those fields on the speaker's persisted cloud state could not be
// verified — and a speaker whose service state is subtly rewritten is far worse than one that
// merely lacks a field.
func (p *Pairer) WithSetupSession(host string) *Pairer {
	p.wsHost = host
	p.runSession = setupsession.Run
	p.cooldown = SessionCooldown
	return p
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

// check returns true once the speaker is reachable, paired AND out of setup mode. When
// either is wrong it drives the SETUP state machine (falling back to a bare
// setMargeAccount POST) and returns false, so Run retries on the fast interval and
// confirms on the next pass.
func (p *Pairer) check(ctx context.Context) bool {
	c, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	info, err := p.speaker.Info(c)
	if err != nil {
		p.log.Warn("autopair: read /info (will retry)", "err", err)
		return false
	}

	inSetup := p.inSetup(c)
	if info.Account != "" && !inSetup {
		p.log.Debug("autopair: already paired", "account", info.Account)
		p.fired = false // paired again; a future unpaired episode may fire anew
		return true
	}
	if info.Account == "" && p.onReset != nil && !p.fired {
		p.fired = true
		p.onReset()
	}
	if inSetup {
		p.log.Info("autopair: speaker is in firmware SETUP mode", "account", info.Account)
	}

	if p.runSession != nil && time.Since(p.lastSession) >= p.cooldown {
		p.lastSession = time.Now()
		err := p.runSession(c, setupsession.Plan{
			Host:      p.wsHost,
			DeviceID:  info.DeviceID,
			AccountID: p.account,
			AuthToken: p.token,
			Name:      info.Name,
		}, p.log)
		if err == nil {
			p.log.Info("autopair: setup state machine completed", "account", p.account)
			return false // confirm on the next (fast) pass
		}
		p.log.Warn("autopair: setup state machine failed, falling back to setMargeAccount", "err", err)
	}

	if err := p.speaker.SetMargeAccount(c, p.account, p.token); err != nil {
		p.log.Warn("autopair: setMargeAccount failed (will retry)", "account", p.account, "err", err)
		return false
	}
	p.log.Info("autopair: re-asserted association", "account", p.account)
	return false
}

// inSetup reports whether the firmware is parked in setup mode. A failed read is not
// setup: it usually means :8090 is not up yet, and the account check alone decides.
func (p *Pairer) inSetup(ctx context.Context) bool {
	np, err := p.speaker.NowPlaying(ctx)
	return err == nil && np.Source == SetupSource
}

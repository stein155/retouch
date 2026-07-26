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
	setLanguage func(context.Context, setupsession.Plan, *slog.Logger) error
	langTries   int // bounded: a flag that refuses to flip must not be poked forever
}

// maxLanguageTries bounds the repair. The frame is silent and cheap, but if the firmware
// refuses to flip its flag there is no point sending it on every heartbeat forever.
const maxLanguageTries = 3

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
	p.setLanguage = setupsession.SetLanguage
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

// check returns true once the speaker is reachable, paired and past the firmware's setup
// prompt. Anything else is repaired here and returns false, so Run confirms on the next pass.
func (p *Pairer) check(ctx context.Context) bool {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := p.speaker.Info(c)
	if err != nil {
		p.log.Warn("autopair: read /info (will retry)", "err", err)
		return false
	}

	if info.Account == "" {
		if p.onReset != nil && !p.fired {
			p.fired = true
			p.onReset()
		}
		if err := p.speaker.SetMargeAccount(c, p.account, p.token); err != nil {
			p.log.Warn("autopair: setMargeAccount failed (will retry)", "account", p.account, "err", err)
			return false
		}
		p.log.Info("autopair: re-asserted association", "account", p.account)
		return false // confirm on the next pass
	}
	p.fired = false // paired; a future unpaired episode may fire anew

	// The speaker keeps asking to be set up while its persistent language flag is unset. One
	// frame flips it, and it stays flipped across reboots.
	if p.setLanguage != nil && p.langTries < maxLanguageTries {
		st, err := p.speaker.SetupState(c)
		if err != nil {
			p.log.Debug("autopair: read /setup (will retry)", "err", err)
			return true // paired and reachable; the flag check can wait for the next pass
		}
		if st.System == speaker.LanguageNotSet {
			p.langTries++
			p.log.Info("autopair: firmware reports setup never finished; setting the language",
				"systemstate", st.System, "attempt", p.langTries)
			if err := p.setLanguage(c, setupsession.Plan{Host: p.wsHost, DeviceID: info.DeviceID}, p.log); err != nil {
				p.log.Warn("autopair: could not set the language (speaker keeps prompting for setup)", "err", err)
			}
			return false // re-read the flag on the next pass
		}
	}

	p.log.Debug("autopair: paired and set up", "account", info.Account)
	return true
}

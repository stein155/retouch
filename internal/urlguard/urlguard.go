// Package urlguard keeps the speaker's cloud URL pointed at the on-speaker stub.
//
// install/install.sh bootstraps a speaker by setting its boseurls to a one-shot string
// (speaker.BootstrapURL): an http://x.invalid placeholder followed by a `curl … ;sh …`
// that fetches and runs netinstall.sh. The firmware runs that embedded command early in
// boot — that is how ReTouch installs without SSH — and netinstall.sh is then meant to
// reset boseurls to the local stub. On some speakers that reset does not stick, leaving
// the bootstrap string as the live margeServerUrl: native sources resolve against a
// bogus URL and the curl|sh re-runs every boot.
//
// This guard fixes it from the speaker side. Whenever it sees EXACTLY the install
// bootstrap string as the speaker's cloud URL, it repoints the cloud URLs at the stub.
// It matches only that one literal, so a recovery command deliberately pushed through
// the same channel (e.g. one that enables SSH) is left untouched and keeps working —
// and if ReTouch is not running at all, nothing repoints it, so the channel stays open
// exactly when it is needed.
package urlguard

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/stein155/retouch/internal/speaker"
)

// Guard repoints the speaker's cloud URLs at the local stub when it finds the leftover
// install bootstrap string.
type Guard struct {
	speaker  *speaker.Client
	base     string
	interval time.Duration
	log      *slog.Logger
}

// New builds a Guard. base is the on-speaker stub base URL (e.g. http://127.0.0.1:9080);
// interval is the re-check heartbeat.
func New(b *speaker.Client, base string, interval time.Duration, log *slog.Logger) *Guard {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Guard{speaker: b, base: base, interval: interval, log: log}
}

// Run checks once promptly at startup, then re-checks on the heartbeat until ctx is
// cancelled. The firmware runs the bootstrap command early in boot, so by the time the
// first check fires the command has already done its job for this boot.
func (g *Guard) Run(ctx context.Context) {
	for {
		g.check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(g.interval):
		}
	}
}

func (g *Guard) check(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cfg, err := g.speaker.CloudConfig(c)
	if err != nil {
		g.log.Debug("read cloud config", "err", err)
		return
	}
	if !isBootstrapLeftover(cfg) {
		return // already clean, or a deliberate command we must not touch
	}
	g.log.Info("cloud URL stuck on install bootstrap; repointing at stub", "base", g.base)
	if err := g.speaker.PointCloudAtStub(c, g.base); err != nil {
		g.log.Warn("repoint cloud at stub", "err", err)
		return
	}
	g.log.Info("repointed cloud URLs at stub")
}

// isBootstrapLeftover reports whether cfg still contains the exact one-shot install
// bootstrap URL. Only that literal qualifies: a deliberately pushed recovery command
// does not contain it and so is preserved.
func isBootstrapLeftover(cfg string) bool {
	return strings.Contains(cfg, speaker.BootstrapURL)
}

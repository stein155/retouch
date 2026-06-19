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
// This guard fixes it from the speaker side. It reads the speaker's reported cloud URL
// (the <margeURL> in /info) and, whenever it sees EXACTLY the install bootstrap string,
// it repoints the cloud URLs at the stub and reboots, so the firmware re-derives
// margeServerUrl from the cleaned boseurls. Unlike the installer's cleanup it does not
// depend on the speaker having `nc` (it dials the :17000 CLI directly), which is the
// likely reason the installer's reset did not stick.
//
// It matches only that one literal, so a recovery command deliberately pushed through
// the same channel (e.g. one that enables SSH) is left untouched and keeps working —
// and if ReTouch is not running at all, nothing repoints it, so the channel stays open
// exactly when it is needed. Reboots are bounded by a persistent attempt counter so a
// reset that never sticks can never become an endless reboot loop.
package urlguard

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/stein155/retouch/internal/speaker"
)

// maxRebootAttempts caps how many times we reboot trying to apply a cleaned cloud URL,
// so a reset that never persists cannot loop forever. The counter is cleared as soon as
// the speaker reports a clean URL.
const maxRebootAttempts = 3

// flushDelay gives the persistent envswitch a moment to reach NAND before the reboot
// drops us — the race the installer's immediate reboot could lose.
const flushDelay = 2 * time.Second

// Guard repoints the speaker's cloud URLs at the local stub when it finds the leftover
// install bootstrap string.
type Guard struct {
	speaker   *speaker.Client
	base      string
	interval  time.Duration
	statePath string
	log       *slog.Logger
}

// New builds a Guard. base is the on-speaker stub base URL (e.g. http://127.0.0.1:9080);
// interval is the re-check heartbeat; statePath persists the reboot attempt counter.
func New(b *speaker.Client, base, statePath string, interval time.Duration, log *slog.Logger) *Guard {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Guard{speaker: b, base: base, statePath: statePath, interval: interval, log: log}
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
	info, err := g.speaker.Info(c)
	if err != nil {
		g.log.Debug("read speaker /info", "err", err)
		return
	}
	if !isBootstrapLeftover(info.MargeURL) {
		clearRebootAttempts(g.statePath) // healthy — reset the guard for any future event
		return
	}

	g.log.Info("cloud URL stuck on install bootstrap; repointing at stub", "base", g.base, "margeURL", info.MargeURL)
	if err := g.speaker.PointCloudAtStub(c, g.base); err != nil {
		g.log.Warn("repoint cloud at stub", "err", err)
		return
	}

	// margeServerUrl is re-derived from boseurls at boot, so the cleaned value only shows
	// up after a restart. Reboot to apply it — but bounded, so a reset that never sticks
	// cannot loop forever.
	attempts := readRebootAttempts(g.statePath)
	if attempts >= maxRebootAttempts {
		g.log.Warn("cloud URL still stuck after reboots; left runtime URLs set, not rebooting again", "attempts", attempts)
		return
	}
	writeRebootAttempts(g.statePath, attempts+1)
	g.log.Info("rebooting to apply cleaned cloud URLs", "attempt", attempts+1, "max", maxRebootAttempts)
	select {
	case <-ctx.Done():
		return
	case <-time.After(flushDelay):
	}
	if err := g.speaker.Reboot(c); err != nil {
		g.log.Warn("reboot", "err", err)
	}
}

// isBootstrapLeftover reports whether the speaker's reported cloud URL still contains
// the exact one-shot install bootstrap URL. Only that literal qualifies: a deliberately
// pushed recovery command does not contain it and so is preserved.
func isBootstrapLeftover(margeURL string) bool {
	return strings.Contains(margeURL, speaker.BootstrapURL)
}

func readRebootAttempts(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func writeRebootAttempts(path string, n int) {
	_ = os.WriteFile(path, []byte(strconv.Itoa(n)), 0o644)
}

func clearRebootAttempts(path string) {
	_ = os.Remove(path)
}

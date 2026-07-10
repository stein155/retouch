// Package telnet owns the LAN block on the speaker's diagnostic CLI (port 17000).
// The firmware exposes a root telnet/CLI on :17000; ReTouch can hide it from the
// LAN with an iptables raw-table DROP (loopback stays open so the on-speaker agent
// keeps working). The choice is persisted in a marker file because the iptables
// rule itself does not survive a reboot — only the marker does, so the block is
// re-applied at every startup.
//
// This lives outside internal/web on purpose: it is host/OS logic (root exec,
// kernel packet filtering, a reboot-persistent marker), not HTTP. web drives it
// through a small Guard instead of shelling out to iptables itself.
package telnet

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// Guard applies and persists the :17000 LAN block. Create with New.
type Guard struct {
	marker string                  // path to the .close-telnet persistence marker
	apply  func(closed bool) error // firewall applier; overridable for tests
	log    *slog.Logger
}

// New builds a Guard rooted at homeDir (where the .close-telnet marker lives).
func New(homeDir string, log *slog.Logger) *Guard {
	return &Guard{
		marker: filepath.Join(homeDir, ".close-telnet"),
		apply:  iptablesApply,
		log:    log,
	}
}

// SetApplier overrides how the firewall change is applied. Tests inject a fake so
// toggling never runs iptables.
func (g *Guard) SetApplier(f func(closed bool) error) { g.apply = f }

// IsClosed reports whether the block is configured to persist (marker present).
// It reflects the intended state, not a live iptables read.
func (g *Guard) IsClosed() bool { return fileExists(g.marker) }

// ApplyAtStartup re-applies the block when the marker says it should be on. Call
// once at startup: the iptables rule does not survive a reboot, only the marker.
// A no-op (nil) when the block is not enabled.
func (g *Guard) ApplyAtStartup() error {
	if !g.IsClosed() {
		return nil
	}
	if err := g.apply(true); err != nil {
		return err
	}
	g.log.Info("closed LAN telnet", "port", 17000)
	return nil
}

// Set closes (closed=true) or opens the LAN block and persists the choice.
func (g *Guard) Set(closed bool) error {
	if closed {
		return g.close()
	}
	return g.open()
}

func (g *Guard) close() error {
	// Apply the firewall FIRST, persist the marker only on success — otherwise a
	// failed iptables call would leave the marker on disk and IsClosed() reporting
	// the port as blocked while root telnet is still LAN-reachable.
	if err := g.apply(true); err != nil {
		return err
	}
	if err := os.WriteFile(g.marker, []byte("1\n"), 0o644); err != nil {
		return err
	}
	g.log.Info("closed LAN telnet", "port", 17000)
	return nil
}

func (g *Guard) open() error {
	// Remove the rule first; drop the marker only once it's actually open, so the
	// persisted intent never says "open" while the DROP rule is still installed.
	if err := g.apply(false); err != nil {
		return err
	}
	if err := os.Remove(g.marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	g.log.Info("reopened LAN telnet", "port", 17000)
	return nil
}

// iptablesApply installs or removes the raw-table DROP that hides the :17000
// diagnostic CLI from the LAN (loopback stays open), de-duplicating any earlier
// copies first.
func iptablesApply(closed bool) error {
	script := "while iptables -t raw -D PREROUTING ! -i lo -p tcp --dport 17000 -j DROP 2>/dev/null; do :; done"
	if closed {
		script += "; iptables -t raw -I PREROUTING 1 ! -i lo -p tcp --dport 17000 -j DROP"
	}
	return exec.Command("sh", "-c", script).Run()
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

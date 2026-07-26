// Package avahi starts the firmware's own mDNS daemon when the firmware itself fails to.
//
// BoseApp does not publish mDNS by itself — it is an avahi *client* (it links
// libavahi-client and registers _soundtouch, _spotify-connect and the AirPlay services
// with a running avahi-daemon). The daemon is launched by /opt/Bose/start-avahi, which
// builds its hostname from the wlan0 MAC:
//
//	hwaddr=$(sed 's/://g' /sys/class/net/wlan0/address)
//
// On a SoundTouch 10 that works: Wi-Fi is on the main CPU, wlan0 exists, avahi comes up
// and the speaker appears in AirPlay, in Spotify and in SoundTouch controllers. On a
// SoundTouch 20 the Wi-Fi lives on the front processor, so the main CPU only has eth0,
// lo and usb0 — there is no wlan0, and nothing in the boot sequence starts the daemon at
// all. The result is a speaker that publishes nothing: invisible to AirPlay, invisible
// to Spotify Connect (its ZeroConf endpoint answers on :8200 but no client can find it),
// and undiscoverable by controllers.
//
// ReTouch is started from /mnt/nv/rc.local before BoseApp comes up, which is the one
// moment where this is fixable: start the daemon here and BoseApp registers with it as
// it always meant to. Ensure is a no-op when a daemon is already listening, so on models
// where the firmware gets there first (the ST10) nothing changes.
package avahi

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Paths and knobs, kept as variables so tests can point them at a temp dir.
var (
	daemonPath = "/usr/sbin/avahi-daemon"
	confPath   = "/var/run/avahi-daemon.conf"
	netClass   = "/sys/class/net"
)

// hostnamePrefix mirrors the product prefix Bose's own start-avahi uses, so the published
// hostname keeps the shape controllers and users already know (Bose-SM2-<mac>.local).
const hostnamePrefix = "Bose-SM2"

// Ensure starts avahi-daemon unless something already holds UDP 5353. It returns nil when
// there is nothing to do — a missing daemon binary is normal on other platforms and is not
// an error worth failing the boot over.
func Ensure(log *slog.Logger) error {
	if inUse() {
		log.Debug("avahi: mDNS already served on :5353; leaving it alone")
		return nil
	}
	if _, err := os.Stat(daemonPath); err != nil {
		log.Debug("avahi: no daemon on this platform; skipping", "path", daemonPath)
		return nil
	}

	host := hostname()
	if err := os.WriteFile(confPath, []byte(config(host)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", confPath, err)
	}

	// --no-drop-root matches Bose's own launcher: the daemon has to stay root to talk to
	// the system bus here. Started detached; if it dies the speaker simply goes back to
	// publishing nothing, which is the state we are fixing, so failure is non-fatal.
	cmd := exec.Command(daemonPath, "--no-drop-root", "--syslog", "-f", confPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start avahi-daemon: %w", err)
	}
	go func() { _ = cmd.Wait() }() // reap it; we never block on it

	log.Info("avahi: started the firmware's mDNS daemon", "host", host+".local")
	return nil
}

// inUse reports whether anything already serves mDNS. Binding the port exclusively is the
// cheapest reliable probe: avahi holds 5353 for its whole life, so a successful exclusive
// bind means no daemon is running. The socket is closed immediately.
func inUse() bool {
	conn, err := net.ListenPacket("udp4", ":5353")
	if err != nil {
		return true
	}
	_ = conn.Close()
	return false
}

// hostname builds the daemon's host-name the way Bose's script does, but falls back to the
// wired MAC when there is no wlan0 (the SoundTouch 20 case). A MAC-derived name is stable
// across reboots; the script's own fallback is a timestamp, which would hand the speaker a
// different .local name on every boot.
func hostname() string {
	for _, iface := range []string{"wlan0", "eth0"} {
		mac, err := os.ReadFile(netClass + "/" + iface + "/address")
		if err != nil {
			continue
		}
		if clean := strings.ReplaceAll(strings.TrimSpace(string(mac)), ":", ""); clean != "" {
			return hostnamePrefix + "-" + clean
		}
	}
	return hostnamePrefix
}

// config renders the daemon config. The values mirror /opt/Bose/start-avahi so the daemon
// behaves exactly as the firmware expects it to: IPv4 only, no wide-area, no workstation
// record, and usb0 (the setup gadget) kept out of it.
func config(host string) string {
	return `[server]
host-name=` + host + `
use-ipv4=yes
use-ipv6=no
ratelimit-interval-usec=1000000
ratelimit-burst=1000
deny-interfaces=usb0

[wide-area]
enable-wide-area=no

[publish]
publish-workstation=no

[reflector]
enable-reflector=no

[rlimits]
rlimit-core=0
rlimit-data=4194304
rlimit-fsize=0
rlimit-nofile=768
rlimit-stack=4194304
rlimit-nproc=3
`
}

// launcherMarker identifies our line in the launcher, so EnsureLauncher is idempotent.
const launcherMarker = "-avahi-only"

// EnsureLauncher adds the daemon start to an existing boot launcher that predates it, so a
// speaker updated over the air (which replaces only the binary) gets the fix on its next
// boot. Without it ReTouch loses the race: the launcher runs the cloud-config repair first,
// and BoseApp registers its services — finding no daemon — while that is still going.
//
// Idempotent, and a no-op when the launcher is missing or already carries the line.
// binPath is written into the launcher verbatim, so it must be the absolute path of this
// binary: the generated launcher has no $BIN variable at run time.
func EnsureLauncher(path, binPath string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // not installed through the launcher; nothing to repair
		}
		return err
	}
	if strings.Contains(string(body), launcherMarker) {
		return nil
	}
	lines := strings.SplitN(string(body), "\n", 2)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "#!") {
		return fmt.Errorf("%s: no shebang to insert after", path)
	}
	patched := lines[0] + "\n\n# Start the firmware's mDNS daemon first; see internal/avahi.\n" +
		strconv.Quote(binPath) + " " + launcherMarker + " >>\"$LOG\" 2>&1 || true\n" + lines[1]
	// Write via a temp file in the same directory so a full or failing NAND can never
	// leave a half-written launcher behind.
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(patched), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

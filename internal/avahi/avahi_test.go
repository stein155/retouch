package avahi

import (
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// hostname prefers wlan0 (what the firmware's own script uses) and falls back to eth0,
// which is the SoundTouch 20 case: no wlan0 exists there, and the script's own fallback is
// a timestamp that would change the speaker's .local name on every boot.
func TestHostnamePrefersWlanThenEth(t *testing.T) {
	dir := t.TempDir()
	old := netClass
	netClass = dir
	t.Cleanup(func() { netClass = old })

	write := func(iface, mac string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, iface), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, iface, "address"), []byte(mac+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// No interfaces at all: a bare prefix, never an empty or timestamped name.
	if got := hostname(); got != hostnamePrefix {
		t.Errorf("hostname with no interfaces = %q, want %q", got, hostnamePrefix)
	}

	write("eth0", "00:0c:8a:b7:21:2b")
	if got, want := hostname(), "Bose-SM2-000c8ab7212b"; got != want {
		t.Errorf("hostname with only eth0 = %q, want %q", got, want)
	}

	write("wlan0", "78:04:73:e7:38:4c")
	if got, want := hostname(), "Bose-SM2-780473e7384c"; got != want {
		t.Errorf("hostname with wlan0 = %q, want %q", got, want)
	}
}

// The rendered config must carry the host name and keep the firmware's own settings —
// IPv4-only and usb0 (the setup gadget) excluded.
func TestConfigCarriesHostAndFirmwareSettings(t *testing.T) {
	got := config("Bose-SM2-abc")
	for _, want := range []string{
		"host-name=Bose-SM2-abc",
		"use-ipv4=yes",
		"use-ipv6=no",
		"deny-interfaces=usb0",
		"publish-workstation=no",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
}

// Ensure must not fight a daemon that is already publishing: on models where the firmware
// starts avahi itself, ReTouch has to keep its hands off.
func TestEnsureNoOpWhenPortHeld(t *testing.T) {
	conn, err := net.ListenPacket("udp4", ":5353")
	if err != nil {
		t.Skipf("cannot bind :5353 in this environment: %v", err)
	}
	defer func() { _ = conn.Close() }()

	dir := t.TempDir()
	oldConf, oldDaemon := confPath, daemonPath
	confPath = filepath.Join(dir, "avahi-daemon.conf")
	daemonPath = filepath.Join(dir, "avahi-daemon") // does not exist either
	t.Cleanup(func() { confPath, daemonPath = oldConf, oldDaemon })

	if err := Ensure(quietLog()); err != nil {
		t.Fatalf("Ensure with :5353 already held: %v", err)
	}
	if _, err := os.Stat(confPath); err == nil {
		t.Error("Ensure wrote a config even though mDNS was already served")
	}
}

// A platform without the daemon binary is normal, not an error: Ensure returns nil and
// writes nothing.
func TestEnsureSkipsWithoutDaemonBinary(t *testing.T) {
	dir := t.TempDir()
	oldConf, oldDaemon := confPath, daemonPath
	confPath = filepath.Join(dir, "avahi-daemon.conf")
	daemonPath = filepath.Join(dir, "no-such-avahi")
	t.Cleanup(func() { confPath, daemonPath = oldConf, oldDaemon })

	if err := Ensure(quietLog()); err != nil {
		t.Fatalf("Ensure without a daemon binary: %v", err)
	}
	if _, err := os.Stat(confPath); err == nil {
		t.Error("Ensure wrote a config with no daemon to start")
	}
}

// EnsureLauncher inserts the daemon start right after the shebang — before the cloud-config
// repair the launcher runs, which is slow enough for BoseApp to register first and lose.
func TestEnsureLauncherInsertsOnceAfterShebang(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "start.sh")
	original := "#!/bin/sh\n\nLOG=/tmp/retouch.log\nfix_cloud\nexec /mnt/nv/retouch/retouch -listen :8000 &\n"
	if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLauncher(path, "/mnt/nv/retouch/retouch"); err != nil {
		t.Fatalf("EnsureLauncher: %v", err)
	}
	patched, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(patched)

	if !strings.Contains(got, `"/mnt/nv/retouch/retouch" -avahi-only`) {
		t.Errorf("launcher missing the avahi start:\n%s", got)
	}
	// It has to come before fix_cloud, or the daemon is late again.
	if idx, fix := strings.Index(got, "-avahi-only"), strings.Index(got, "fix_cloud"); idx > fix {
		t.Errorf("avahi start lands after fix_cloud (%d > %d):\n%s", idx, fix, got)
	}
	if lines := strings.SplitN(got, "\n", 2); !strings.HasPrefix(lines[0], "#!") {
		t.Errorf("shebang no longer first: %q", lines[0])
	}
	if !strings.Contains(got, "exec /mnt/nv/retouch/retouch -listen :8000 &") {
		t.Errorf("original launcher body lost:\n%s", got)
	}

	// Second call must change nothing.
	if err := EnsureLauncher(path, "/mnt/nv/retouch/retouch"); err != nil {
		t.Fatalf("second EnsureLauncher: %v", err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != got {
		t.Error("EnsureLauncher is not idempotent")
	}
}

// A missing launcher is not an error: ReTouch may be running outside an installed speaker.
func TestEnsureLauncherMissingFileIsFine(t *testing.T) {
	if err := EnsureLauncher(filepath.Join(t.TempDir(), "absent.sh"), "/bin/true"); err != nil {
		t.Errorf("EnsureLauncher on a missing launcher: %v", err)
	}
}

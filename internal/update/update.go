// Package update owns ReTouch's self-update: discovering releases on GitHub,
// downloading and verifying the new binary (checksum + ed25519 signature, via
// internal/release), swapping it in with a rollback copy, and restarting the
// agent. Both the web API (POST /api/update) and the Home Assistant update
// entity drive updates through the same Manager, so there is exactly one place
// that may replace the binary — and exactly one lock serializing it.
package update

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/release"
)

const repo = "stein155/retouch"

// releasePublicKey is the base64-encoded ed25519 public key that release SHA256SUMS
// files are signed with. When set, a self-update refuses to install unless the
// release ships a valid SHA256SUMS.sig — so a checksum that merely matches the
// (same-origin) SHA256SUMS is no longer enough; the binary must be signed by the
// holder of the private key, closing the "compromised GitHub release → root on
// every speaker" gap that TLS + checksum alone leave open.
//
// Empty (the default) keeps the prior behaviour (TLS + checksum only). To enable
// signing, generate a keypair, put the public half here, and sign releases in CI
// with the private half — see docs/RELEASE_SIGNING.md.
const releasePublicKey = "vmNYVuOZnDN7P7ipK43aJNZ2R6tT1IRX6TXvw7+IvX8="

const downloadTimeout = 5 * time.Minute

// betaPRRe matches the per-PR beta tag the Beta Build workflow publishes
// (beta-pr-<number>), so the app can show "PR #<n>" and accept it as a target.
var betaPRRe = regexp.MustCompile(`^beta-pr-(\d+)$`)

// requireSig reports whether installing tag must verify SHA256SUMS.sig. When
// signing is enabled it is required for everything except an explicitly-requested
// beta: betas are built by the Beta Build workflow (which never holds the signing
// key) so they ship no signature, and they are maintainer-triggered opt-in. The
// exemption holds only when the tag was explicitly chosen — the auto/latest path
// must never accept an unsigned beta, or a compromised repo could publish a stable
// release tagged beta-pr-<n>, have releases/latest serve it, and reach root on
// every speaker with TLS+checksum only, defeating the signing this key enforces.
func requireSig(tag string, explicit bool) bool {
	if releasePublicKey == "" {
		return false
	}
	_, isBeta := BetaPR(tag)
	return !(isBeta && explicit)
}

// BetaPR extracts the PR number from a beta-pr-<n> tag.
func BetaPR(tag string) (int, bool) {
	m := betaPRRe.FindStringSubmatch(tag)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

// Release is one GitHub release, as much of it as the updater needs.
type Release struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// ErrNotUpdatable means this instance can't self-update (not an installed speaker).
var ErrNotUpdatable = errors.New("updates are only available on an installed speaker")

// ErrBusy means an update is already in progress.
var ErrBusy = errors.New("an update is already in progress")

// UnknownTagError reports a requested tag that is not a published release.
type UnknownTagError struct{ Tag string }

func (e UnknownTagError) Error() string { return "unknown release " + e.Tag }

// Manager performs self-updates. Create with New; safe for concurrent use.
type Manager struct {
	version string
	homeDir string
	log     *slog.Logger
	client  *http.Client // GitHub API lookups, over an SSRF-safe transport

	// mu is held for the whole install (TryLock — a failed acquire means "busy").
	// Start hands it off to the background install goroutine.
	mu sync.Mutex

	// beforeRestart, when set, runs right before the post-install restart (the
	// restart os.Exit()s, skipping context cancellation — use this to stop child
	// processes that would otherwise be orphaned).
	beforeRestart func()
}

// New builds a Manager for the running version, rooted at homeDir (where the
// installed binary, start.sh and .version live).
func New(version, homeDir string, log *slog.Logger) *Manager {
	return &Manager{
		version: version,
		homeDir: homeDir,
		log:     log,
		client:  &http.Client{Timeout: 12 * time.Second, Transport: release.SafeTransport()},
	}
}

// SetBeforeRestart registers a hook run just before the post-update restart.
func (m *Manager) SetBeforeRestart(f func()) { m.beforeRestart = f }

// Version returns the running version the Manager was built with.
func (m *Manager) Version() string { return m.version }

// Updatable reports whether self-update is possible here: only on an installed
// speaker, where homeDir holds the running binary.
func (m *Manager) Updatable() bool {
	return filepath.IsAbs(m.homeDir) && m.homeDir != "/" && fileExists(filepath.Join(m.homeDir, "retouch"))
}

// Start resolves tag ("" means the latest stable release), verifies it is a
// release we actually publish, and — unless it is already running — installs it
// and restarts in the background. It returns the resolved target and whether it
// was already current. Errors: ErrNotUpdatable, ErrBusy, UnknownTagError, or a
// wrapped release-lookup failure.
func (m *Manager) Start(ctx context.Context, tag string) (target string, current bool, err error) {
	if !m.Updatable() {
		return "", false, ErrNotUpdatable
	}
	if !m.mu.TryLock() {
		return "", false, ErrBusy
	}
	target = strings.TrimSpace(tag)
	explicit := target != "" // a user-picked tag, vs the auto/latest path
	if target == "" {
		latest, err := m.Latest(ctx)
		if err != nil {
			m.mu.Unlock()
			return "", false, fmt.Errorf("latest release check failed: %w", err)
		}
		if latest == "" {
			m.mu.Unlock()
			return "", false, errors.New("latest release missing tag")
		}
		target = latest
	} else {
		// Only ever install a tag we actually publish — never an arbitrary ref —
		// so a crafted request can't point the speaker at a foreign download.
		ok, err := m.isOffered(ctx, target)
		if err != nil {
			m.mu.Unlock()
			return "", false, fmt.Errorf("release check failed: %w", err)
		}
		if !ok {
			m.mu.Unlock()
			return "", false, UnknownTagError{Tag: target}
		}
	}
	if target == m.version {
		m.mu.Unlock()
		return target, true, nil
	}
	m.install(target, explicit)
	return target, false, nil
}

// UpdateInfo reports the running version, the latest available stable release, its
// release URL, and whether updating is possible here (only on an installed speaker).
// It backs the Home Assistant `update` entity so HA can show an update notification.
// Off-speaker (not updatable) it reports latest == installed and skips the GitHub
// call, so no false update is offered.
func (m *Manager) UpdateInfo(ctx context.Context) (installed, latest, releaseURL string, updatable bool, err error) {
	installed = m.version
	updatable = m.Updatable()
	if !updatable {
		return installed, installed, "", false, nil
	}
	latest, err = m.Latest(ctx)
	if err != nil {
		return installed, "", "", true, err
	}
	if latest == "" {
		latest = installed
	}
	if latest != installed {
		releaseURL = "https://github.com/" + repo + "/releases/tag/" + latest
	}
	return installed, latest, releaseURL, true, nil
}

// UpdateToLatest installs the latest stable release and restarts, reusing the same
// path as POST /api/update. It backs the Home Assistant update entity's Install
// action. It returns quickly: the download + restart run in the background on
// success, and an already-current version is not an error.
func (m *Manager) UpdateToLatest(ctx context.Context) error {
	_, _, err := m.Start(ctx, "")
	return err
}

// Latest returns the tag of the latest stable GitHub release.
func (m *Manager) Latest(ctx context.Context) (string, error) {
	var rel Release
	if err := m.getJSON(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", &rel); err != nil {
		return "", err
	}
	return strings.TrimSpace(rel.TagName), nil
}

// Releases returns the repo's releases, newest first (GitHub's order).
func (m *Manager) Releases(ctx context.Context) ([]Release, error) {
	var rels []Release
	if err := m.getJSON(ctx, "https://api.github.com/repos/"+repo+"/releases?per_page=100", &rels); err != nil {
		return nil, err
	}
	return rels, nil
}

// isOffered reports whether tag is a real, non-draft release of this repo.
func (m *Manager) isOffered(ctx context.Context, tag string) (bool, error) {
	rels, err := m.Releases(ctx)
	if err != nil {
		return false, err
	}
	for _, rel := range rels {
		if !rel.Draft && strings.TrimSpace(rel.TagName) == tag {
			return true, nil
		}
	}
	return false, nil
}

// install downloads and swaps in release `to`, then restarts — in the background.
// The caller must already hold mu (via TryLock); install hands the lock off to the
// goroutine and releases it when the install finishes or fails.
func (m *Manager) install(to string, explicit bool) {
	from := m.version
	go func() {
		defer m.mu.Unlock()
		if err := m.installRelease(context.Background(), to, explicit); err != nil {
			m.log.Warn("self-update failed", "from", from, "to", to, "err", err)
			return
		}
		m.log.Info("self-update installed; restarting", "from", from, "to", to)
		m.restart()
	}()
}

func (m *Manager) installRelease(ctx context.Context, tag string, explicit bool) error {
	bin := filepath.Join(m.homeDir, "retouch")
	newBin := bin + ".new"
	sums := filepath.Join(m.homeDir, "SHA256SUMS")
	base := "https://github.com/" + repo + "/releases/download/" + tag
	if err := m.downloadFile(ctx, base+"/retouch-armv7l", newBin, 0o755); err != nil {
		return err
	}
	if err := m.downloadFile(ctx, base+"/SHA256SUMS", sums, 0o644); err != nil {
		_ = os.Remove(newBin)
		return err
	}
	// When signing is enabled, the checksums file itself must carry a valid
	// signature before we trust any checksum in it. Betas are exempt: they are
	// built from PR code by the Beta Build workflow, which must never be given
	// the release signing key, so a beta ships no SHA256SUMS.sig. Betas stay
	// gated by being maintainer-triggered, opt-in prereleases over TLS + checksum.
	//
	if requireSig(tag, explicit) {
		sig := filepath.Join(m.homeDir, "SHA256SUMS.sig")
		if err := m.downloadFile(ctx, base+"/SHA256SUMS.sig", sig, 0o644); err != nil {
			_ = os.Remove(newBin)
			return err
		}
		if err := release.VerifySignature(releasePublicKey, sums, sig); err != nil {
			_ = os.Remove(newBin)
			return err
		}
	}
	want, err := release.ChecksumFor(sums, "retouch-armv7l")
	if err != nil {
		_ = os.Remove(newBin)
		return err
	}
	if err := release.VerifyChecksum(newBin, want); err != nil {
		_ = os.Remove(newBin)
		return err
	}
	// Keep the outgoing binary as retouch.old so a bad release can be rolled back
	// by hand (best-effort; a first install has none). Restore it if the swap fails,
	// so we never leave the speaker with no binary.
	old := bin + ".old"
	hadOld := fileExists(bin)
	if hadOld {
		_ = os.Rename(bin, old)
	}
	if err := os.Rename(newBin, bin); err != nil {
		_ = os.Remove(newBin)
		if hadOld {
			_ = os.Rename(old, bin)
		}
		return err
	}
	return os.WriteFile(filepath.Join(m.homeDir, ".version"), []byte(tag+"\n"), 0o644)
}

func (m *Manager) getJSON(ctx context.Context, target string, out any) error {
	return release.GetJSON(ctx, m.client, "ReTouch/"+m.version, target, out)
}

func (m *Manager) downloadFile(ctx context.Context, target, path string, mode fs.FileMode) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	// Use the same SSRF-safe transport as the API lookups (m.client's 12s timeout
	// is too short for a binary, so this is a fresh client, not the shared one):
	// without it a 302 or DNS rebind on the artifact host could reach loopback/RFC1918.
	client := &http.Client{Timeout: downloadTimeout, Transport: release.SafeTransport()}
	return release.Download(ctx, client, "ReTouch/"+m.version, target, path, mode)
}

func (m *Manager) restart() {
	// os.Exit skips context cancellation, so give the host a chance to stop child
	// processes (plugins) that would otherwise be orphaned to init and duplicated
	// by the relaunched ReTouch.
	if m.beforeRestart != nil {
		m.beforeRestart()
	}
	start := filepath.Join(m.homeDir, "start.sh")
	if fileExists(start) {
		cmd := exec.Command("sh", "-c", "sleep 1; "+shellQuote(start)+" >/tmp/retouch-start.log 2>&1 &")
		if err := cmd.Start(); err != nil {
			m.log.Warn("schedule restart", "err", err)
			return
		}
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}
	m.log.Warn("start script missing after update", "path", start)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

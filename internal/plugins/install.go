package plugins

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stein155/retouch/internal/release"
)

// CatalogEntry describes a plugin ReTouch knows how to install: where its release
// lives and how to verify it. The curated catalog below is the trusted default; an
// advanced user may also install an off-catalog entry, in which case the ed25519
// PubKey is the only thing standing between them and an unsigned binary.
type CatalogEntry struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Repo        string `json:"repo"`   // owner/name
	Asset       string `json:"asset"`  // armv7l binary asset in the release
	PubKey      string `json:"pubkey"` // base64 ed25519 key; "" means checksum-only
}

// Catalog is the curated list of installable plugins. Kept in-code for now; it can
// later be fetched from a hosted index and merged with this default.
func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{
			Name:        "homekit",
			Title:       "Apple Home",
			Description: "Add this radio to Apple Home and control it with Siri. Adds HomeKit support, which isn't built into ReTouch by default.",
			Repo:        "stein155/retouch-homekit",
			Asset:       "retouch-homekit-armv7l",
		},
		{
			Name:        "ring",
			Title:       "Ring chime",
			Description: "Play a chime through this speaker when a Ring doorbell is pressed or motion is detected.",
			Repo:        "stein155/retouch-ring",
			Asset:       "retouch-ring-armv7l",
		},
	}
}

// LookupCatalog returns the curated entry for name.
func LookupCatalog(name string) (CatalogEntry, bool) {
	for _, e := range Catalog() {
		if e.Name == name {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

// releaseInfo is the sliver of the GitHub release API we read to resolve a tag.
type releaseInfo struct {
	TagName string `json:"tag_name"`
}

// LatestTag resolves the newest release tag for repo — the tag Install would pick
// with an empty tag. The UI uses it to offer an over-the-air update when it differs
// from the installed version.
func (m *Manager) LatestTag(ctx context.Context, repo string) (string, error) {
	var rel releaseInfo
	api := m.apiBase + "/repos/" + repo + "/releases/latest"
	if err := release.GetJSON(ctx, m.client, m.ua, api, &rel); err != nil {
		return "", fmt.Errorf("resolve latest release: %w", err)
	}
	tag := strings.TrimSpace(rel.TagName)
	if tag == "" {
		return "", fmt.Errorf("latest release of %s has no tag", repo)
	}
	return tag, nil
}

// Install downloads, verifies and records the plugin described by entry, then starts
// supervising it. tag targets a specific release; "" means the repo's latest.
// Verification mirrors ReTouch's own OTA: the binary must match its SHA256SUMS entry,
// and when a PubKey is set, SHA256SUMS itself must carry a valid ed25519 signature.
func (m *Manager) Install(ctx context.Context, entry CatalogEntry, tag string) error {
	m.installMu.Lock()
	defer m.installMu.Unlock()
	if entry.Name == "" || entry.Repo == "" || entry.Asset == "" {
		return fmt.Errorf("plugin entry missing name/repo/asset")
	}
	if strings.ContainsAny(entry.Name, "/\\.") {
		return fmt.Errorf("invalid plugin name %q", entry.Name)
	}
	if tag == "" {
		var err error
		if tag, err = m.LatestTag(ctx, entry.Repo); err != nil {
			return err
		}
	}

	dir := m.pluginDir(entry.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}
	base := m.dlBase + "/" + entry.Repo + "/releases/download/" + tag
	newBin := filepath.Join(dir, binName+".new")
	sums := filepath.Join(dir, "SHA256SUMS")

	if err := release.Download(ctx, m.client, m.ua, base+"/"+entry.Asset, newBin, 0o755); err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	if err := release.Download(ctx, m.client, m.ua, base+"/SHA256SUMS", sums, 0o644); err != nil {
		_ = os.Remove(newBin)
		return fmt.Errorf("download checksums: %w", err)
	}
	if entry.PubKey != "" {
		sig := filepath.Join(dir, "SHA256SUMS.sig")
		if err := release.Download(ctx, m.client, m.ua, base+"/SHA256SUMS.sig", sig, 0o644); err != nil {
			_ = os.Remove(newBin)
			return fmt.Errorf("download signature: %w", err)
		}
		if err := release.VerifySignature(entry.PubKey, sums, sig); err != nil {
			_ = os.Remove(newBin)
			return fmt.Errorf("verify signature: %w", err)
		}
	}
	want, err := release.ChecksumFor(sums, entry.Asset)
	if err != nil {
		_ = os.Remove(newBin)
		return err
	}
	if err := release.VerifyChecksum(newBin, want); err != nil {
		_ = os.Remove(newBin)
		return err
	}
	if err := os.Rename(newBin, filepath.Join(dir, binName)); err != nil {
		_ = os.Remove(newBin)
		return fmt.Errorf("install binary: %w", err)
	}

	inst := Installed{
		Name:    entry.Name,
		Repo:    entry.Repo,
		Asset:   entry.Asset,
		PubKey:  entry.PubKey,
		Version: tag,
		Enabled: true,
	}
	if err := m.record(inst); err != nil {
		return err
	}
	m.log.Info("plugin installed", "plugin", inst.Name, "version", tag)
	return nil
}

// InstallLocal installs a binary uploaded through the UI (a sideload), for plugins
// whose release repo is still private. It skips release verification — the operator
// is trusting a binary they built or downloaded themselves — and marks the plugin
// Sideloaded so the UI can show it can't be updated over the air.
func (m *Manager) InstallLocal(name string, r io.Reader) error {
	m.installMu.Lock()
	defer m.installMu.Unlock()
	if name == "" || strings.ContainsAny(name, "/\\.") {
		return fmt.Errorf("invalid plugin name %q", name)
	}
	dir := m.pluginDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}
	newBin := filepath.Join(dir, binName+".new")
	f, err := os.OpenFile(newBin, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(newBin)
		return fmt.Errorf("write binary: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(newBin)
		return closeErr
	}
	if err := os.Rename(newBin, filepath.Join(dir, binName)); err != nil {
		_ = os.Remove(newBin)
		return fmt.Errorf("install binary: %w", err)
	}
	inst := Installed{Name: name, Version: "sideloaded", Enabled: true, Sideloaded: true}
	if err := m.record(inst); err != nil {
		return err
	}
	m.log.Info("plugin sideloaded", "plugin", name)
	return nil
}

// record upserts inst into the manifest, persists it, and (re)starts its supervisor
// so a freshly installed binary comes up immediately.
func (m *Manager) record(inst Installed) error {
	m.mu.Lock()
	replaced := false
	for i := range m.state {
		if m.state[i].Name == inst.Name {
			m.state[i], replaced = inst, true
			break
		}
	}
	if !replaced {
		m.state = append(m.state, inst)
	}
	runCtx := m.runCtx
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("save plugin state: %w", err)
	}
	if runCtx != nil {
		m.startSupervisor(runCtx, inst.Name)
	}
	return nil
}

// Remove stops the plugin's child process, drops it from the manifest, and deletes
// its directory. It is a no-op (nil) if the plugin isn't installed.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	idx := -1
	for i := range m.state {
		if m.state[i].Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		m.mu.Unlock()
		return nil
	}
	var done chan struct{}
	if p := m.procs[name]; p != nil {
		if p.cancel != nil {
			p.cancel()
		}
		done = p.done
	}
	delete(m.procs, name)
	m.state = append(m.state[:idx], m.state[idx+1:]...)
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("save plugin state: %w", err)
	}
	// Wait for the child to actually be gone before deleting its directory: a dying
	// plugin still flushes its config/log on the way down, which would resurrect
	// plugins/<name>/ — with credentials in it — after the user removed it.
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			m.log.Warn("plugin slow to stop; removing its dir anyway", "plugin", name)
		}
	}
	if err := os.RemoveAll(m.pluginDir(name)); err != nil {
		return fmt.Errorf("remove plugin dir: %w", err)
	}
	m.log.Info("plugin removed", "plugin", name)
	return nil
}

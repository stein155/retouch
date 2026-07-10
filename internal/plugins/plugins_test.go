package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(t.TempDir(), "127.0.0.1:8090", "http://127.0.0.1:8000", "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestInstallAndRemove(t *testing.T) {
	asset := []byte("#!/bin/sh\necho fake plugin\n")
	sum := sha256.Sum256(asset)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			_, _ = w.Write([]byte(`{"tag_name":"v1"}`))
		case strings.HasSuffix(r.URL.Path, "/ring-armv7l"):
			_, _ = w.Write(asset)
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  ring-armv7l\n"))
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	m := testManager(t)
	// The install client normally refuses loopback (SafeTransport); the test server
	// is on 127.0.0.1, so swap in a plain client and point the bases at it.
	m.client = ts.Client()
	m.apiBase, m.dlBase = ts.URL, ts.URL

	entry := CatalogEntry{Name: "ring", Repo: "stein155/retouch-ring", Asset: "ring-armv7l"}
	if err := m.Install(context.Background(), entry, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	got := m.List()
	if len(got) != 1 || got[0].Name != "ring" || got[0].Version != "v1" {
		t.Fatalf("List after install = %+v", got)
	}
	if b, err := os.ReadFile(m.pluginDir("ring") + "/" + binName); err != nil || string(b) != string(asset) {
		t.Fatalf("installed binary wrong: err=%v content=%q", err, b)
	}

	// A tampered checksum must be rejected and leave nothing behind.
	bad := CatalogEntry{Name: "evil", Repo: "stein155/retouch-ring", Asset: "ring-armv7l"}
	m2 := testManager(t)
	m2.client = ts.Client()
	m2.apiBase = ts.URL
	m2.dlBase = ts.URL
	// Serve a SHA256SUMS that won't match by pointing at a server that lies.
	liar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			_, _ = w.Write([]byte(`{"tag_name":"v1"}`))
		case strings.HasSuffix(r.URL.Path, "/ring-armv7l"):
			_, _ = w.Write(asset)
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  ring-armv7l\n"))
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer liar.Close()
	m2.apiBase, m2.dlBase = liar.URL, liar.URL
	if err := m2.Install(context.Background(), bad, ""); err == nil {
		t.Fatal("Install accepted a checksum mismatch")
	}
	if len(m2.List()) != 0 {
		t.Fatal("failed install left state behind")
	}

	if err := m.Remove("ring"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(m.List()) != 0 {
		t.Fatalf("List after remove = %+v", m.List())
	}
	if _, err := os.Stat(m.pluginDir("ring")); !os.IsNotExist(err) {
		t.Fatalf("plugin dir still present after remove: %v", err)
	}
}

func TestLatestTag(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			_, _ = w.Write([]byte(`{"tag_name":"v1.0.1"}`))
			return
		}
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer ts.Close()

	m := testManager(t)
	m.client = ts.Client()
	m.apiBase = ts.URL

	tag, err := m.LatestTag(context.Background(), "stein155/retouch-homekit")
	if err != nil {
		t.Fatalf("LatestTag: %v", err)
	}
	if tag != "v1.0.1" {
		t.Fatalf("LatestTag = %q, want v1.0.1", tag)
	}
}

func TestProxyRewriteAndDown(t *testing.T) {
	var seenPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()
	port, _ := strconv.Atoi(mustPort(t, backend.URL))

	m := testManager(t)
	m.mu.Lock()
	m.state = []Installed{{Name: "ring", Enabled: true}}
	m.procs["ring"] = &proc{running: true, port: port}
	m.mu.Unlock()

	h, ok := m.Proxy("ring")
	if !ok {
		t.Fatal("Proxy returned ok=false for an installed plugin")
	}

	// A proxied request drops the /api/plugins/ring prefix before reaching the plugin.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/plugins/ring/manifest", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d", rec.Code)
	}
	if seenPath != "/manifest" {
		t.Fatalf("plugin saw path %q, want /manifest", seenPath)
	}

	// When the child isn't running, the proxy answers 503 rather than dialing a dead port.
	m.mu.Lock()
	m.procs["ring"].running = false
	m.mu.Unlock()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/plugins/ring/manifest", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("down proxy status = %d, want 503", rec.Code)
	}

	if _, ok := m.Proxy("nope"); ok {
		t.Fatal("Proxy returned ok=true for an uninstalled plugin")
	}
}

func mustPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

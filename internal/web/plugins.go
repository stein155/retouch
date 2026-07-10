package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/stein155/retouch/internal/plugins"
)

// pluginInstallTimeout bounds a plugin install (release lookup + binary download +
// verify). It runs on a background context so a browser navigating away can't cancel
// a half-finished install.
const pluginInstallTimeout = 5 * time.Minute

// listPlugins returns what's installed (with live run status) alongside the curated
// catalog of installable plugins, so the UI can render both in one call.
func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) {
	installed := []plugins.Status{}
	if s.plugins != nil {
		installed = s.plugins.List()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installed": installed,
		"catalog":   plugins.Catalog(),
		"sideload":  s.plugins != nil && s.sideload, // UI shows "install from file" only when enabled
	})
}

// installPlugin installs a curated plugin by name. An optional {"tag":"..."} body
// targets a specific release; the default is the plugin's latest.
func (s *Server) installPlugin(w http.ResponseWriter, r *http.Request) {
	if s.plugins == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "plugins are only available on an installed speaker"})
		return
	}
	name := r.PathValue("name")
	entry, ok := plugins.LookupCatalog(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown plugin " + name})
		return
	}
	var body struct {
		Tag string `json:"tag"`
	}
	if r.Body != nil {
		_ = decodeJSONBody(r, &body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pluginInstallTimeout)
	defer cancel()
	if err := s.plugins.Install(ctx, entry, body.Tag); err != nil {
		s.fail(w, "plugin install failed: "+err.Error(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "installed", "plugin": name})
}

// pluginLatestTimeout bounds the single release-API lookup behind /latest.
const pluginLatestTimeout = 15 * time.Second

// pluginLatest reports the newest available release tag for a curated plugin, so
// the UI can offer an over-the-air update when it differs from the installed
// version. A re-install (POST …/install) upgrades the binary in place, keeping the
// plugin's persisted state (e.g. HomeKit pairing).
func (s *Server) pluginLatest(w http.ResponseWriter, r *http.Request) {
	if s.plugins == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "plugins are only available on an installed speaker"})
		return
	}
	name := r.PathValue("name")
	entry, ok := plugins.LookupCatalog(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown plugin " + name})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pluginLatestTimeout)
	defer cancel()
	tag, err := s.plugins.LatestTag(ctx, entry.Repo)
	if err != nil {
		s.fail(w, "resolve latest release failed: "+err.Error(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"tag": tag})
}

// pluginUploadMax caps a sideloaded plugin binary. A plugin is a few MB; 32 MiB
// leaves headroom while bounding the write on the memory-constrained speaker.
const pluginUploadMax = 32 << 20

// uploadPlugin installs a locally-built plugin binary (multipart field "binary").
// This is the sideload path for plugins whose release repo is still private, so it
// skips release verification — which makes it, in effect, a run-arbitrary-code
// endpoint. The browser-focused guard can't stop a non-browser LAN client, so the
// endpoint stays 403 unless the operator opted in with -allow-sideload at start-up.
// The body is streamed straight into the plugin dir (never buffered through
// RAM-backed tmpfs, which a near-cap upload could exhaust).
func (s *Server) uploadPlugin(w http.ResponseWriter, r *http.Request) {
	if s.plugins == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "plugins are only available on an installed speaker"})
		return
	}
	if !s.sideload {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "sideloading is disabled; start ReTouch with -allow-sideload to enable it"})
		return
	}
	name := r.PathValue("name")
	mr, err := r.MultipartReader()
	if err != nil {
		s.badRequest(w, "expected a multipart upload", err)
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.badRequest(w, "malformed upload", err)
			return
		}
		if part.FormName() != "binary" {
			continue
		}
		if err := s.plugins.InstallLocal(name, part); err != nil {
			s.fail(w, "plugin sideload failed: "+err.Error(), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "installed", "plugin": name})
		return
	}
	s.badRequest(w, "missing binary upload", nil)
}

// removePlugin stops and deletes an installed plugin.
func (s *Server) removePlugin(w http.ResponseWriter, r *http.Request) {
	if s.plugins == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "plugins are only available on an installed speaker"})
		return
	}
	name := r.PathValue("name")
	if err := s.plugins.Remove(name); err != nil {
		s.fail(w, "plugin remove failed: "+err.Error(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "plugin": name})
}

// proxyPlugin forwards any other /api/plugins/<name>/… request to the plugin's own
// loopback API. The surrounding guard already applied the DNS-rebinding, CSRF and
// body-size checks, so the plugin sees only same-origin, size-bounded requests.
func (s *Server) proxyPlugin(w http.ResponseWriter, r *http.Request) {
	if s.plugins == nil {
		http.Error(w, "plugins unavailable", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	h, ok := s.plugins.Proxy(name)
	if !ok {
		http.Error(w, "unknown plugin", http.StatusNotFound)
		return
	}
	h.ServeHTTP(w, r)
}

// decodeJSONBody decodes a small JSON request body without pulling in a second copy
// of the limit logic; guard already caps the body at maxRequestBody.
func decodeJSONBody(r *http.Request, out any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(out)
}

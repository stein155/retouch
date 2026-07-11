package web

// The display API lets plugins put content on the SoundTouch 20 front-panel
// OLED without ever touching /dev/fb0 themselves — internal/display is the
// single writer and arbitrates notifications, standby screens and the
// firmware's own frame. Mutating endpoints are loopback-only: plugins run on
// the speaker itself, and nobody on the LAN should be able to scribble on
// the display.

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/stein155/retouch/internal/display"
)

// SetDisplay wires the OLED manager; nil (or never calling this) keeps the
// API responding with available:false and ignoring content.
func (s *Server) SetDisplay(m *display.Manager) { s.display = m }

// displayInfo tells a plugin whether this speaker has the ST20 panel, so it
// can hide display settings elsewhere.
func (s *Server) displayInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"available": s.display.Available()})
}

type displayBody struct {
	Owner   string   `json:"owner"`   // standby: who this screen belongs to
	Icon    string   `json:"icon"`    // built-in icon name
	Sprite  []string `json:"sprite"`  // optional custom sprite; overrides icon
	Text    string   `json:"text"`    // sentence under the icon
	Seconds int      `json:"seconds"` // notify: how long to show (default 8)
}

func (s *Server) displayNotify(w http.ResponseWriter, r *http.Request) {
	body, ok := s.displayReq(w, r)
	if !ok {
		return
	}
	s.display.Notify(display.Content{Icon: body.Icon, Sprite: body.Sprite, Text: body.Text}, time.Duration(body.Seconds)*time.Second)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) displaySetStandby(w http.ResponseWriter, r *http.Request) {
	body, ok := s.displayReq(w, r)
	if !ok {
		return
	}
	if body.Owner == "" {
		http.Error(w, `{"error":"owner required"}`, http.StatusBadRequest)
		return
	}
	s.display.SetStandby(body.Owner, display.Content{Icon: body.Icon, Sprite: body.Sprite, Text: body.Text})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) displayClearStandby(w http.ResponseWriter, r *http.Request) {
	if !s.loopbackOnly(w, r) {
		return
	}
	owner := r.URL.Query().Get("owner")
	if owner == "" {
		http.Error(w, `{"error":"owner required"}`, http.StatusBadRequest)
		return
	}
	s.display.ClearStandby(owner)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// displayReq applies the loopback check and decodes the body.
func (s *Server) displayReq(w http.ResponseWriter, r *http.Request) (displayBody, bool) {
	var body displayBody
	if !s.loopbackOnly(w, r) {
		return body, false
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&body); err != nil {
		http.Error(w, `{"error":"bad json"}`, http.StatusBadRequest)
		return body, false
	}
	return body, true
}

// loopbackOnly rejects callers that aren't local processes (plugins). The
// display is speaker-local hardware; the LAN has no business writing to it.
func (s *Server) loopbackOnly(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	http.Error(w, `{"error":"loopback only"}`, http.StatusForbidden)
	return false
}

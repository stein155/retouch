package web

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/stein155/retouch/internal/speaker"
)

// speakerNotifyBody is the JSON a caller (typically a plugin, via --host-url) POSTs
// to /api/speaker/notify. Only url is required; the rest tune volume and the
// now-playing text the speaker shows while the clip plays.
type speakerNotifyBody struct {
	URL    string `json:"url"`
	Volume int    `json:"volume"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Track  string `json:"track"`
	AppKey string `json:"appKey"`
}

// speakerNotify plays an audio notification on the speaker via the firmware's
// /speaker endpoint. Loopback-only: it is the plugin-facing entry point so
// afvalwijzer, ring and friends can play a clip by POSTing a URL, with the
// app_key, volume clamping and play_info XML handled here in the core.
func (s *Server) speakerNotify(w http.ResponseWriter, r *http.Request) {
	if !s.loopbackOnly(w, r) {
		return
	}
	var body speakerNotifyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "bad body", err)
		return
	}
	if body.URL == "" {
		http.Error(w, `{"error":"url required"}`, http.StatusBadRequest)
		return
	}
	// Notifications interrupt playback and can wake the speaker, so give the request
	// a bit more room than the default 4s speaker timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err := s.speaker.PlayNotification(ctx, speaker.Notification{
		URL:    body.URL,
		Volume: body.Volume,
		Artist: body.Artist,
		Album:  body.Album,
		Track:  body.Track,
		AppKey: body.AppKey,
	})
	if err != nil {
		s.fail(w, "notification failed", err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

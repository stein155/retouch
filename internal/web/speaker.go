package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stein155/retouch/internal/speaker"
)

// notifyAudioMax caps an uploaded notification clip. Clips are short chimes that
// ReTouch holds in RAM on the memory-constrained speaker, so this is deliberately
// tight — a few seconds of MP3/AAC is well under a megabyte; 4 MiB covers a generous
// clip while bounding what a buggy or hostile caller can make us allocate.
const notifyAudioMax = 4 << 20

// notifyClipTTL is how long an uploaded clip stays fetchable after upload. The
// firmware fetches it once, right after we POST /speaker; a couple of minutes covers
// a slow fetch or a retry, after which the clip is swept.
const notifyClipTTL = 2 * time.Minute

// maxAudioClips bounds how many uploaded clips we hold at once (oldest evicted first),
// a second backstop on memory beyond the per-clip size cap and the TTL sweep.
const maxAudioClips = 8

// audioClip is one uploaded notification clip, served back to the firmware on loopback.
type audioClip struct {
	data        []byte
	contentType string
	created     time.Time
	expires     time.Time
}

// speakerNotifyBody is the JSON form of /api/speaker/notify: play a URL the firmware
// fetches itself (an already-hosted clip, or a Google TTS URL — no hosting needed).
type speakerNotifyBody struct {
	URL    string `json:"url"`
	Volume int    `json:"volume"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Track  string `json:"track"`
	AppKey string `json:"appKey"`
}

// speakerNotify plays an audio notification via the firmware's /speaker endpoint.
// Loopback-only: it is the plugin-facing entry point so afvalwijzer, ring and friends
// can ring the speaker without re-implementing the app_key + play_info XML.
//
// Two shapes, by Content-Type:
//   - application/json {url,...}: play a URL the firmware fetches directly. Use this
//     for remote clips and for Google TTS (pass the translate_tts URL straight through).
//   - anything else: the body IS the audio file. ReTouch caches it and serves it back
//     to the firmware on loopback, so plugins can ship a local chime without hosting it.
//     Metadata comes from the query string (?volume=&artist=&album=&track=&appKey=).
func (s *Server) speakerNotify(w http.ResponseWriter, r *http.Request) {
	if !s.loopbackOnly(w, r) {
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		s.speakerNotifyURL(w, r)
		return
	}
	s.speakerNotifyUpload(w, r)
}

func (s *Server) speakerNotifyURL(w http.ResponseWriter, r *http.Request) {
	var body speakerNotifyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "bad body", err)
		return
	}
	if body.URL == "" {
		http.Error(w, `{"error":"url required"}`, http.StatusBadRequest)
		return
	}
	s.playNotification(w, r, speaker.Notification{
		URL: body.URL, Volume: body.Volume,
		Artist: body.Artist, Album: body.Album, Track: body.Track, AppKey: body.AppKey,
	})
}

func (s *Server) speakerNotifyUpload(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		// The guard caps the body at notifyAudioMax; a MaxBytesError means too large.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, `{"error":"audio too large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		s.badRequest(w, "read audio", err)
		return
	}
	if len(data) == 0 {
		http.Error(w, `{"error":"empty audio body"}`, http.StatusBadRequest)
		return
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}
	name := s.storeClip(data, ct)

	q := r.URL.Query()
	volume, _ := strconv.Atoi(q.Get("volume"))
	n := speaker.Notification{
		URL:    s.audioURL(name),
		Volume: volume,
		Artist: q.Get("artist"),
		Album:  q.Get("album"),
		Track:  q.Get("track"),
		AppKey: q.Get("appKey"),
	}
	if !s.playNotification(w, r, n) {
		// The firmware never got a URL it could fetch, so drop the clip now instead
		// of holding the bytes for the full TTL.
		s.dropClip(name)
	}
}

// playNotification runs the /speaker call and writes the HTTP response. It reports
// whether the request succeeded so callers can clean up on failure.
func (s *Server) playNotification(w http.ResponseWriter, r *http.Request, n speaker.Notification) bool {
	// Notifications interrupt playback and can wake the speaker, so allow more room
	// than the default 4s speaker timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := s.speaker.PlayNotification(ctx, n); err != nil {
		s.fail(w, "notification failed", err)
		return false
	}
	writeJSON(w, 200, map[string]any{"ok": true})
	return true
}

// serveAudio hands an uploaded clip back to the firmware. Loopback-only: the only
// client is the co-located firmware fetching over 127.0.0.1.
func (s *Server) serveAudio(w http.ResponseWriter, r *http.Request) {
	if !s.loopbackOnly(w, r) {
		return
	}
	name := r.PathValue("name")
	s.audioMu.Lock()
	clip := s.audioClips[name]
	s.audioMu.Unlock()
	if clip == nil || time.Now().After(clip.expires) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", clip.contentType)
	// ServeContent handles range requests, which the firmware's fetcher may use.
	http.ServeContent(w, r, name, clip.created, bytes.NewReader(clip.data))
}

// storeClip caches an uploaded clip and returns the URL name (id + extension) the
// firmware will fetch it under. Sweeps expired clips and bounds the total held.
func (s *Server) storeClip(data []byte, contentType string) string {
	now := time.Now()
	name := clipID() + extForContentType(contentType)
	s.audioMu.Lock()
	defer s.audioMu.Unlock()
	if s.audioClips == nil {
		s.audioClips = make(map[string]*audioClip)
	}
	for k, c := range s.audioClips {
		if now.After(c.expires) {
			delete(s.audioClips, k)
		}
	}
	for len(s.audioClips) >= maxAudioClips {
		oldest := ""
		for k, c := range s.audioClips {
			if oldest == "" || c.created.Before(s.audioClips[oldest].created) {
				oldest = k
			}
		}
		delete(s.audioClips, oldest)
	}
	s.audioClips[name] = &audioClip{data: data, contentType: contentType, created: now, expires: now.Add(notifyClipTTL)}
	return name
}

func (s *Server) dropClip(name string) {
	s.audioMu.Lock()
	delete(s.audioClips, name)
	s.audioMu.Unlock()
}

// audioURL builds the URL the firmware fetches an uploaded clip from. On-speaker the
// firmware and ReTouch share the host, so this is ReTouch's own loopback address.
func (s *Server) audioURL(name string) string {
	base := s.notifyBase
	if base == "" {
		base = "http://127.0.0.1:8000"
	}
	return base + "/api/speaker/audio/" + name
}

func clipID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// extForContentType maps the common Bose-supported audio types to a file extension,
// so the fetched URL carries one (some firmware paths key playback off it).
func extForContentType(ct string) string {
	switch {
	case strings.Contains(ct, "mpeg"), strings.Contains(ct, "mp3"):
		return ".mp3"
	case strings.Contains(ct, "aac"), strings.Contains(ct, "mp4"):
		return ".aac"
	case strings.Contains(ct, "wav"):
		return ".wav"
	case strings.Contains(ct, "flac"):
		return ".flac"
	case strings.Contains(ct, "ogg"), strings.Contains(ct, "vorbis"):
		return ".ogg"
	default:
		return ".mp3"
	}
}

package marge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// TuneIn resolves a station id to playable streams and (best-effort) its display
// metadata, so the firmware's now-playing shows the real name + logo.
type TuneIn interface {
	Resolve(ctx context.Context, stationID string) ([]string, error)
	Describe(ctx context.Context, stationID string) (name, logo string)
}

// NowPlayingSource returns the live "now playing" track line for a station id, if
// known. Used to put the current track (not just the station name) on the
// speaker's own display — see tuneinPlayback.
type NowPlayingSource interface {
	Track(stationID string) (string, bool)
}

// The BMX TuneIn playback response the firmware's TUNEIN worker expects when it
// fetches the registry baseUrl + /v1/playback/station/<id>. Field names/casing
// mirror the Bose wire format.
type bmxStream struct {
	HasPlaylist bool   `json:"hasPlaylist"`
	IsRealtime  bool   `json:"isRealtime"`
	StreamUrl   string `json:"streamUrl"`
}

type bmxAudio struct {
	HasPlaylist bool        `json:"hasPlaylist"`
	IsRealtime  bool        `json:"isRealtime"`
	StreamUrl   string      `json:"streamUrl"`
	Streams     []bmxStream `json:"streams"`
}

type bmxPlayback struct {
	Audio      bmxAudio `json:"audio"`
	ImageUrl   string   `json:"imageUrl"`
	Name       string   `json:"name"`
	StreamType string   `json:"streamType"`
}

// bmxTunein routes the /bmx/tunein/* calls the TUNEIN worker makes. The important
// one is playback/station/<id>, which resolves the live stream; the rest are
// init/no-op calls the worker expects to succeed (token, self, report).
func (s *Server) bmxTunein(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	rest := strings.TrimPrefix(p, "/bmx/tunein")
	switch {
	case strings.HasPrefix(rest, "/v1/playback/station/"):
		s.tuneinPlayback(w, r, strings.TrimPrefix(rest, "/v1/playback/station/"))
	case strings.HasPrefix(rest, "/v1/token"):
		s.jsonRaw(w, `{"access_token":"stlocal","refresh_token":"stlocal"}`)
	case rest == "" || rest == "/":
		// service self descriptor — minimal, the worker only checks it answers
		s.jsonRaw(w, `{"_links":{"self":{"href":"/"}}}`)
	default:
		// navigate / search / report / favorite — not needed for playback
		s.jsonRaw(w, `{}`)
	}
}

// tuneinPlayback resolves a station id to its live stream(s) via the injected
// resolver (TuneIn OPML) and returns the BMX playback document.
func (s *Server) tuneinPlayback(w http.ResponseWriter, r *http.Request, stationID string) {
	stationID = strings.Trim(stationID, "/")
	// Cut at any character that could smuggle extra path segments or query
	// parameters into the upstream Tune.ashx request ('&' would inject a second
	// query parameter, e.g. formats=hls).
	if i := strings.IndexAny(stationID, "/?&#%"); i >= 0 {
		stationID = stationID[:i]
	}
	if s.tunein == nil {
		http.Error(w, "no resolver", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	urls, err := s.tunein.Resolve(ctx, stationID)
	if err != nil || len(urls) == 0 {
		s.log.Warn("bmx tunein resolve failed", "station", stationID, "err", err)
		http.Error(w, "resolve failed", http.StatusBadGateway)
		return
	}
	name, logo := s.tunein.Describe(ctx, stationID) // best-effort metadata for now-playing
	// Put the live track on the speaker's own display when we can. The firmware
	// shows whatever Name this playback doc carries; historically the Bose cloud
	// pushed the rolling track here, but with the cloud gone the speaker only ever
	// saw the station name. We only swap Name for the live track once the firmware
	// has RE-FETCHED this station's playback doc — proof it polls for updates, so
	// the track will actually refresh rather than freeze at select-time. The first
	// fetch always keeps the station name, so this can never regress the display.
	if s.nowPlaying != nil && s.sawRepoll(stationID) {
		if track, ok := s.nowPlaying.Track(stationID); ok && track != "" {
			s.log.Info("bmx tunein now-playing on display", "station", stationID, "track", track)
			name = track
		}
	}
	streams := make([]bmxStream, 0, len(urls))
	for _, u := range urls {
		streams = append(streams, bmxStream{HasPlaylist: true, IsRealtime: true, StreamUrl: u})
	}
	resp := bmxPlayback{
		Audio:      bmxAudio{HasPlaylist: true, IsRealtime: true, StreamUrl: urls[0], Streams: streams},
		ImageUrl:   logo,
		Name:       name,
		StreamType: "liveRadio",
	}
	s.log.Info("bmx tunein playback", "station", stationID, "streams", len(urls))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) jsonRaw(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

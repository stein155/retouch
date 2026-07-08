package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/speaker"
)

// Live state pushed to browsers over Server-Sent Events (/api/events) so the UI
// updates the instant playback or volume changes, instead of each tab polling
// /api/now + /api/volume every few seconds. One hub polls the speaker on behalf
// of every connected browser and broadcasts only when the state actually
// changes, so N open tabs cost one poll loop, not N.
const (
	// eventsActive is the poll cadence while playback is in flux (buffering /
	// switching), so "starten → bufferen → live" lands quickly on screen.
	eventsActive = 1500 * time.Millisecond
	// eventsIdle is the calm cadence otherwise — still catches volume changes
	// from the physical buttons or another app within a couple of seconds.
	eventsIdle = 3 * time.Second
	// eventsHeartbeat keeps the SSE connection warm through proxies that would
	// otherwise time out an idle stream.
	eventsHeartbeat = 20 * time.Second
)

// snapshot is one push of live speaker state. Fields are omitted when a read
// failed so the browser keeps its last-known value instead of a zeroed one.
type snapshot struct {
	Now    *speaker.NowPlaying `json:"now,omitempty"`
	Volume *int                `json:"volume,omitempty"`
}

// hub fans one speaker poll loop out to every connected SSE client.
type hub struct {
	poll func(context.Context) (snapshot, bool) // reads the speaker; false on error
	log  *slog.Logger

	mu   sync.Mutex
	subs map[chan []byte]struct{}
	last []byte // most recent broadcast payload, replayed to new subscribers
	wake chan struct{}
}

func newHub(poll func(context.Context) (snapshot, bool), log *slog.Logger) *hub {
	return &hub{
		poll: poll,
		log:  log,
		subs: map[chan []byte]struct{}{},
		wake: make(chan struct{}, 1),
	}
}

// run drives the poll loop until ctx is cancelled. It idles (no speaker traffic)
// whenever no browser is connected, and polls promptly again on a nudge or when
// a new subscriber joins.
func (h *hub) run(ctx context.Context) {
	for {
		if h.subCount() == 0 {
			select {
			case <-ctx.Done():
				return
			case <-h.wake:
				continue
			}
		}
		interval := eventsIdle
		if snap, ok := h.poll(ctx); ok {
			if b, err := json.Marshal(snap); err == nil {
				h.broadcast(b)
			}
			if activeNow(snap.Now) {
				interval = eventsActive
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-h.wake: // a nudge (state just changed) or a new subscriber
		case <-time.After(interval):
		}
	}
}

// activeNow reports whether playback is mid-transition, so the loop polls faster
// until it settles.
func activeNow(np *speaker.NowPlaying) bool {
	return np != nil && np.PlayStatus == "BUFFERING_STATE"
}

func (h *hub) subCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// broadcast delivers a payload to every subscriber, remembering it for clients
// that connect later. Slow clients are skipped rather than allowed to stall the
// loop — they catch up on the next change.
func (h *hub) broadcast(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if bytes.Equal(b, h.last) {
		return // nothing changed; don't wake idle clients
	}
	h.last = b
	for ch := range h.subs {
		select {
		case ch <- b:
		default:
		}
	}
}

// subscribe registers a client. It replays the last known state immediately and
// nudges the loop to fetch a fresh reading, then returns an unsubscribe func.
func (h *hub) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 4)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	last := h.last
	h.mu.Unlock()
	if last != nil {
		ch <- last // buffered: safe, gives the tab something to show at once
	}
	h.nudge()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// nudge asks the loop to poll now instead of waiting for the next tick. Called
// after any action that changes playback or volume so the push is immediate.
func (h *hub) nudge() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// pollState reads the live speaker state the UI cares about. now-playing is
// required (its failure means the speaker is unreachable); volume is
// best-effort so a hiccup reading it doesn't blank the now-playing push.
func (s *Server) pollState(ctx context.Context) (snapshot, bool) {
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	np, err := s.speaker.NowPlaying(c)
	if err != nil {
		return snapshot{}, false
	}
	s.enrichNowPlaying(np)
	snap := snapshot{Now: np}
	if v, err := s.speaker.Volume(c); err == nil {
		snap.Volume = &v
	}
	return snap, true
}

// events streams live speaker state to one browser over Server-Sent Events.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // don't let a reverse proxy buffer the stream

	ch, unsubscribe := s.hub.subscribe()
	defer unsubscribe()

	fmt.Fprint(w, "retry: 3000\n\n") // tell EventSource how fast to reconnect
	fl.Flush()

	hb := time.NewTicker(eventsHeartbeat)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: state\ndata: %s\n\n", b)
			fl.Flush()
		case <-hb.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			fl.Flush()
		}
	}
}

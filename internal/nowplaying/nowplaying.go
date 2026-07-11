// Package nowplaying enriches the speaker's now-playing with the live song,
// artist and cover art. The speaker no longer receives track metadata (it came
// from the retired Bose cloud), so the current track is read from the standard
// ICY metadata on the station's own stream (internal/icy), falling back to
// TuneIn's Describe, with cover art looked up generically (internal/artwork).
// Both the web UI and the Home Assistant bridge consume the same Enricher, so
// the stream is read once per station, not once per consumer.
package nowplaying

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/artwork"
	"github.com/stein155/retouch/internal/icy"
	"github.com/stein155/retouch/internal/release"
	"github.com/stein155/retouch/internal/speaker"
	"github.com/stein155/retouch/internal/tunein"
)

// ttl is how long a now-playing lookup stays fresh. Songs change every few
// minutes, so a short cache keeps the line current without per-poll stream reads.
const ttl = 15 * time.Second

// streamURLTTL is how long a resolved stream URL is reused before re-resolving.
const streamURLTTL = 5 * time.Minute

// entry is a now-playing lookup cached briefly so the UI's poll (every few
// seconds) doesn't re-read the stream for the same station. fetching guards
// against launching a second background refresh while one is already in flight.
type entry struct {
	song, artist, art string
	at                time.Time
	fetching          bool
}

// urlEntry caches the stream URL resolved from TuneIn so each poll doesn't
// re-hit Tune.ashx; the URL (and any embedded token) is stable for a while.
type urlEntry struct {
	url string
	at  time.Time
}

// Enricher fills in live track metadata for playing stations. Create with New;
// safe for concurrent use.
type Enricher struct {
	speaker *speaker.Client
	tunein  *tunein.Client
	proxy   *http.Client // artwork lookups (public hosts only)
	stream  *http.Client // reads ICY metadata off the audio stream

	mu         sync.Mutex
	cache      map[string]entry    // now-playing, keyed by station id
	streamURLs map[string]urlEntry // resolved stream URL, keyed by station id
}

// New builds an Enricher. The outbound clients only dial public addresses
// (release.SafeTransport), since stream and artwork URLs come from third parties.
func New(sp *speaker.Client, tc *tunein.Client) *Enricher {
	return &Enricher{
		speaker:    sp,
		tunein:     tc,
		proxy:      &http.Client{Timeout: 12 * time.Second, Transport: release.SafeTransport()},
		stream:     &http.Client{Timeout: 12 * time.Second, Transport: release.SafeTransport()},
		cache:      map[string]entry{},
		streamURLs: map[string]urlEntry{},
	}
}

// NowPlaying reads the speaker's now-playing and enriches it — the same view
// the web UI shows. The Home Assistant bridge uses it so HA sees the track,
// not just the station name.
func (e *Enricher) NowPlaying(ctx context.Context) (*speaker.NowPlaying, error) {
	np, err := e.speaker.NowPlaying(ctx)
	if err != nil {
		return nil, err
	}
	e.Enrich(np)
	return np, nil
}

// Track returns the live "now playing" line for a TuneIn station id — "Artist -
// Song", or just "Song" when the artist is unknown or merely repeats the station
// — and whether one is known. Like Enrich it applies the cache and kicks off a
// background refresh when stale, so it stays fast; it returns ("", false) until
// the first lookup lands. Used to drive the track onto the speaker's own display
// via marge (the firmware only shows what the cloud playback doc carries).
func (e *Enricher) Track(id string) (string, bool) {
	if !strings.HasPrefix(id, "s") {
		return "", false
	}
	e.mu.Lock()
	ent, ok := e.cache[id]
	if (!ok || time.Since(ent.at) >= ttl) && !ent.fetching {
		ent.fetching = true
		e.cache[id] = ent
		go e.refresh(id)
	}
	e.mu.Unlock()
	if !ok || ent.song == "" {
		return "", false
	}
	if artist := strings.TrimSpace(ent.artist); artist != "" && !strings.EqualFold(artist, strings.TrimSpace(ent.song)) {
		return artist + " - " + ent.song, true
	}
	return ent.song, true
}

// Enrich fills in the song/artist/cover for a playing station. The read happens
// in the background so the caller's poll stays fast; this call only applies
// whatever is cached and kicks off a refresh when the entry is stale.
// Best-effort: with nothing cached yet the UI just shows the station until the
// next poll fills it in.
func (e *Enricher) Enrich(np *speaker.NowPlaying) {
	if np == nil {
		return
	}
	if np.Art == speaker.InternetRadioIcon {
		np.Art = ""
	}
	if !strings.HasPrefix(np.StationID, "s") {
		return
	}
	if np.PlayStatus != "" && np.PlayStatus != "PLAY_STATE" && np.PlayStatus != "BUFFERING_STATE" {
		return // nothing is playing — don't show a stale song
	}
	id := np.StationID
	e.mu.Lock()
	ent, ok := e.cache[id]
	if (!ok || time.Since(ent.at) >= ttl) && !ent.fetching {
		ent.fetching = true
		e.cache[id] = ent
		go e.refresh(id)
	}
	e.mu.Unlock()

	if !ok {
		return
	}
	// The speaker fills Track with the station name as a placeholder and never
	// knows the artist, so the stream's live song wins whenever it has one.
	if ent.song != "" {
		np.Track = ent.song
		np.Artist = ent.artist
		// Some streams (e.g. NPO) put the programme name where the artist goes;
		// when that just repeats the station, drop it rather than show it twice.
		if strings.EqualFold(strings.TrimSpace(np.Artist), strings.TrimSpace(np.Station)) {
			np.Artist = ""
		}
		// The speaker's own art is the generic station logo; our track cover is
		// more specific, so it takes priority when we have one.
		if ent.art != "" {
			np.Art = ent.art
		}
	} else if np.Art == "" && ent.art != "" {
		np.Art = ent.art
	}
}

// refresh reads the current track off the station's stream and looks up cover
// art, then stores the result. Runs in its own goroutine off a fresh context
// (the request that triggered it has already returned). Any failure caches an
// empty entry so the poll doesn't retry until the TTL lapses.
func (e *Enricher) refresh(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	// This goroutine parses untrusted stream/ICY/artwork data. If any of that
	// panics, clear the entry's `fetching` flag so the station isn't wedged
	// forever — the Enrich/Track guard skips a station while fetching is true, so
	// a leaked flag would stop it ever refreshing again (and exempt it from
	// eviction). Best-effort empty entry, retried after the TTL.
	defer func() {
		if r := recover(); r != nil {
			e.mu.Lock()
			e.cache[id] = entry{at: time.Now()}
			e.mu.Unlock()
		}
	}()

	var ent entry
	// Primary: the standard ICY metadata in the stream itself.
	if url := e.streamURL(ctx, id); url != "" {
		if title, err := icy.StreamTitle(ctx, e.stream, url); err != nil {
			e.invalidateStreamURL(id) // stale URL / expired token — re-resolve next time
		} else {
			ent.artist, ent.song = icy.SplitArtistTitle(title)
		}
	}
	// Fallback: TuneIn's Describe when the stream carried no track metadata.
	if ent.song == "" {
		if t, err := e.tunein.NowPlaying(ctx, id); err == nil && t.Song != "" {
			ent.song, ent.artist, ent.art = t.Song, t.Artist, t.Art
		}
	}
	// Cover art for whatever we found: look it up generically when we don't
	// already have one (ICY carries none; TuneIn sometimes does).
	if ent.song != "" && ent.art == "" {
		term := strings.TrimSpace(ent.artist + " " + ent.song)
		if art, err := artwork.Search(ctx, e.proxy, term); err == nil {
			ent.art = art
		}
	}
	ent.at = time.Now()

	e.mu.Lock()
	// The cache only needs the stations currently on screen; drop expired
	// entries once it grows past that so it can't accumulate for months.
	if len(e.cache) > 64 {
		for k, v := range e.cache {
			if k != id && time.Since(v.at) >= ttl && !v.fetching {
				delete(e.cache, k)
			}
		}
	}
	e.cache[id] = ent
	e.mu.Unlock()
}

// streamURL returns the station's playable stream URL, resolved via TuneIn and
// cached so each poll doesn't re-resolve. Empty on failure.
func (e *Enricher) streamURL(ctx context.Context, id string) string {
	e.mu.Lock()
	if u, ok := e.streamURLs[id]; ok && time.Since(u.at) < streamURLTTL {
		e.mu.Unlock()
		return u.url
	}
	e.mu.Unlock()

	urls, err := e.tunein.Resolve(ctx, id)
	if err != nil {
		return ""
	}
	u := tunein.PlayableURL(urls)
	if u == "" {
		return ""
	}
	e.mu.Lock()
	e.streamURLs[id] = urlEntry{url: u, at: time.Now()}
	e.mu.Unlock()
	return u
}

// invalidateStreamURL drops a cached stream URL so it is re-resolved next time.
func (e *Enricher) invalidateStreamURL(id string) {
	e.mu.Lock()
	delete(e.streamURLs, id)
	e.mu.Unlock()
}

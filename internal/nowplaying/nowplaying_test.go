package nowplaying

import (
	"testing"
	"time"

	"github.com/stein155/retouch/internal/speaker"
	"github.com/stein155/retouch/internal/tunein"
)

// seed puts a fresh cache entry in place so Enrich applies it without kicking
// off a background refresh (at is now, so the entry is not stale).
func seed(e *Enricher, id string, ent entry) {
	ent.at = time.Now()
	e.mu.Lock()
	e.cache[id] = ent
	e.mu.Unlock()
}

func TestEnrichAppliesCachedTrack(t *testing.T) {
	e := New(speaker.New("127.0.0.1"), tunein.New())
	seed(e, "s123", entry{song: "Song", artist: "Artist", art: "http://cover"})

	np := &speaker.NowPlaying{StationID: "s123", Station: "Radio X", Track: "Radio X", PlayStatus: "PLAY_STATE"}
	e.Enrich(np)
	if np.Track != "Song" || np.Artist != "Artist" || np.Art != "http://cover" {
		t.Fatalf("enriched = %q/%q/%q, want Song/Artist/http://cover", np.Track, np.Artist, np.Art)
	}
}

func TestEnrichDropsArtistEqualToStation(t *testing.T) {
	// Some streams put the programme/station name in the artist slot; Enrich
	// must not show the station twice.
	e := New(speaker.New("127.0.0.1"), tunein.New())
	seed(e, "s1", entry{song: "Song", artist: " radio x "})

	np := &speaker.NowPlaying{StationID: "s1", Station: "Radio X", PlayStatus: "PLAY_STATE"}
	e.Enrich(np)
	if np.Artist != "" {
		t.Fatalf("artist = %q, want dropped (repeats station)", np.Artist)
	}
}

func TestTrackLine(t *testing.T) {
	e := New(speaker.New("127.0.0.1"), tunein.New())

	// Artist + song -> "Artist - Song".
	seed(e, "s1", entry{song: "Song", artist: "Artist"})
	if got, ok := e.Track("s1"); !ok || got != "Artist - Song" {
		t.Errorf("Track = %q,%v want \"Artist - Song\",true", got, ok)
	}

	// Artist repeating the song -> just the song.
	seed(e, "s2", entry{song: "Song", artist: " song "})
	if got, ok := e.Track("s2"); !ok || got != "Song" {
		t.Errorf("Track = %q,%v want \"Song\",true", got, ok)
	}

	// No song cached, or non-TuneIn id -> not known.
	seed(e, "s3", entry{})
	if _, ok := e.Track("s3"); ok {
		t.Error("Track ok for empty song, want false")
	}
	if _, ok := e.Track("p100"); ok {
		t.Error("Track ok for non-TuneIn id, want false")
	}
}

func TestEnrichSkipsWhenNotPlaying(t *testing.T) {
	e := New(speaker.New("127.0.0.1"), tunein.New())
	seed(e, "s1", entry{song: "Song"})

	np := &speaker.NowPlaying{StationID: "s1", Station: "Radio X", Track: "Radio X", PlayStatus: "STOP_STATE"}
	e.Enrich(np)
	if np.Track != "Radio X" {
		t.Fatalf("track = %q, want untouched while stopped", np.Track)
	}

	// Non-TuneIn station ids (no "s" prefix) are never enriched.
	np = &speaker.NowPlaying{StationID: "p100", Track: "Local FM", PlayStatus: "PLAY_STATE"}
	e.Enrich(np)
	if np.Track != "Local FM" {
		t.Fatalf("track = %q, want untouched for non-TuneIn id", np.Track)
	}
}

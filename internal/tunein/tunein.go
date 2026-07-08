// Package tunein is a tiny client for TuneIn's public OPML API
// (opml.radiotime.com): search for stations, resolve a station id to its
// playable stream URLs, and describe the track currently playing. No key, no
// account. Now-playing is read primarily from the standard ICY stream metadata
// (internal/icy); NowPlaying here is the fallback for streams that carry none.
package tunein

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// base is the TuneIn OPML API root. It is a var (not a const) only so in-package
// tests can point the client at a local httptest server; production never sets it.
var base = "https://opml.radiotime.com"

// formats is the codec list we ask TuneIn for. The SoundTouch renderer cannot
// parse HLS (.m3u8) playlists, so HLS is intentionally excluded.
const formats = "mp3,aac"

// Station is a search result.
type Station struct {
	ID         string `json:"id"`         // TuneIn guide id, e.g. s6712
	Name       string `json:"name"`       // display name
	Logo       string `json:"logo"`       // station logo URL
	Bitrate    string `json:"bitrate"`    // kbit/s as reported, may be ""
	NowPlaying string `json:"nowPlaying"` // current track, may be ""
}

// Client talks to the TuneIn OPML API.
type Client struct{ http *http.Client }

// New returns a Client with a sane timeout.
func New() *Client { return &Client{http: &http.Client{Timeout: 10 * time.Second}} }

// searchResp mirrors the Search.ashx render=json shape (all fields are strings).
type searchResp struct {
	Body []struct {
		Element string `json:"element"`
		Type    string `json:"type"`
		Item    string `json:"item"`
		Text    string `json:"text"`
		GuideID string `json:"guide_id"`
		Image   string `json:"image"`
		Bitrate string `json:"bitrate"`
		Subtext string `json:"subtext"`
	} `json:"body"`
}

// Search returns matching radio stations for a free-text query.
func (c *Client) Search(ctx context.Context, query string) ([]Station, error) {
	u := fmt.Sprintf("%s/Search.ashx?query=%s&types=station&formats=%s&render=json",
		base, urlQueryEscape(query), formats)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var sr searchResp
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("tunein search decode: %w", err)
	}
	out := make([]Station, 0, len(sr.Body))
	for _, o := range sr.Body {
		// Keep only playable stations (skip "link" browse categories).
		if o.Item != "station" || !strings.HasPrefix(o.GuideID, "s") {
			continue
		}
		out = append(out, Station{
			ID:         o.GuideID,
			Name:       o.Text,
			Logo:       o.Image,
			Bitrate:    o.Bitrate,
			NowPlaying: o.Subtext,
		})
	}
	return out, nil
}

// Describe returns a station's display name and logo URL for a guide id, via
// Describe.ashx. Best-effort: returns empty strings (no error) when unavailable.
func (c *Client) Describe(ctx context.Context, stationID string) (name, logo string) {
	u := fmt.Sprintf("%s/Describe.ashx?id=%s&render=json", base, urlQueryEscape(stationID))
	body, err := c.get(ctx, u)
	if err != nil {
		return "", ""
	}
	var dr struct {
		Body []struct {
			Name string `json:"name"`
			Logo string `json:"logo"`
		} `json:"body"`
	}
	if err := json.Unmarshal(body, &dr); err != nil || len(dr.Body) == 0 {
		return "", ""
	}
	return dr.Body[0].Name, dr.Body[0].Logo
}

// boolish decodes a TuneIn flag that may arrive as a JSON bool (is_music) or a
// quoted string ("true"). Anything truthy decodes to true.
type boolish bool

func (b *boolish) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	*b = boolish(s == "true" || s == "1")
	return nil
}

// Track is the song currently playing on a station, as TuneIn reports it. Any
// field may be empty; Song+Artist are the useful pair for a "now playing" line.
type Track struct {
	Song   string `json:"song"`   // current track title
	Artist string `json:"artist"` // current artist
	Album  string `json:"album"`  // current album, often empty
	Art    string `json:"art"`    // album/artist cover URL, often empty
	Logo   string `json:"logo"`   // station logo (fallback cover)
	IsLive bool   `json:"isLive"` // station carries live song metadata
}

// NowPlaying returns the track currently playing on a station via Describe.ashx.
// It is the fallback for the ICY stream reader (internal/icy): many stations
// carry no in-stream metadata, and TuneIn sometimes still knows the song.
// Best-effort: returns a zero Track (no error) when nothing is available.
func (c *Client) NowPlaying(ctx context.Context, stationID string) (Track, error) {
	u := fmt.Sprintf("%s/Describe.ashx?id=%s&render=json", base, urlQueryEscape(stationID))
	body, err := c.get(ctx, u)
	if err != nil {
		return Track{}, err
	}
	var dr struct {
		Body []struct {
			IsMusic     boolish `json:"is_music"`
			HasSong     boolish `json:"has_song"`
			Logo        string  `json:"logo"`
			CurrentSong string  `json:"current_song"`
			Artist      string  `json:"current_artist"`
			Album       string  `json:"current_album"`
			AlbumArt    string  `json:"current_album_art"`
			ArtistArt   string  `json:"current_artist_art"`
		} `json:"body"`
	}
	if err := json.Unmarshal(body, &dr); err != nil || len(dr.Body) == 0 {
		return Track{}, nil
	}
	b := dr.Body[0]
	art := b.AlbumArt
	if art == "" {
		art = b.ArtistArt
	}
	return Track{
		Song:   strings.TrimSpace(b.CurrentSong),
		Artist: strings.TrimSpace(b.Artist),
		Album:  strings.TrimSpace(b.Album),
		Art:    art,
		Logo:   b.Logo,
		IsLive: bool(b.IsMusic) || bool(b.HasSong),
	}, nil
}

// Resolve returns the stream URLs for a station id, in TuneIn's order.
func (c *Client) Resolve(ctx context.Context, stationID string) ([]string, error) {
	u := fmt.Sprintf("%s/Tune.ashx?id=%s&formats=%s", base, urlQueryEscape(stationID), formats)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var urls []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			urls = append(urls, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("tunein: read streams for %s: %w", stationID, err)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("tunein: no streams for %s", stationID)
	}
	return urls, nil
}

// PlayableURL picks the best stream URL for the SoundTouch renderer: a plain
// http:// stream is preferred because the speaker's renderer is unreliable with
// modern HTTPS. Falls back to the first URL.
func PlayableURL(urls []string) string {
	for _, u := range urls {
		if strings.HasPrefix(u, "http://") {
			return u
		}
	}
	if len(urls) > 0 {
		return urls[0]
	}
	return ""
}

func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	if !strings.HasPrefix(u, base+"/") {
		return nil, fmt.Errorf("refusing non-TuneIn host: %s", u)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ReTouch/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunein status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256*1024))
}

// urlQueryEscape is a minimal query escaper (avoids importing net/url just for
// one call and keeps spaces/'&' safe). '%' must be escaped too, or a literal
// percent in the query (e.g. "100% NL") yields an invalid escape sequence;
// Replacer works in a single pass, so the '%' of the other replacements is safe.
func urlQueryEscape(s string) string {
	r := strings.NewReplacer("%", "%25", " ", "%20", "&", "%26", "?", "%3F", "#", "%23", "+", "%2B", "=", "%3D")
	return r.Replace(s)
}

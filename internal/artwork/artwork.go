// Package artwork looks up cover art for a track via the public iTunes Search
// API. It is deliberately station-agnostic: given an "artist title" string
// (e.g. from an ICY StreamTitle) it returns an album-cover URL, so ReTouch can
// show cover art for any station without a per-broadcaster integration. No key,
// no account.
package artwork

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// base is the iTunes Search API root. A var only so tests can point at a local
// server; production never sets it.
var base = "https://itunes.apple.com"

// Search returns a cover-art URL for the given "artist title" term, or "" (nil
// error) when nothing matches — an expected outcome, not a failure. The image
// is requested at 600×600; iTunes serves any size on the same path.
func Search(ctx context.Context, hc *http.Client, term string) (string, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return "", nil
	}
	u := fmt.Sprintf("%s/search?media=music&entity=song&limit=1&term=%s", base, url.QueryEscape(term))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ReTouch/1.0")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("artwork: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return "", err
	}
	var sr struct {
		Results []struct {
			ArtworkURL100 string `json:"artworkUrl100"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &sr); err != nil || len(sr.Results) == 0 {
		return "", nil
	}
	art := sr.Results[0].ArtworkURL100
	if art == "" {
		return "", nil
	}
	// The API returns a 100×100 thumbnail; the same path serves larger sizes.
	return strings.Replace(art, "100x100", "600x600", 1), nil
}

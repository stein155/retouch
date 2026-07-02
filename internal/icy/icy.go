// Package icy reads "now playing" metadata straight from a Shoutcast/Icecast
// audio stream. Almost every internet-radio stream interleaves a small text
// block (the ICY protocol) carrying the current StreamTitle — usually
// "Artist - Title" — so this is the one station-agnostic way to know what is on
// air without a per-broadcaster API. It carries no cover art (title text only);
// see internal/artwork for that.
package icy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// maxMetaInt bounds how many audio bytes we are willing to read before a
// metadata block, so a stream advertising an absurd icy-metaint can't make us
// download megabytes. Real streams use 1k–64k.
const maxMetaInt = 512 << 10 // 512 KiB

// maxBlocks caps how many metadata blocks we read while waiting for a non-empty
// title. Icecast sends the current title in the first block after connect, so
// one is normally enough; a couple more cover the rare empty-first-block case.
const maxBlocks = 3

// StreamTitle opens streamURL with ICY metadata enabled and returns the first
// non-empty StreamTitle it sees (e.g. "Artist - Title"). It returns "" with a
// nil error when the stream carries no metadata — that is an expected outcome,
// not a failure. hc must follow redirects (streamtheworld and friends 302 to a
// real edge); http.DefaultClient does.
func StreamTitle(ctx context.Context, hc *http.Client, streamURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "ReTouch/1.0")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("icy: status %d", resp.StatusCode)
	}
	metaint, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("icy-metaint")))
	if err != nil || metaint <= 0 || metaint > maxMetaInt {
		return "", nil // stream does not interleave metadata
	}

	var lenBuf [1]byte
	for b := 0; b < maxBlocks; b++ {
		// Skip the audio bytes up to the next metadata block.
		if _, err := io.CopyN(io.Discard, resp.Body, int64(metaint)); err != nil {
			return "", err
		}
		// One length byte in 16-byte units, then that many bytes of metadata.
		if _, err := io.ReadFull(resp.Body, lenBuf[:]); err != nil {
			return "", err
		}
		n := int(lenBuf[0]) * 16
		if n == 0 {
			continue // no metadata in this block; try the next
		}
		meta := make([]byte, n)
		if _, err := io.ReadFull(resp.Body, meta); err != nil {
			return "", err
		}
		if title := parseStreamTitle(string(meta)); title != "" {
			return title, nil
		}
	}
	return "", nil
}

// parseStreamTitle pulls the value out of a metadata block that looks like
// "StreamTitle='Artist - Title';StreamUrl='...';". Returns "" when absent.
func parseStreamTitle(meta string) string {
	const key = "StreamTitle='"
	i := strings.Index(meta, key)
	if i < 0 {
		return ""
	}
	rest := meta[i+len(key):]
	// The value ends at the closing "';"; fall back to a lone "'" for the last
	// field, which some servers don't terminate with a semicolon.
	end := strings.Index(rest, "';")
	if end < 0 {
		end = strings.IndexByte(rest, '\'')
	}
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// SplitArtistTitle splits a StreamTitle on the first " - " into artist and
// title, the near-universal convention. When there is no separator the whole
// string is the title and artist is empty.
func SplitArtistTitle(streamTitle string) (artist, title string) {
	s := strings.TrimSpace(streamTitle)
	if i := strings.Index(s, " - "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+3:])
	}
	return "", s
}

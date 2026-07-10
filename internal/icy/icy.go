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
	"unicode/utf8"
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
	return strings.TrimSpace(decodeText(rest[:end]))
}

// decodeText normalises a StreamTitle value to UTF-8. The ICY spec nominally
// wants UTF-8, but many stations send the block in Windows-1252 (or plain
// Latin-1), so a byte like 0xE9 ("é") would otherwise be invalid UTF-8 and
// render as the replacement character. Valid UTF-8 is returned untouched;
// anything else is decoded byte-for-byte through Windows-1252.
func decodeText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if r, ok := win1252[s[i]]; ok {
			b.WriteRune(r)
		} else {
			// Bytes outside the 0x80–0x9F block map straight to the matching
			// Unicode code point (Latin-1 == the low Unicode range).
			b.WriteRune(rune(s[i]))
		}
	}
	return b.String()
}

// win1252 holds the Windows-1252 code points that differ from Latin-1, all in
// the 0x80–0x9F range (curly quotes, dashes, the euro sign, and friends).
var win1252 = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡',
	0x88: 'ˆ', 0x89: '‰', 0x8A: 'Š', 0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž', 0x91: '‘',
	0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—', 0x98: '˜',
	0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ', 0x9E: 'ž', 0x9F: 'Ÿ',
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

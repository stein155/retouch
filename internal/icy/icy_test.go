package icy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestParseStreamTitle(t *testing.T) {
	tests := []struct {
		name, meta, want string
	}{
		{"typical", "StreamTitle='Charles & Eddie - Would I Lie To You';StreamUrl='';", "Charles & Eddie - Would I Lie To You"},
		{"trailing nuls trimmed", "StreamTitle='Song';\x00\x00\x00", "Song"},
		{"no semicolon terminator", "StreamTitle='Song'", "Song"},
		{"empty value", "StreamTitle='';StreamUrl='';", ""},
		{"absent", "StreamUrl='x';", ""},
		{"latin-1 é", "StreamTitle='Andr\xe9 Hazes Jr';", "André Hazes Jr"},
		{"windows-1252 em dash", "StreamTitle='A \x96 B';", "A – B"},
		{"utf-8 kept as-is", "StreamTitle='André';", "André"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseStreamTitle(tt.meta); got != tt.want {
				t.Errorf("parseStreamTitle(%q) = %q, want %q", tt.meta, got, tt.want)
			}
		})
	}
}

func TestSplitArtistTitle(t *testing.T) {
	tests := []struct {
		in, artist, title string
	}{
		{"Charles & Eddie - Would I Lie To You", "Charles & Eddie", "Would I Lie To You"},
		{"  A  -  B  ", "A", "B"},
		{"NPO Radio 2 - De Wild - PowNed", "NPO Radio 2", "De Wild - PowNed"}, // splits on first " - "
		{"Just A Title", "", "Just A Title"},
		{"", "", ""},
	}
	for _, tt := range tests {
		artist, title := SplitArtistTitle(tt.in)
		if artist != tt.artist || title != tt.title {
			t.Errorf("SplitArtistTitle(%q) = (%q, %q), want (%q, %q)", tt.in, artist, title, tt.artist, tt.title)
		}
	}
}

// icyBody builds a valid ICY stream body: metaint audio bytes, a length byte,
// then the metadata block padded to a 16-byte multiple.
func icyBody(metaint int, meta string) []byte {
	audio := strings.Repeat("A", metaint)
	blocks := (len(meta) + 15) / 16
	padded := meta + strings.Repeat("\x00", blocks*16-len(meta))
	return append([]byte(audio), append([]byte{byte(blocks)}, []byte(padded)...)...)
}

func TestStreamTitle(t *testing.T) {
	const metaint = 32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Icy-MetaData") != "1" {
			t.Errorf("missing Icy-MetaData header")
		}
		w.Header().Set("icy-metaint", strconv.Itoa(metaint))
		_, _ = w.Write(icyBody(metaint, "StreamTitle='A - B';"))
	}))
	defer srv.Close()

	got, err := StreamTitle(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("StreamTitle: %v", err)
	}
	if got != "A - B" {
		t.Errorf("StreamTitle = %q, want %q", got, "A - B")
	}
}

func TestStreamTitleSkipsEmptyBlock(t *testing.T) {
	const metaint = 16
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("icy-metaint", strconv.Itoa(metaint))
		// First block empty (length 0), second carries the title.
		body := append([]byte(strings.Repeat("A", metaint)), 0)
		body = append(body, icyBody(metaint, "StreamTitle='Song';")...)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := StreamTitle(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("StreamTitle: %v", err)
	}
	if got != "Song" {
		t.Errorf("StreamTitle = %q, want %q", got, "Song")
	}
}

func TestStreamTitleNoMetaint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("audio"))
	}))
	defer srv.Close()

	got, err := StreamTitle(context.Background(), srv.Client(), srv.URL)
	if err != nil || got != "" {
		t.Errorf("got (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestStreamTitleNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	if _, err := StreamTitle(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("expected error on non-200")
	}
}

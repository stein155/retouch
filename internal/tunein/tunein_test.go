package tunein

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// withServer points the package base URL at a test server for the duration of fn,
// restoring it afterward. The handler receives every request the client makes.
func withServer(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	old := base
	base = srv.URL
	return New(), func() {
		base = old
		srv.Close()
	}
}

func TestSearch(t *testing.T) {
	const body = `{
	  "head": {"status": "200"},
	  "body": [
	    {"element":"outline","type":"audio","item":"station","text":"BBC Radio 1","guide_id":"s24939","image":"http://img/r1.png","bitrate":"128","subtext":"Now: A Song"},
	    {"element":"outline","type":"link","item":"link","text":"More Stations","guide_id":"c57943"},
	    {"element":"outline","type":"audio","item":"station","text":"Bad GuideID","guide_id":"p999","image":"","bitrate":"","subtext":""},
	    {"element":"outline","type":"audio","item":"station","text":"NPO Radio 2","guide_id":"s6712","image":"http://img/r2.png","bitrate":"320","subtext":""}
	  ]
	}`

	var gotPath, gotRawQuery string
	c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	})
	defer done()

	got, err := c.Search(context.Background(), "radio & jazz?")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	want := []Station{
		{ID: "s24939", Name: "BBC Radio 1", Logo: "http://img/r1.png", Bitrate: "128", NowPlaying: "Now: A Song"},
		{ID: "s6712", Name: "NPO Radio 2", Logo: "http://img/r2.png", Bitrate: "320", NowPlaying: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Search stations = %#v, want %#v", got, want)
	}

	// URL construction: correct endpoint and escaped query.
	if gotPath != "/Search.ashx" {
		t.Errorf("path = %q, want /Search.ashx", gotPath)
	}
	// Raw query keeps the manual escaping from urlQueryEscape (e.g. %26, %3F).
	if !strings.Contains(gotRawQuery, "query=radio%20%26%20jazz%3F") {
		t.Errorf("raw query = %q, want escaped query=radio%%20%%26%%20jazz%%3F", gotRawQuery)
	}
	// Server-side parsed form should round-trip the escapes back to the original.
	if q := mustParseQuery(t, gotRawQuery).Get("query"); q != "radio & jazz?" {
		t.Errorf("decoded query = %q, want %q", q, "radio & jazz?")
	}
	if f := mustParseQuery(t, gotRawQuery).Get("formats"); f != formats {
		t.Errorf("formats = %q, want %q", f, formats)
	}
	if ty := mustParseQuery(t, gotRawQuery).Get("types"); ty != "station" {
		t.Errorf("types = %q, want station", ty)
	}
	if rn := mustParseQuery(t, gotRawQuery).Get("render"); rn != "json" {
		t.Errorf("render = %q, want json", rn)
	}
}

func TestSearchErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
		wantLen int
	}{
		{name: "bad json", status: 200, body: `not json at all`, wantErr: true},
		{name: "non-200", status: 500, body: `{}`, wantErr: true},
		{name: "empty body array", status: 200, body: `{"body":[]}`, wantErr: false, wantLen: 0},
		{name: "no station items", status: 200, body: `{"body":[{"item":"link","guide_id":"c1"}]}`, wantErr: false, wantLen: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			defer done()

			got, err := c.Search(context.Background(), "x")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%#v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	const body = `{"body":[{"name":"BBC Radio 1","logo":"http://img/r1.png"}]}`
	var gotPath, gotQuery string
	c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	})
	defer done()

	name, logo := c.Describe(context.Background(), "s24939")
	if name != "BBC Radio 1" || logo != "http://img/r1.png" {
		t.Errorf("Describe = (%q, %q), want (BBC Radio 1, http://img/r1.png)", name, logo)
	}
	if gotPath != "/Describe.ashx" {
		t.Errorf("path = %q, want /Describe.ashx", gotPath)
	}
	if id := mustParseQuery(t, gotQuery).Get("id"); id != "s24939" {
		t.Errorf("id = %q, want s24939", id)
	}
}

func TestDescribeBestEffort(t *testing.T) {
	// Describe swallows all failures and returns empty strings (no error path).
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "non-200", status: 500, body: `{}`},
		{name: "bad json", status: 200, body: `}{`},
		{name: "empty body", status: 200, body: `{"body":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			defer done()

			name, logo := c.Describe(context.Background(), "s1")
			if name != "" || logo != "" {
				t.Errorf("Describe = (%q, %q), want empty", name, logo)
			}
		})
	}
}

func TestNowPlaying(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		want   Track
	}{
		{
			name: "album art, is_music as bool true",
			body: `{"body":[{"is_music":true,"has_song":"false","logo":"http://img/l.png",
			        "current_song":" Song ","current_artist":" Artist ","current_album":" Album ",
			        "current_album_art":"http://img/album.png","current_artist_art":"http://img/artist.png"}]}`,
			status: 200,
			want: Track{Song: "Song", Artist: "Artist", Album: "Album",
				Art: "http://img/album.png", Logo: "http://img/l.png", IsLive: true},
		},
		{
			name: "falls back to artist art, has_song quoted true",
			body: `{"body":[{"is_music":"false","has_song":"true","logo":"http://img/l.png",
			        "current_song":"S","current_artist":"A","current_album":"",
			        "current_album_art":"","current_artist_art":"http://img/artist.png"}]}`,
			status: 200,
			want: Track{Song: "S", Artist: "A", Album: "",
				Art: "http://img/artist.png", Logo: "http://img/l.png", IsLive: true},
		},
		{
			name:   "not live: both flags false",
			body:   `{"body":[{"is_music":"false","has_song":false,"current_song":"S","current_artist":"A"}]}`,
			status: 200,
			want:   Track{Song: "S", Artist: "A", IsLive: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			defer done()

			got, err := c.NowPlaying(context.Background(), "s1")
			if err != nil {
				t.Fatalf("NowPlaying: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NowPlaying = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNowPlayingBestEffort(t *testing.T) {
	// Bad JSON / empty body -> zero Track, no error. Transport error -> error.
	t.Run("bad json is zero track no error", func(t *testing.T) {
		c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`}{`))
		})
		defer done()
		got, err := c.NowPlaying(context.Background(), "s1")
		if err != nil || !reflect.DeepEqual(got, Track{}) {
			t.Errorf("got (%#v, %v), want (zero, nil)", got, err)
		}
	})
	t.Run("non-200 returns error", func(t *testing.T) {
		c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(503)
		})
		defer done()
		if _, err := c.NowPlaying(context.Background(), "s1"); err == nil {
			t.Error("expected error on non-200")
		}
	})
}

func TestResolve(t *testing.T) {
	const body = "http://stream.example/1.mp3\r\n" +
		"#comment line\n" +
		"  https://stream.example/2.aac  \n" +
		"ftp://ignored\n" +
		"\n"
	var gotPath, gotQuery string
	c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	})
	defer done()

	urls, err := c.Resolve(context.Background(), "s6712")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"http://stream.example/1.mp3", "https://stream.example/2.aac"}
	if !reflect.DeepEqual(urls, want) {
		t.Errorf("Resolve = %#v, want %#v", urls, want)
	}
	if gotPath != "/Tune.ashx" {
		t.Errorf("path = %q, want /Tune.ashx", gotPath)
	}
	q := mustParseQuery(t, gotQuery)
	if q.Get("id") != "s6712" {
		t.Errorf("id = %q, want s6712", q.Get("id"))
	}
	if q.Get("formats") != formats {
		t.Errorf("formats = %q, want %q", q.Get("formats"), formats)
	}
}

func TestResolveErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "no stream lines", status: 200, body: "#just a comment\nnot a url\n"},
		{name: "empty body", status: 200, body: ""},
		{name: "non-200", status: 404, body: "http://x/y.mp3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			defer done()
			if _, err := c.Resolve(context.Background(), "s1"); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestPlayableURL(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{name: "prefers http over https", in: []string{"https://a", "http://b", "http://c"}, want: "http://b"},
		{name: "falls back to first when no http", in: []string{"https://a", "https://b"}, want: "https://a"},
		{name: "empty", in: nil, want: ""},
		{name: "single https", in: []string{"https://only"}, want: "https://only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlayableURL(tt.in); got != tt.want {
				t.Errorf("PlayableURL(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestURLQueryEscape(t *testing.T) {
	cases := map[string]string{
		"hello world": "hello%20world",
		"a&b":         "a%26b",
		"q?":          "q%3F",
		"c#d":         "c%23d",
		"a+b":         "a%2Bb",
		"plain":       "plain",
	}
	for in, want := range cases {
		if got := urlQueryEscape(in); got != want {
			t.Errorf("urlQueryEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetRejectsNonTuneInHost(t *testing.T) {
	// The get() guard must refuse any URL not under the (test) base.
	c, done := withServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	defer done()
	_, err := c.get(context.Background(), "http://evil.example/Search.ashx")
	if err == nil || !strings.Contains(err.Error(), "refusing non-TuneIn host") {
		t.Errorf("get(evil host) err = %v, want refusing non-TuneIn host", err)
	}
}

func mustParseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", raw, err)
	}
	return v
}

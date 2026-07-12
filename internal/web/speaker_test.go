package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doLoopbackCT is like do but presents a loopback RemoteAddr (so it clears the
// loopbackOnly guard) and a caller-chosen Content-Type.
func doLoopbackCT(t *testing.T, h http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:8000"
	req.RemoteAddr = "127.0.0.1:54321"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSpeakerNotifyURL(t *testing.T) {
	h, sp := newServer(t)

	rec := doLoopbackCT(t, h, "POST", "/api/speaker/notify", "application/json",
		`{"url":"http://translate.google.com/translate_tts?q=hoi","volume":40,"artist":"Afvalwijzer","track":"Morgen: GFT"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %q; want 200", rec.Code, rec.Body.String())
	}
	n := sp.LastNotification()
	if n == nil {
		t.Fatal("sim recorded no notification")
	}
	// A URL is passed straight through to the firmware (TTS / remote clips).
	if n.URL != "http://translate.google.com/translate_tts?q=hoi" || n.Volume != 40 {
		t.Errorf("url/volume wrong: %+v", n)
	}
	if n.Artist != "Afvalwijzer" || n.Track != "Morgen: GFT" {
		t.Errorf("metadata wrong: %+v", n)
	}
	if n.AppKey == "" {
		t.Error("app_key not defaulted")
	}
}

func TestSpeakerNotifyUploadRoundTrip(t *testing.T) {
	h, sp := newServer(t)

	const clip = "ID3fake-mp3-bytes"
	rec := doLoopbackCT(t, h, "POST", "/api/speaker/notify?volume=30&artist=Ring&track=Voordeur",
		"audio/mpeg", clip)
	if rec.Code != 200 {
		t.Fatalf("upload status = %d, body %q; want 200", rec.Code, rec.Body.String())
	}
	n := sp.LastNotification()
	if n == nil {
		t.Fatal("sim recorded no notification")
	}
	// The firmware is handed a ReTouch-hosted loopback URL, not the raw bytes.
	if !strings.Contains(n.URL, "/api/speaker/audio/") || !strings.HasSuffix(n.URL, ".mp3") {
		t.Errorf("notify URL not a hosted clip: %q", n.URL)
	}
	if n.Volume != 30 || n.Artist != "Ring" || n.Track != "Voordeur" {
		t.Errorf("metadata wrong: %+v", n)
	}

	// The hosted clip must be fetchable (this is what the firmware does).
	path := n.URL[strings.Index(n.URL, "/api/speaker/audio/"):]
	got := doLoopbackCT(t, h, "GET", path, "", "")
	if got.Code != 200 || got.Body.String() != clip {
		t.Fatalf("audio fetch: status %d body %q; want 200 %q", got.Code, got.Body.String(), clip)
	}
	if ct := got.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("audio content-type = %q, want audio/mpeg", ct)
	}
}

func TestSpeakerNotifyUploadEmpty(t *testing.T) {
	h, _ := newServer(t)
	rec := doLoopbackCT(t, h, "POST", "/api/speaker/notify", "audio/mpeg", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSpeakerNotifyRejectsEmptyURL(t *testing.T) {
	h, _ := newServer(t)
	rec := doLoopbackCT(t, h, "POST", "/api/speaker/notify", "application/json", `{"volume":40}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSpeakerNotifyLoopbackOnly(t *testing.T) {
	h, _ := newServer(t)
	// do() uses a non-loopback RemoteAddr (httptest's 192.0.2.1 default), so the
	// guard must refuse it — the endpoint is for on-speaker plugins, not the LAN.
	rec := do(t, h, "POST", "/api/speaker/notify", `{"url":"http://x/a.mp3"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSpeakerAudioMissing(t *testing.T) {
	h, _ := newServer(t)
	rec := doLoopbackCT(t, h, "GET", "/api/speaker/audio/deadbeef.mp3", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

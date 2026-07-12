package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doLoopback is like do but presents a loopback RemoteAddr, so it clears the
// loopbackOnly guard the speaker/display notify endpoints enforce.
func doLoopback(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:8000"
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSpeakerNotify(t *testing.T) {
	h, sp := newServer(t)

	rec := doLoopback(t, h, "POST", "/api/speaker/notify",
		`{"url":"http://example.com/ding.mp3","volume":40,"artist":"Ring","track":"Voordeur"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %q; want 200", rec.Code, rec.Body.String())
	}
	n := sp.LastNotification()
	if n == nil {
		t.Fatal("sim recorded no notification")
	}
	if n.URL != "http://example.com/ding.mp3" || n.Volume != 40 {
		t.Errorf("url/volume wrong: %+v", n)
	}
	if n.Artist != "Ring" || n.Track != "Voordeur" {
		t.Errorf("metadata wrong: %+v", n)
	}
	// The endpoint fills in the default app_key so plugins don't need to know it.
	if n.AppKey == "" {
		t.Error("app_key not defaulted")
	}
}

func TestSpeakerNotifyRejectsEmptyURL(t *testing.T) {
	h, _ := newServer(t)
	rec := doLoopback(t, h, "POST", "/api/speaker/notify", `{"volume":40}`)
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

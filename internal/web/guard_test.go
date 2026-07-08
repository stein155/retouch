package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostAllowed(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"192.168.1.42:8080", true},
		{"192.168.1.42", true},
		{"127.0.0.1:8000", true},
		{"[::1]:8080", true},
		{"localhost:8080", true},
		{"keuken.local", true},
		{"keuken.local:8080", true},
		{"keuken.local.:8080", true}, // trailing dot
		{"evil.com:8080", false},     // rebinding target
		{"attacker.example", false},
		{"", false},
	}
	for _, c := range cases {
		if got := hostAllowed(c.host); got != c.want {
			t.Errorf("hostAllowed(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestGuardBlocksRebindingAndCSRF(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := (&Server{}).guard(ok)

	req := func(method, host, origin, secFetch string) int {
		r := httptest.NewRequest(method, "http://x/api/settings", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if secFetch != "" {
			r.Header.Set("Sec-Fetch-Site", secFetch)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	// GET from the speaker's own address passes.
	if code := req("GET", "192.168.1.42:8080", "", ""); code != http.StatusOK {
		t.Errorf("GET same-host = %d, want 200", code)
	}
	// DNS rebinding: request arrives under the attacker's hostname.
	if code := req("GET", "evil.com", "", ""); code != http.StatusForbidden {
		t.Errorf("GET rebinding host = %d, want 403", code)
	}
	// CSRF: cross-origin mutating request is refused (Origin mismatch).
	if code := req("PUT", "192.168.1.42:8080", "http://evil.com", ""); code != http.StatusForbidden {
		t.Errorf("PUT cross-origin = %d, want 403", code)
	}
	// Same-origin mutating request passes.
	if code := req("PUT", "192.168.1.42:8080", "http://192.168.1.42:8080", ""); code != http.StatusOK {
		t.Errorf("PUT same-origin = %d, want 200", code)
	}
	// Simple POST with no Origin but a cross-site Sec-Fetch-Site is refused.
	if code := req("POST", "192.168.1.42:8080", "", "cross-site"); code != http.StatusForbidden {
		t.Errorf("POST cross-site = %d, want 403", code)
	}
}

package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doAuth is do with a session cookie attached (pass "" for none) and returns
// the recorder, whose Set-Cookie can be fed into the next call via cookieOf.
func doAuth(t *testing.T, h http.Handler, method, target, body, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	req.Host = "127.0.0.1:8000"
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// cookieOf extracts the session cookie set by a login/set-password response.
func cookieOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "retouch_session" && c.Value != "" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("no session cookie in response (Set-Cookie: %v)", rec.Header().Values("Set-Cookie"))
	return ""
}

// Without a password everything behaves exactly like before: settings open.
func TestAuthOpenWithoutPassword(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, "GET", "/api/auth", "")
	var got map[string]bool
	decodeBody(t, rec, &got)
	if got["hasPassword"] || !got["authenticated"] {
		t.Fatalf("fresh /api/auth = %v, want hasPassword=false authenticated=true", got)
	}
	if rec := do(t, h, "PUT", "/api/settings", `{"language":"nl"}`); rec.Code != 200 {
		t.Fatalf("PUT settings without password: %d, want 200", rec.Code)
	}
}

// Setting a password locks the settings side of the API behind a login and
// leaves the player side open.
func TestAuthLockAndLogin(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, "POST", "/api/auth/password", `{"newPassword":"geheim"}`)
	if rec.Code != 200 {
		t.Fatalf("set password: %d (%s)", rec.Code, rec.Body)
	}
	setterCookie := cookieOf(t, rec)

	// The protected endpoints all demand a login now.
	for _, c := range []struct{ method, path, body string }{
		{"PUT", "/api/settings", `{"language":"nl"}`},
		{"POST", "/api/update", `{}`},
		{"GET", "/api/debug", ""},
		{"GET", "/api/mqtt/status", ""},
		{"POST", "/api/auth/password", `{"newPassword":"anders"}`},
		// Plugins are part of the gated settings sheet: install/remove/sideload
		// (arbitrary-code) and the config proxy must demand a login. requireAuth
		// runs before any catalog lookup, so the plugin name is a placeholder.
		{"POST", "/api/plugins/example/install", `{}`},
		{"POST", "/api/plugins/example/upload", ``},
		{"DELETE", "/api/plugins/example", ``},
		{"GET", "/api/plugins/example/manifest", ``},
		{"POST", "/api/plugins/example/action/x", `{}`},
	} {
		if rec := do(t, h, c.method, c.path, c.body); rec.Code != 401 {
			t.Errorf("%s %s without login: %d, want 401", c.method, c.path, rec.Code)
		}
	}

	// The player keeps working without a login.
	for _, path := range []string{"/api/presets", "/api/volume", "/api/version", "/api/now"} {
		if rec := do(t, h, "GET", path, ""); rec.Code != 200 {
			t.Errorf("GET %s without login: %d, want 200", path, rec.Code)
		}
	}

	// Unauthenticated GET /api/settings only reveals what the app needs to boot.
	rec = do(t, h, "GET", "/api/settings", "")
	var limited map[string]any
	decodeBody(t, rec, &limited)
	if limited["hasPassword"] != true || limited["authenticated"] != false {
		t.Errorf("limited settings flags = %v", limited)
	}
	if limited["language"] == nil || limited["name"] == nil {
		t.Errorf("limited settings missing boot fields: %v", limited)
	}
	for _, key := range []string{"network", "mqtt", "closeTelnet", "bass", "model"} {
		if _, ok := limited[key]; ok {
			t.Errorf("limited settings leaks %q: %v", key, limited)
		}
	}

	// Wrong password: refused (and damped — not asserted on time).
	if rec := do(t, h, "POST", "/api/auth/login", `{"password":"fout"}`); rec.Code != 401 {
		t.Fatalf("wrong login: %d, want 401", rec.Code)
	}

	// Right password: a session cookie that unlocks the settings API.
	rec = do(t, h, "POST", "/api/auth/login", `{"password":"geheim"}`)
	if rec.Code != 200 {
		t.Fatalf("login: %d (%s)", rec.Code, rec.Body)
	}
	cookie := cookieOf(t, rec)
	if rec := doAuth(t, h, "PUT", "/api/settings", `{"language":"nl"}`, cookie); rec.Code != 200 {
		t.Fatalf("PUT settings with login: %d (%s)", rec.Code, rec.Body)
	}
	rec = doAuth(t, h, "GET", "/api/settings", "", cookie)
	var full map[string]any
	decodeBody(t, rec, &full)
	if _, ok := full["closeTelnet"]; !ok {
		t.Errorf("full settings missing closeTelnet: %v", full)
	}

	// The password setter got a session too.
	if rec := doAuth(t, h, "GET", "/api/auth", "", setterCookie); rec.Code != 200 {
		t.Fatalf("GET auth: %d", rec.Code)
	}

	// Logout kills the session.
	if rec := doAuth(t, h, "POST", "/api/auth/logout", "", cookie); rec.Code != 200 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := doAuth(t, h, "PUT", "/api/settings", `{"language":"nl"}`, cookie); rec.Code != 401 {
		t.Fatalf("PUT settings after logout: %d, want 401", rec.Code)
	}
}

func TestChangePasswordRequiresCurrent(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, "POST", "/api/auth/password", `{"newPassword":"eerste"}`)
	if rec.Code != 200 {
		t.Fatalf("set password: %d", rec.Code)
	}
	cookie := cookieOf(t, rec)

	if rec := doAuth(t, h, "POST", "/api/auth/password", `{"currentPassword":"fout","newPassword":"tweede"}`, cookie); rec.Code != 401 {
		t.Fatalf("change with wrong current: %d, want 401", rec.Code)
	}
	if rec := doAuth(t, h, "POST", "/api/auth/password", `{"currentPassword":"eerste","newPassword":"abc"}`, cookie); rec.Code != 400 {
		t.Fatalf("change to too-short password: %d, want 400", rec.Code)
	}
	rec = doAuth(t, h, "POST", "/api/auth/password", `{"currentPassword":"eerste","newPassword":"tweede"}`, cookie)
	if rec.Code != 200 {
		t.Fatalf("change password: %d (%s)", rec.Code, rec.Body)
	}
	newCookie := cookieOf(t, rec)

	// A change invalidates every other session; the changer keeps a fresh one.
	if rec := doAuth(t, h, "PUT", "/api/settings", `{"language":"nl"}`, cookie); rec.Code != 401 {
		t.Fatalf("old session after change: %d, want 401", rec.Code)
	}
	if rec := doAuth(t, h, "PUT", "/api/settings", `{"language":"nl"}`, newCookie); rec.Code != 200 {
		t.Fatalf("new session after change: %d, want 200", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/auth/login", `{"password":"tweede"}`); rec.Code != 200 {
		t.Fatalf("login with new password: %d", rec.Code)
	}
}

// Sessions live in a file, so an OTA update or reboot (both restart the
// process) doesn't log the user out.
func TestSessionSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	srv, _, _ := newServerAt(t, dir)
	h := srv.Handler()

	rec := do(t, h, "POST", "/api/auth/password", `{"newPassword":"geheim"}`)
	if rec.Code != 200 {
		t.Fatalf("set password: %d", rec.Code)
	}
	cookie := cookieOf(t, rec)

	srv2, _, _ := newServerAt(t, dir)
	h2 := srv2.Handler()
	if rec := doAuth(t, h2, "PUT", "/api/settings", `{"language":"nl"}`, cookie); rec.Code != 200 {
		t.Fatalf("session after restart: %d, want 200", rec.Code)
	}
	if rec := do(t, h2, "PUT", "/api/settings", `{"language":"nl"}`); rec.Code != 401 {
		t.Fatalf("no session after restart: %d, want 401", rec.Code)
	}
}

// Factory-reset recovery wipes the password and reopens telnet — and is a
// silent no-op when there is nothing to recover (fresh install pairing for the
// first time).
func TestRecoverAfterFactoryReset(t *testing.T) {
	srv, _, _ := newServerAt(t, t.TempDir())
	fwCalls := []bool{}
	srv.SetTelnetFirewall(func(closed bool) error { fwCalls = append(fwCalls, closed); return nil })
	h := srv.Handler()

	// Nothing set: recovery must change nothing (and not touch the firewall).
	if err := srv.RecoverAfterFactoryReset(); err != nil {
		t.Fatalf("no-op recovery: %v", err)
	}
	if len(fwCalls) != 0 {
		t.Fatalf("no-op recovery touched firewall: %v", fwCalls)
	}

	// Lock settings and close telnet.
	rec := do(t, h, "POST", "/api/auth/password", `{"newPassword":"geheim"}`)
	cookie := cookieOf(t, rec)
	if rec := doAuth(t, h, "PUT", "/api/settings", `{"closeTelnet":true}`, cookie); rec.Code != 200 {
		t.Fatalf("close telnet: %d", rec.Code)
	}

	if err := srv.RecoverAfterFactoryReset(); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	// Password gone: settings open again, old session irrelevant.
	if rec := do(t, h, "PUT", "/api/settings", `{"language":"nl"}`); rec.Code != 200 {
		t.Fatalf("PUT settings after recovery: %d, want 200 (password cleared)", rec.Code)
	}
	// Telnet reopened: last firewall call is an open, and the marker is gone.
	if len(fwCalls) == 0 || fwCalls[len(fwCalls)-1] {
		t.Fatalf("firewall calls after recovery = %v, want trailing open (false)", fwCalls)
	}
	rec = do(t, h, "GET", "/api/settings", "")
	var got map[string]any
	decodeBody(t, rec, &got)
	if got["closeTelnet"] != false {
		t.Fatalf("closeTelnet after recovery = %v, want false", got["closeTelnet"])
	}
}

// The CSRF guard still covers the auth endpoints: a cross-origin login attempt
// is refused before any password check.
func TestLoginCSRFGuard(t *testing.T) {
	h, _ := newServer(t)
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"password":"x"}`))
	req.Host = "127.0.0.1:8000"
	req.Header.Set("Origin", "http://attacker.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login: %d, want 403", rec.Code)
	}
}

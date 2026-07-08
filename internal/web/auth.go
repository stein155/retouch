package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/atomicjson"
	"github.com/stein155/retouch/internal/auth"
	"github.com/stein155/retouch/internal/settings"
)

// The admin password gates the settings side of the API (see Handler). No
// password set = everything open, exactly like builds before this existed.
// Sessions live in a small on-disk file so an OTA update or reboot (both restart
// the process) doesn't log everyone out.

const (
	sessionCookie = "retouch_session"
	sessionTTL    = 30 * 24 * time.Hour
	// minPasswordLen is deliberately lenient: the speaker is a LAN-only device
	// and the password mostly guards against housemates, not the internet.
	minPasswordLen = 4
)

// sessionStore holds active login sessions, keyed by the SHA-256 of the session
// token (so the file on disk never contains replayable tokens).
type sessionStore struct {
	path string
	mu   sync.Mutex
	m    map[string]time.Time // token key -> expiry
}

func openSessions(path string) *sessionStore {
	st := &sessionStore{path: path, m: map[string]time.Time{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st.m)
	}
	now := time.Now()
	for k, exp := range st.m {
		if now.After(exp) {
			delete(st.m, k)
		}
	}
	return st
}

// persistLocked writes the session map. Caller holds st.mu. Errors are dropped:
// a failed write only means sessions won't survive the next restart.
func (st *sessionStore) persistLocked() {
	_ = atomicjson.Write(st.path, st.m, 0o600)
}

func (st *sessionStore) create(token string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.m[auth.TokenKey(token)] = time.Now().Add(sessionTTL)
	st.persistLocked()
}

// valid reports whether token names a live session, sliding its expiry forward.
// The slide is only persisted when it moves the expiry by more than a day, so
// routine requests don't rewrite the file.
func (st *sessionStore) valid(token string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	key := auth.TokenKey(token)
	exp, ok := st.m[key]
	now := time.Now()
	if !ok || now.After(exp) {
		delete(st.m, key)
		return false
	}
	if next := now.Add(sessionTTL); next.Sub(exp) > 24*time.Hour {
		st.m[key] = next
		st.persistLocked()
	}
	return true
}

func (st *sessionStore) remove(token string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.m, auth.TokenKey(token))
	st.persistLocked()
}

// clear drops every session (password changed or reset).
func (st *sessionStore) clear() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.m = map[string]time.Time{}
	st.persistLocked()
}

// authorized reports whether r may touch the protected (settings) API: always
// when no admin password is set, otherwise only with a live session cookie.
func (s *Server) authorized(r *http.Request) bool {
	if s.settings.Get().Auth.PasswordHash == "" {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	return err == nil && s.sessions.valid(c.Value)
}

// requireAuth guards a handler behind the settings login. 401 (not the guard's
// 403) tells the frontend to show the login screen.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login required"})
			return
		}
		next(w, r)
	}
}

// verifyPassword checks secret against the stored admin hash. Attempts are
// serialized and failures cost an extra second, which both damps brute-forcing
// and keeps parallel PBKDF2 runs from pegging the speaker's single core.
func (s *Server) verifyPassword(secret string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	a := s.settings.Get().Auth
	if a.PasswordHash == "" {
		return false
	}
	if auth.Verify(secret, a.PasswordHash, a.PasswordSalt, a.Iterations) {
		return true
	}
	time.Sleep(time.Second)
	return false
}

// startSession mints a session for this browser and sets its cookie. No Secure
// flag: the app is plain HTTP on the LAN by design.
func (s *Server) startSession(w http.ResponseWriter) {
	token := auth.NewSessionToken()
	s.sessions.create(token)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// getAuth tells the UI whether a password is set and whether this browser is in.
func (s *Server) getAuth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{
		"hasPassword":   s.settings.Get().Auth.PasswordHash != "",
		"authenticated": s.authorized(r),
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "bad body", err)
		return
	}
	if !s.verifyPassword(body.Password) {
		s.log.Warn("settings login failed")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong password"})
		return
	}
	s.startSession(w)
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.remove(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// setPassword sets or changes the admin password. Handler() wraps it in
// requireAuth, so with a password already set only a logged-in browser gets
// here; the currentPassword check below still guards against a walked-away
// unlocked session being used to silently take over.
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "bad body", err)
		return
	}
	if len(strings.TrimSpace(body.NewPassword)) < minPasswordLen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password too short"})
		return
	}
	if s.settings.Get().Auth.PasswordHash != "" && !s.verifyPassword(body.CurrentPassword) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong password"})
		return
	}
	hash, salt := auth.Hash(body.NewPassword)
	if err := s.settings.SetAuth(settings.Auth{PasswordHash: hash, PasswordSalt: salt, Iterations: auth.Iterations}); err != nil {
		s.fail(w, "save password failed", err)
		return
	}
	// Other browsers must log in with the new password; this one stays in.
	s.sessions.clear()
	s.startSession(w)
	s.log.Info("settings password set")
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// RecoverAfterFactoryReset is the forgotten-password escape hatch, wired to
// autopair's factory-reset detection: physical access to the speaker (holding
// its reset button) is the proof of ownership that replaces the password. It
// wipes the admin password and every session and reopens LAN telnet. A no-op
// when there is nothing to recover, so a fresh install pairing for the first
// time changes nothing.
func (s *Server) RecoverAfterFactoryReset() error {
	hadPassword := s.settings.Get().Auth.PasswordHash != ""
	hadTelnetClosed := fileExists(filepath.Join(s.homeDir, ".close-telnet"))
	if !hadPassword && !hadTelnetClosed {
		return nil
	}
	s.log.Warn("factory reset detected: clearing settings password, reopening telnet")
	if err := s.settings.SetAuth(settings.Auth{}); err != nil {
		return err
	}
	s.sessions.clear()
	if hadTelnetClosed {
		return s.setCloseTelnet(false)
	}
	return nil
}

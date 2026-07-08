// Package settings persists small STLocal-level preferences that aren't speaker
// state — the UI language and whether the HomeKit bridge is on. (The speaker's own
// name and bass live on the speaker; its sysLanguage is a separate voice-prompt
// setting we don't touch.)
package settings

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/stein155/retouch/internal/atomicjson"
)

// Settings is the persisted preference set.
type Settings struct {
	Language string `json:"language"` // UI language code, e.g. "en", "nl"
	HomeKit  bool   `json:"homekit"`  // whether the Apple Home (HomeKit) bridge is on
	MQTT     MQTT   `json:"mqtt"`     // Home Assistant MQTT bridge config
	Auth     Auth   `json:"auth"`     // admin password for the settings UI
}

// Auth holds the hashed admin password that gates the settings UI. A zero Auth
// (empty PasswordHash) means no password is set and settings stay open, which is
// also what old settings files without the field decode to.
type Auth struct {
	PasswordHash string `json:"passwordHash"` // hex PBKDF2 key; "" = no password
	PasswordSalt string `json:"passwordSalt"` // hex random salt
	Iterations   int    `json:"iterations"`   // PBKDF2 work factor used for this hash
}

// MQTT is the persisted configuration for the Home Assistant MQTT bridge (see
// internal/habridge). Password is stored in the clear in the local JSON, like the
// other on-box state — the file never leaves the speaker.
type MQTT struct {
	Enabled         bool   `json:"enabled"`
	Host            string `json:"host"`
	Port            int    `json:"port"`            // 0 -> 1883 (8883 with TLS)
	Username        string `json:"username"`        // optional
	Password        string `json:"password"`        // optional
	BaseTopic       string `json:"baseTopic"`       // default "retouch/<deviceID>"
	DiscoveryPrefix string `json:"discoveryPrefix"` // default "homeassistant"
	TLS             bool   `json:"tls"`
}

// Store is a JSON-file-backed settings store, safe for concurrent handlers.
type Store struct {
	path string
	mu   sync.Mutex
	s    Settings
}

// Open loads (or starts) a store at path.
func Open(path string) *Store {
	st := &Store{path: path, s: Settings{Language: "en"}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st.s)
	}
	return st
}

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.s
}

// SetLanguage updates the UI language and persists.
func (s *Store) SetLanguage(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if code != "" {
		s.s.Language = code
	}
	return s.persistLocked()
}

// SetHomeKit records whether the HomeKit bridge is on and persists.
func (s *Store) SetHomeKit(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.HomeKit = on
	return s.persistLocked()
}

// SetMQTT replaces the MQTT bridge config and persists it.
func (s *Store) SetMQTT(cfg MQTT) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.MQTT = cfg
	return s.persistLocked()
}

// SetAuth replaces the admin password hash and persists it. A zero Auth clears
// the password (settings open again).
func (s *Store) SetAuth(a Auth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.Auth = a
	return s.persistLocked()
}

// persistLocked atomically writes the current settings. Caller holds s.mu.
// 0600: the file holds the MQTT broker password in the clear.
func (s *Store) persistLocked() error {
	return atomicjson.Write(s.path, s.s, 0o600)
}

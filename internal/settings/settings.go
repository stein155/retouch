// Package settings persists small STLocal-level preferences that aren't speaker
// state — currently just the UI language. (The speaker's own name and bass live on
// the speaker; its sysLanguage is a separate voice-prompt setting we don't touch.)
package settings

import (
	"encoding/json"
	"os"
	"sync"
)

// Settings is the persisted preference set.
type Settings struct {
	Language string `json:"language"` // UI language code, e.g. "en", "nl"
	MQTT     MQTT   `json:"mqtt"`     // Home Assistant MQTT bridge config
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

// SetMQTT replaces the MQTT bridge config and persists it.
func (s *Store) SetMQTT(cfg MQTT) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.MQTT = cfg
	return s.persistLocked()
}

// persistLocked atomically writes the current settings. Caller holds s.mu.
func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	// 0600: this file holds the MQTT broker password in the clear.
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

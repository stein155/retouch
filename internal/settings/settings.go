// Package settings persists small STLocal-level preferences that aren't speaker
// state — the UI language and whether the HomeKit bridge is on. (The speaker's own
// name and bass live on the speaker; its sysLanguage is a separate voice-prompt
// setting we don't touch.)
package settings

import (
	"encoding/json"
	"os"
	"sync"
)

// Settings is the persisted preference set.
type Settings struct {
	Language string `json:"language"` // UI language code, e.g. "en", "nl"
	HomeKit  bool   `json:"homekit"`  // whether the Apple Home (HomeKit) bridge is on
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
	return s.save()
}

// SetHomeKit records whether the HomeKit bridge is on and persists.
func (s *Store) SetHomeKit(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.HomeKit = on
	return s.save()
}

// save writes the current settings atomically. Caller must hold s.mu.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Package store is a tiny JSON-file preset store (6 slots), safe for concurrent
// HTTP handlers.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/stein155/retouch/internal/atomicjson"
)

// Preset is one saved station on a slot (1..6).
type Preset struct {
	Slot      int    `json:"slot"`
	StationID string `json:"stationId"` // TuneIn guide id
	Name      string `json:"name"`
	Logo      string `json:"logo,omitempty"`
}

// Store persists presets to a JSON file.
type Store struct {
	path string
	mu   sync.Mutex
	m    map[int]Preset
}

// Open loads (or starts) a store at path.
func Open(path string) (*Store, error) {
	s := &Store{path: path, m: map[int]Preset{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var list []Preset
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	for _, p := range list {
		if p.Slot >= 1 && p.Slot <= 6 {
			s.m[p.Slot] = p
		}
	}
	return s, nil
}

// All returns the presets sorted by slot.
func (s *Store) All() []Preset {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Preset, 0, len(s.m))
	for _, p := range s.m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// Get returns the preset on a slot.
func (s *Store) Get(slot int) (Preset, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[slot]
	return p, ok
}

// Set saves a preset and persists.
func (s *Store) Set(p Preset) error {
	if p.Slot < 1 || p.Slot > 6 {
		return fmt.Errorf("slot must be 1..6")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[p.Slot] = p
	return s.persistLocked()
}

// Remove clears a slot and persists.
func (s *Store) Remove(slot int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, slot)
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	out := make([]Preset, 0, len(s.m))
	for _, p := range s.m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return atomicjson.Write(s.path, out, 0o644)
}

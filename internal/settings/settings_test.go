package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultWhenFileAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Open(path)
	if got := s.Get().Language; got != "en" {
		t.Fatalf("default Language = %q, want \"en\"", got)
	}
}

func TestSetLanguageAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Open(path)
	if err := s.SetLanguage("nl"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if got := s.Get().Language; got != "nl" {
		t.Fatalf("Language = %q, want \"nl\"", got)
	}

	// Reopen from the same path; the language must have persisted.
	s2 := Open(path)
	if got := s2.Get().Language; got != "nl" {
		t.Fatalf("reopened Language = %q, want \"nl\"", got)
	}
}

func TestSetLanguageEmptyIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Open(path)
	if err := s.SetLanguage("fr"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	// Empty code must not overwrite the existing language.
	if err := s.SetLanguage(""); err != nil {
		t.Fatalf("SetLanguage(\"\"): %v", err)
	}
	if got := s.Get().Language; got != "fr" {
		t.Fatalf("Language = %q after empty set, want \"fr\"", got)
	}
}

func TestOpenCorruptFileFallsBackToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	// A corrupt file falls back to defaults but must not be silently discarded:
	// it is preserved as .corrupt so a wiped admin password/MQTT creds can be
	// recovered instead of vanishing.
	s := Open(path)
	if got := s.Get().Language; got != "en" {
		t.Fatalf("corrupt-file Language = %q, want default \"en\"", got)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("corrupt file not preserved as .corrupt: %v", err)
	}
}

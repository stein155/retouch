package atomicjson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := map[string]int{"a": 1, "b": 2}
	if err := Write(path, want, 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("round-trip = %v, want a:1 b:2", got)
	}

	// No .tmp left behind on success.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp not cleaned up: %v", err)
	}
}

package urlguard

import (
	"path/filepath"
	"testing"

	"github.com/stein155/retouch/internal/speaker"
)

func TestIsBootstrapLeftover(t *testing.T) {
	cases := []struct {
		name     string
		margeURL string
		want     bool
	}{
		{
			name:     "exact bootstrap leftover",
			margeURL: speaker.BootstrapURL,
			want:     true,
		},
		{
			name:     "clean stub url",
			margeURL: "http://127.0.0.1:9080",
			want:     false,
		},
		{
			name:     "deliberate ssh recovery command preserved",
			margeURL: "http://x.invalid;dropbear -p 22 &",
			want:     false,
		},
		{
			name:     "placeholder host but not the bootstrap command",
			margeURL: "http://x.invalid/update",
			want:     false,
		},
		{
			name:     "empty",
			margeURL: "",
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBootstrapLeftover(tc.margeURL); got != tc.want {
				t.Fatalf("isBootstrapLeftover(%q) = %v, want %v", tc.margeURL, got, tc.want)
			}
		})
	}
}

func TestRebootAttempts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json.urlguard")

	if got := readRebootAttempts(path); got != 0 {
		t.Fatalf("missing file: got %d, want 0", got)
	}
	writeRebootAttempts(path, 2)
	if got := readRebootAttempts(path); got != 2 {
		t.Fatalf("after write: got %d, want 2", got)
	}
	clearRebootAttempts(path)
	if got := readRebootAttempts(path); got != 0 {
		t.Fatalf("after clear: got %d, want 0", got)
	}
}

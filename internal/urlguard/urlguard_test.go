package urlguard

import (
	"testing"

	"github.com/stein155/retouch/internal/speaker"
)

func TestIsBootstrapLeftover(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
		want bool
	}{
		{
			name: "exact bootstrap leftover",
			cfg:  "margeServerUrl=" + speaker.BootstrapURL + "\n",
			want: true,
		},
		{
			name: "clean stub url",
			cfg:  "margeServerUrl=http://127.0.0.1:9080\n",
			want: false,
		},
		{
			name: "deliberate ssh recovery command preserved",
			cfg:  "margeServerUrl=http://x.invalid;dropbear -p 22 &\n",
			want: false,
		},
		{
			name: "placeholder host but not the bootstrap command",
			cfg:  "margeServerUrl=http://x.invalid/update\n",
			want: false,
		},
		{
			name: "empty",
			cfg:  "",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBootstrapLeftover(tc.cfg); got != tc.want {
				t.Fatalf("isBootstrapLeftover(%q) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}

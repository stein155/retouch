package speaker

import "testing"

func TestStationIDFromLocation(t *testing.T) {
	cases := map[string]string{
		"/v1/playback/station/s47309": "s47309",
		"s12345":                      "s12345",
		"":                            "",
		"  /a/b/c  ":                  "c",
		"/trailing/":                  "",
	}
	for in, want := range cases {
		if got := stationIDFromLocation(in); got != want {
			t.Errorf("stationIDFromLocation(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestXMLEsc(t *testing.T) {
	if got := xmlEsc(`a & b < c > d "e"`); got != `a &amp; b &lt; c &gt; d &quot;e&quot;` {
		t.Errorf("xmlEsc wrong: %q", got)
	}
}

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

func TestContentItemXML(t *testing.T) {
	// Preset shape: blank account, optional art present. Must match the wire format
	// /storePreset previously built inline.
	got := contentItemXML("TUNEIN", "stationurl", "/v1/playback/station/s99", "", "Radio 538", "http://logo")
	want := `<ContentItem source="TUNEIN" type="stationurl" location="/v1/playback/station/s99" sourceAccount="" isPresetable="true"><itemName>Radio 538</itemName><containerArt>http://logo</containerArt></ContentItem>`
	if got != want {
		t.Errorf("preset ContentItem:\n got %q\nwant %q", got, want)
	}

	// Select shape: account set, no art.
	got = contentItemXML("TUNEIN", "stationurl", "/loc", "acct", "X", "")
	want = `<ContentItem source="TUNEIN" type="stationurl" location="/loc" sourceAccount="acct" isPresetable="true"><itemName>X</itemName></ContentItem>`
	if got != want {
		t.Errorf("select ContentItem:\n got %q\nwant %q", got, want)
	}
}

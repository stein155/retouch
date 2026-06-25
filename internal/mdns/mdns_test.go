package mdns

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Keuken":              "keuken",
		"Living Room":         "living-room",
		"  Bose  SoundTouch ": "bose-soundtouch",
		"Café_2":              "caf-2",
		"---":                 "retouch",
		"":                    "retouch",
		"Bad/Chars!#":         "badchars",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeReadNameRoundTrip(t *testing.T) {
	enc := encodeName("keuken.local")
	name, next, ok := readName(enc, 0)
	if !ok || name != "keuken.local" || next != len(enc) {
		t.Fatalf("round-trip failed: name=%q next=%d ok=%v (len=%d)", name, next, ok, len(enc))
	}
}

func TestReadNameCompression(t *testing.T) {
	// "local" at offset 12, then "keuken" + pointer to offset 12.
	msg := make([]byte, 12)
	localAt := len(msg)
	msg = append(msg, encodeName("local")...)
	nameAt := len(msg)
	msg = append(msg, 6)
	msg = append(msg, []byte("keuken")...)
	msg = append(msg, 0xC0, byte(localAt)) // pointer to "local"

	name, next, ok := readName(msg, nameAt)
	if !ok || name != "keuken.local" {
		t.Fatalf("compressed name = %q ok=%v, want keuken.local", name, ok)
	}
	if next != len(msg) {
		t.Errorf("next = %d, want %d (just past the pointer)", next, len(msg))
	}
}

func TestRecordIsParseableA(t *testing.T) {
	r := New("192.168.1.42", "Keuken", nil)
	pkt := r.record(120)

	if got := binary.BigEndian.Uint16(pkt[6:8]); got != 1 {
		t.Fatalf("ANCOUNT = %d, want 1", got)
	}
	ip, ok := answerAFor(pkt, "keuken.local")
	if !ok {
		t.Fatal("answerAFor did not find the A record")
	}
	if !ip.Equal(net.ParseIP("192.168.1.42")) {
		t.Errorf("A record ip = %v, want 192.168.1.42", ip)
	}
}

func TestQuestionParses(t *testing.T) {
	q := question("kantoor.local")
	if got := binary.BigEndian.Uint16(q[4:6]); got != 1 {
		t.Fatalf("QDCOUNT = %d, want 1", got)
	}
	name, next, ok := readName(q, 12)
	if !ok || name != "kantoor.local" {
		t.Fatalf("question name = %q ok=%v", name, ok)
	}
	if qtype := binary.BigEndian.Uint16(q[next : next+2]); qtype != typeANY {
		t.Errorf("qtype = %d, want ANY(%d)", qtype, typeANY)
	}
}

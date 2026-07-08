package auth

import (
	"encoding/hex"
	"testing"
)

// PBKDF2-HMAC-SHA256 test vectors from RFC 7914 §11.
func TestPBKDF2Vectors(t *testing.T) {
	cases := []struct {
		password, salt string
		iter, keyLen   int
		want           string
	}{
		{"passwd", "salt", 1, 64,
			"55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc" +
				"49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783"},
		{"Password", "NaCl", 80000, 64,
			"4ddcd8f60b98be21830cee5ef22701f9641a4418d04c0414aeff08876b34ab56" +
				"a1d425a1225833549adb841b51c9b3176a272bdebba1d078478f62b397f33c8d"},
	}
	for _, c := range cases {
		got := pbkdf2Key([]byte(c.password), []byte(c.salt), c.iter, c.keyLen)
		if hex.EncodeToString(got) != c.want {
			t.Errorf("pbkdf2(%q,%q,%d): got %x, want %s", c.password, c.salt, c.iter, got, c.want)
		}
	}
}

func TestHashVerifyRoundTrip(t *testing.T) {
	hash, salt := Hash("geheim")
	if !Verify("geheim", hash, salt, Iterations) {
		t.Fatal("correct password rejected")
	}
	if Verify("fout", hash, salt, Iterations) {
		t.Fatal("wrong password accepted")
	}
	if Verify("geheim", hash, salt+"00", Iterations) {
		t.Fatal("wrong salt accepted")
	}
	if Verify("geheim", "", "", Iterations) {
		t.Fatal("empty hash accepted")
	}
	// Two hashes of the same password must differ (fresh salt each time).
	hash2, salt2 := Hash("geheim")
	if hash == hash2 || salt == salt2 {
		t.Fatal("salt not fresh per Hash call")
	}
}

func TestSessionToken(t *testing.T) {
	a, b := NewSessionToken(), NewSessionToken()
	if a == b || len(a) < 40 {
		t.Fatalf("tokens not unique/long enough: %q %q", a, b)
	}
	if TokenKey(a) == TokenKey(b) || len(TokenKey(a)) != 64 {
		t.Fatalf("token keys malformed")
	}
}

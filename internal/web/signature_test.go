package web

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyReleaseSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	dir := t.TempDir()
	sums := filepath.Join(dir, "SHA256SUMS")
	sig := filepath.Join(dir, "SHA256SUMS.sig")
	content := []byte("abc123  retouch-armv7l\n")
	if err := os.WriteFile(sums, content, 0o644); err != nil {
		t.Fatal(err)
	}
	writeSig := func(over []byte, key ed25519.PrivateKey) {
		s := ed25519.Sign(key, over)
		if err := os.WriteFile(sig, []byte(base64.StdEncoding.EncodeToString(s)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Valid signature over the exact checksums bytes.
	writeSig(content, priv)
	if err := verifyReleaseSignature(pubB64, sums, sig); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}

	// Signature over different bytes must fail (tampered checksums).
	writeSig([]byte("tampered  retouch-armv7l\n"), priv)
	if err := verifyReleaseSignature(pubB64, sums, sig); err == nil {
		t.Error("tampered checksums accepted")
	}

	// Signature by a different key must fail (forged release).
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	writeSig(content, otherPriv)
	if err := verifyReleaseSignature(pubB64, sums, sig); err == nil {
		t.Error("signature from wrong key accepted")
	}

	// Malformed public key must fail closed.
	writeSig(content, priv)
	if err := verifyReleaseSignature("not-base64!!", sums, sig); err == nil {
		t.Error("bad public key accepted")
	}
}

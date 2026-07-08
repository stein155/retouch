// Package auth hashes the admin password and mints session tokens for the web
// app's settings login. Stdlib only: PBKDF2-HMAC-SHA256 with a per-password salt.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
)

// Iterations is the PBKDF2 work factor for newly set passwords. The speaker is a
// single ~800 MHz ARMv7 core (each iteration is two SHA-256 compressions), so 50k
// keeps a verify well under a second there while still making offline guessing of
// the on-box settings file expensive. Stored alongside each hash so it can be
// raised later without invalidating existing passwords.
const Iterations = 50_000

const (
	keyLen  = 32
	saltLen = 16
)

// Hash derives a PBKDF2 hash for secret with a fresh random salt. Both return
// values are hex-encoded.
func Hash(secret string) (hashHex, saltHex string) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		// crypto/rand never fails on Linux; there is no sane fallback if it does.
		panic("auth: rand.Read: " + err.Error())
	}
	key := pbkdf2Key([]byte(secret), salt, Iterations, keyLen)
	return hex.EncodeToString(key), hex.EncodeToString(salt)
}

// Verify reports whether secret matches a hash produced by Hash (or by an older
// build with a different iteration count).
func Verify(secret, hashHex, saltHex string, iterations int) bool {
	want, err := hex.DecodeString(hashHex)
	if err != nil || len(want) == 0 {
		return false
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	if iterations <= 0 {
		iterations = Iterations
	}
	got := pbkdf2Key([]byte(secret), salt, iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewSessionToken returns a fresh unguessable session token (256 bits, base64url).
func NewSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("auth: rand.Read: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// TokenKey is the stable identifier a session token is stored under: hex
// SHA-256 of the token, so the on-disk session file never holds replayable
// tokens.
func TokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

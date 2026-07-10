package release

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestChecksumRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "artifact")
	data := []byte("hello plugin")
	if err := os.WriteFile(bin, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hexsum := hex.EncodeToString(sum[:])
	sums := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sums, []byte(hexsum+"  artifact\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	want, err := ChecksumFor(sums, "artifact")
	if err != nil {
		t.Fatalf("ChecksumFor: %v", err)
	}
	if want != hexsum {
		t.Fatalf("ChecksumFor = %s, want %s", want, hexsum)
	}
	if err := VerifyChecksum(bin, want); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	if err := VerifyChecksum(bin, "deadbeef"); err == nil {
		t.Fatal("VerifyChecksum accepted a wrong sum")
	}
	if _, err := ChecksumFor(sums, "missing"); err == nil {
		t.Fatal("ChecksumFor found a checksum that isn't listed")
	}
}

func TestVerifySignature(t *testing.T) {
	dir := t.TempDir()
	sums := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sums, []byte("abc  artifact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(sums)
	sig := filepath.Join(dir, "SHA256SUMS.sig")
	if err := os.WriteFile(sig, []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))), 0o644); err != nil {
		t.Fatal(err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if err := VerifySignature(pubB64, sums, sig); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
	// A different key must reject.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := VerifySignature(base64.StdEncoding.EncodeToString(otherPub), sums, sig); err == nil {
		t.Fatal("VerifySignature accepted a signature from the wrong key")
	}
}

func TestDownloadAndGetJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bin":
			_, _ = w.Write([]byte("BINARY"))
		case "/json":
			_, _ = w.Write([]byte(`{"tag_name":"v9"}`))
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	dst := filepath.Join(t.TempDir(), "out")
	if err := Download(context.Background(), ts.Client(), "test", ts.URL+"/bin", dst, 0o755); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "BINARY" {
		t.Fatalf("downloaded %q, want BINARY", b)
	}
	if err := Download(context.Background(), ts.Client(), "test", ts.URL+"/nope", dst, 0o755); err == nil {
		t.Fatal("Download of a 404 should fail")
	}

	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := GetJSON(context.Background(), ts.Client(), "test", ts.URL+"/json", &rel); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if rel.TagName != "v9" {
		t.Fatalf("GetJSON tag = %q, want v9", rel.TagName)
	}
}

func TestDownloadRemovesPartialFile(t *testing.T) {
	// An upstream that dies mid-body (Content-Length says 10, sends 5) must not
	// leave a truncated file behind.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("short"))
	}))
	defer ts.Close()

	dst := filepath.Join(t.TempDir(), "out")
	if err := Download(context.Background(), ts.Client(), "test", ts.URL, dst, 0o755); err == nil {
		t.Fatal("Download succeeded, want error")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("partial file exists after failed download: %v", err)
	}
}

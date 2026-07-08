// Package release fetches and verifies release artifacts (binaries + a SHA256SUMS
// manifest) from an HTTP source such as a GitHub release. It is the shared core
// behind both ReTouch's own self-update (internal/web) and plugin installs
// (internal/plugins): download over an SSRF-safe transport, match a SHA-256 from a
// SHA256SUMS file, and optionally require a valid ed25519 signature over that
// checksums file before any checksum in it is trusted.
package release

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// GetJSON fetches url with client and decodes the (bounded) JSON body into out.
// ua sets the User-Agent so GitHub doesn't reject the request.
func GetJSON(ctx context.Context, client *http.Client, ua, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return HTTPStatusError{URL: url, Status: resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// Download fetches url into dst with the given file mode, using client. A partial
// download is removed rather than left behind.
func Download(ctx context.Context, client *http.Client, ua, url, dst string, mode fs.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return HTTPStatusError{URL: url, Status: resp.StatusCode}
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
	}
	return closeErr
}

// ChecksumFor returns the lowercase hex SHA-256 recorded for name in a SHA256SUMS
// file (lines of "<hex>  <name>").
func ChecksumFor(sumsPath, name string) (string, error) {
	b, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", MissingChecksumError{Name: name}
}

// SHA256File returns the lowercase hex SHA-256 of the file at path.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyChecksum reports an error unless the file at path hashes to want.
func VerifyChecksum(path, want string) error {
	got, err := SHA256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return ChecksumMismatchError{Want: want, Got: got}
	}
	return nil
}

// VerifySignature checks that sigPath holds a valid ed25519 signature (base64) over
// the raw bytes of sumsPath, made by the private half of pubKeyB64.
func VerifySignature(pubKeyB64, sumsPath, sigPath string) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubKeyB64))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid release public key")
	}
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	sigRaw, err := os.ReadFile(sigPath)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigRaw)))
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}
	if !ed25519.Verify(pub, sums, sig) {
		return fmt.Errorf("release signature verification failed")
	}
	return nil
}

// SafeTransport returns a transport whose dialer refuses any non-public address,
// checked per hop so a 302-to-loopback or a DNS rebind can't slip an internal
// target past a pre-flight hostname check. All release artifacts live on public
// hosts (github.com), so nothing legitimate is affected.
func SafeTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if ip := net.ParseIP(host); ip == nil || !PublicIP(ip) {
					return fmt.Errorf("refusing to connect to non-public address %s", address)
				}
				return nil
			},
		}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
}

// PublicIP reports whether a is globally routable — not loopback, RFC1918/ULA,
// link-local, multicast or unspecified.
func PublicIP(a net.IP) bool {
	return !(a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsMulticast() || a.IsUnspecified())
}

// HTTPStatusError is returned when a fetch gets a non-200 response.
type HTTPStatusError struct {
	URL    string
	Status int
}

func (e HTTPStatusError) Error() string { return e.URL + " status " + strconv.Itoa(e.Status) }

// MissingChecksumError is returned when a SHA256SUMS file lists no entry for a name.
type MissingChecksumError struct{ Name string }

func (e MissingChecksumError) Error() string { return "missing checksum for " + e.Name }

// ChecksumMismatchError is returned when a downloaded file's hash differs from the
// one recorded in SHA256SUMS.
type ChecksumMismatchError struct{ Want, Got string }

func (e ChecksumMismatchError) Error() string { return "checksum mismatch " + e.Got + " != " + e.Want }

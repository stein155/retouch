package app

// Enables the firmware's /speaker audio-notification endpoint.
//
// Before playing a notification the SoundTouch firmware validates the request's
// app_key by calling Bose's audio-notification auth host over HTTPS. Bose shut that
// host down, so the call fails and /speaker is dead. We resurrect it the same way
// ReTouch resurrects radio: stand in for the dead host locally. On boot the launcher
// redirects the speaker's local :443 auth call to this process; here we serve a tiny
// TLS endpoint that answers /v1/auth with 200, presenting a leaf cert for the auth
// hostnames signed by a CA we generate once and append to the speaker's trust store.
// The firmware then trusts the stub, the app_key "validates", and /speaker works.

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// speakerAuthAddr is the loopback TLS port the boot launcher redirects the speaker's
// audio-notification auth call (local :443) to.
const speakerAuthAddr = "127.0.0.1:9443"

// speakerAuthCAMarker labels our CA in the system bundle so we append it exactly once
// and can recognise/remove it on uninstall.
const speakerAuthCAMarker = "ReTouch speaker notification auth CA"

// speakerAuthHosts are the audio-notification auth hostnames the firmware contacts.
// The leaf cert covers both; the boot launcher points them at loopback in /etc/hosts.
var speakerAuthHosts = []string{
	"audionotification.api.bosecm.com",
	"audionotificationdev.api.bosecm.com",
}

// speakerAuthHandler answers the one request the firmware makes: GET /v1/auth. The
// firmware only checks for HTTP 200, so an empty body is enough.
func speakerAuthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})
}

// ensureSpeakerAuthTLS generates the CA + leaf cert (once, under home) and appends the
// CA to the system trust bundle, returning the cert and key paths for the TLS listener.
func ensureSpeakerAuthTLS(home string, log *slog.Logger) (certFile, keyFile string, err error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", "", err
	}
	caFile := filepath.Join(home, "speaker-auth-ca.pem")
	certFile = filepath.Join(home, "speaker-auth-cert.pem")
	keyFile = filepath.Join(home, "speaker-auth-key.pem")
	if _, statErr := os.Stat(certFile); errors.Is(statErr, os.ErrNotExist) {
		if err := writeSpeakerAuthCerts(caFile, certFile, keyFile); err != nil {
			return "", "", err
		}
	}
	if err := trustSpeakerAuthCA(caFile, home); err != nil {
		return "", "", err
	}
	log.Info("speaker notification auth ready", "cert", certFile)
	return certFile, keyFile, nil
}

// writeSpeakerAuthCerts creates a self-signed CA and a leaf cert for speakerAuthHosts
// signed by it, writing all three PEM files. Long validity (20y) since the speaker's
// clock and our reinstall cadence make short-lived certs a needless failure mode.
func writeSpeakerAuthCerts(caFile, certFile, keyFile string) error {
	now := time.Now()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: speakerAuthCAMarker},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano() + 1),
		Subject:      pkix.Name{CommonName: speakerAuthHosts[0]},
		DNSNames:     speakerAuthHosts,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caTmpl, &leafKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writePEM(caFile, "CERTIFICATE", caDER, 0o644); err != nil {
		return err
	}
	if err := writePEM(certFile, "CERTIFICATE", leafDER, 0o644); err != nil {
		return err
	}
	return writePEM(keyFile, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(leafKey), 0o600)
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: der})
}

// trustSpeakerAuthCA appends our CA to the system trust bundle so the firmware trusts
// the stub. Idempotent (keyed on the marker), backs up the original once, and remounts
// the read-only rootfs rw around the append — the same dance the installer does.
func trustSpeakerAuthCA(caFile, home string) error {
	const bundle = "/etc/pki/tls/certs/ca-bundle.crt"
	ca, err := os.ReadFile(caFile)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(bundle)
	if err != nil {
		return err
	}
	if strings.Contains(string(body), speakerAuthCAMarker) {
		return nil // already trusted
	}
	backup := filepath.Join(home, "ca-bundle.crt.original")
	if _, statErr := os.Stat(backup); errors.Is(statErr, os.ErrNotExist) {
		if err := copyFile(bundle, backup, 0o644); err != nil {
			return err
		}
	}
	_ = remountRoot("rw")
	defer func() { _ = remountRoot("ro") }()
	f, err := os.OpenFile(bundle, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "\n# %s\n%s", speakerAuthCAMarker, ca)
	return err
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

// remountRoot remounts / read-write or read-only, tolerating either argument order
// (BusyBox vs util-linux mount differ on the speaker firmware).
func remountRoot(mode string) error {
	if err := exec.Command("mount", "/", "-o", mode+",remount").Run(); err == nil {
		return nil
	}
	return exec.Command("mount", "-o", "remount,"+mode, "/").Run()
}

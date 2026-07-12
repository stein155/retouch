package app

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

const speakerAuthCAMarker = "ReTouch speaker notification auth CA"

var speakerAuthHosts = []string{
	"audionotification.api.bosecm.com",
	"audionotificationdev.api.bosecm.com",
}

func speakerAuthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})
}

func ensureSpeakerAuthTLS(home string, log *slog.Logger) (string, string, error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", "", err
	}
	caFile := filepath.Join(home, "speaker-auth-ca.pem")
	certFile := filepath.Join(home, "speaker-auth-cert.pem")
	keyFile := filepath.Join(home, "speaker-auth-key.pem")
	if _, err := os.Stat(certFile); errors.Is(err, os.ErrNotExist) {
		if err := writeSpeakerAuthCerts(caFile, certFile, keyFile); err != nil {
			return "", "", err
		}
	}
	if err := trustSpeakerAuthCA(caFile, home); err != nil {
		return "", "", err
	}
	log.Info("speaker notification auth TLS ready", "cert", certFile)
	return certFile, keyFile, nil
}

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
		Subject:      pkix.Name{CommonName: speakerAuthHosts[1]},
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
		return nil
	}
	backup := filepath.Join(home, "ca-bundle.crt.original")
	if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
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

func remountRoot(mode string) error {
	if err := exec.Command("mount", "/", "-o", mode+",remount").Run(); err == nil {
		return nil
	}
	return exec.Command("mount", "-o", "remount,"+mode, "/").Run()
}

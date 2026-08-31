package tlsconf

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

func TestEphemeralCertificateHandshakes(t *testing.T) {
	tlsConfig, info, err := Load(config.TLS{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != SourceEphemeral {
		t.Fatalf("source = %q, want %q", info.Source, SourceEphemeral)
	}
	if len(info.Fingerprint) != 32*3-1 {
		t.Fatalf("fingerprint %q is not 32 colon-separated bytes", info.Fingerprint)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed under test
	}}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.TLS.Version < tls.VersionTLS12 {
		t.Fatalf("negotiated TLS %x, want >= 1.2", resp.TLS.Version)
	}
}

func TestEphemeralCertificateShape(t *testing.T) {
	tlsConfig, _, err := Load(config.TLS{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("localhost not covered: %v", err)
	}
	if err := leaf.VerifyHostname("127.0.0.1"); err != nil {
		t.Errorf("127.0.0.1 not covered: %v", err)
	}
}

// writePEMPair persists a generated certificate as the PEM files a
// customer would configure.
func writePEMPair(t *testing.T, dir string) (certPath, keyPath string, cert tls.Certificate) {
	t.Helper()
	cert, err := generateSelfSigned(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, cert
}

func TestFileCertificates(t *testing.T) {
	certPath, keyPath, written := writePEMPair(t, t.TempDir())
	tlsConfig, info, err := Load(config.TLS{Certificate: certPath, PrivateKey: keyPath}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != SourceFile {
		t.Fatalf("source = %q, want %q", info.Source, SourceFile)
	}
	if info.Fingerprint != fingerprint(written.Certificate[0]) {
		t.Fatal("served certificate differs from the configured file")
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want TLS 1.2", tlsConfig.MinVersion)
	}
}

func TestLoneCertificateOrKeyIsError(t *testing.T) {
	certPath, keyPath, _ := writePEMPair(t, t.TempDir())
	for _, cfg := range []config.TLS{{Certificate: certPath}, {PrivateKey: keyPath}} {
		if _, _, err := Load(cfg, time.Now()); err == nil {
			t.Fatalf("want an error for the half-configured pair %+v, got nil", cfg)
		}
	}
}

func TestMissingFileIsError(t *testing.T) {
	_, _, err := Load(config.TLS{Certificate: "/nonexistent/cert.pem", PrivateKey: "/nonexistent/key.pem"}, time.Now())
	if err == nil {
		t.Fatal("want error for missing certificate files, got nil")
	}
}

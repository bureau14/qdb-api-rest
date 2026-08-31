// Package tlsconf builds the tls.Config for the HTTPS listener: a PEM
// certificate/key pair from disk when configured, an ephemeral
// self-signed certificate otherwise, so TLS works with zero
// configuration (docs/adr/0001-tls-certificates.md).
package tlsconf

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

// Source records where the served certificate came from, for the startup
// log.
type Source string

const (
	SourceFile      Source = "file"
	SourceEphemeral Source = "ephemeral-self-signed"
)

// Info describes the certificate the listener will serve; the caller logs
// it (a warning for ephemeral certificates, so an unconfigured production
// deployment is visible).
type Info struct {
	Source      Source
	Fingerprint string // sha256 over the leaf certificate DER
	NotAfter    time.Time
}

// fingerprint renders the sha256 of the leaf DER the way clients pin it:
// colon-separated upper-case hex.
func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	pairs := make([]string, len(sum))
	for i, b := range sum {
		pairs[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	return strings.Join(pairs, ":")
}

// selfSignedNames returns the SANs of a generated certificate: loopback
// plus this machine's hostname.
func selfSignedNames() (dns []string, ips []net.IP) {
	dns = []string{"localhost"}
	if hostname, err := os.Hostname(); err == nil && hostname != "" && hostname != "localhost" {
		dns = append(dns, hostname)
	}
	ips = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	return dns, ips
}

// generateSelfSigned mints an ECDSA P-256 self-signed certificate. Ten
// years of validity: expiry must never stop a long-running process from
// handshaking.
func generateSelfSigned(now time.Time) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	dns, ips := selfSignedNames()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "qdb_rest ephemeral"},
		NotBefore:    now.Add(-time.Hour), // tolerate modest clock skew
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// loadCertificate resolves the certificate per config: the PEM pair when
// configured, a generated one otherwise. A lone certificate or key is
// rejected here, the one place that reads the pair, so a half-configured
// pair can never silently serve an ephemeral certificate.
func loadCertificate(cfg config.TLS, now time.Time) (tls.Certificate, Source, error) {
	if (cfg.Certificate == "") != (cfg.PrivateKey == "") {
		return tls.Certificate{}, SourceFile, errors.New("tls.certificate and tls.private_key must be set together")
	}
	if cfg.Certificate == "" {
		cert, err := generateSelfSigned(now)
		return cert, SourceEphemeral, err
	}
	cert, err := tls.LoadX509KeyPair(cfg.Certificate, cfg.PrivateKey)
	if err != nil {
		return tls.Certificate{}, SourceFile, fmt.Errorf("loading tls certificate: %w", err)
	}
	return cert, SourceFile, nil
}

// Load returns the TLS configuration for the HTTPS listener plus a
// description of the certificate for the startup log.
func Load(cfg config.TLS, now time.Time) (*tls.Config, Info, error) {
	cert, source, err := loadCertificate(cfg, now)
	if err != nil {
		return nil, Info{}, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, Info{}, fmt.Errorf("parsing leaf certificate: %w", err)
	}
	info := Info{Source: source, Fingerprint: fingerprint(cert.Certificate[0]), NotAfter: leaf.NotAfter}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, info, nil
}

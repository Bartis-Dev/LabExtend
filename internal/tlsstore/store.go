// Package tlsstore manages LabExtend's optional HTTPS certificate.
//
// The certificate can come from three places (checked in order):
//   1. LABEXTEND_TLS_CERT_FILE / LABEXTEND_TLS_KEY_FILE — explicit env paths
//   2. <datadir>/tls/cert.pem + key.pem — uploaded via the web UI
//   3. self-signed, generated on first boot when LABEXTEND_TLS_SELF_SIGN=true
//
// The current cert is held behind an atomic pointer so the HTTPS server's
// GetCertificate callback can hot-reload after an upload without restart.
package tlsstore

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	certName = "cert.pem"
	keyName  = "key.pem"
	subdir   = "tls"
)

type Source string

const (
	SourceEnv        Source = "env"
	SourceDataDir    Source = "data_dir"
	SourceSelfSigned Source = "self_signed"
)

// Info describes the certificate currently in use; safe to expose via API.
type Info struct {
	Loaded     bool      `json:"loaded"`
	Source     Source    `json:"source,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	Issuer     string    `json:"issuer,omitempty"`
	DNSNames   []string  `json:"dns_names,omitempty"`
	IPs        []string  `json:"ips,omitempty"`
	NotBefore  time.Time `json:"not_before,omitempty"`
	NotAfter   time.Time `json:"not_after,omitempty"`
	SelfSigned bool      `json:"self_signed,omitempty"`
}

type Store struct {
	dataDir  string
	envCert  string
	envKey   string
	cert     atomic.Pointer[tls.Certificate]
	source   atomic.Pointer[Source]
	parsed   atomic.Pointer[x509.Certificate]
	mu       sync.Mutex // serializes writes to disk
}

func New(dataDir, envCertFile, envKeyFile string) *Store {
	return &Store{dataDir: dataDir, envCert: envCertFile, envKey: envKeyFile}
}

// CertPath returns the on-disk path to the cert file. Same for KeyPath.
// Files may not yet exist.
func (s *Store) CertPath() string { return filepath.Join(s.dataDir, subdir, certName) }
func (s *Store) KeyPath() string  { return filepath.Join(s.dataDir, subdir, keyName) }

// LoadOrCreate picks the active cert source in priority order: env paths,
// then datadir/tls. If neither yields a cert, it generates a fresh
// self-signed cert covering localhost + 127.0.0.1 so the server can
// always boot into HTTPS. The user can replace it later via the UI.
func (s *Store) LoadOrCreate() error {
	if s.envCert != "" && s.envKey != "" {
		if err := s.loadFromFiles(s.envCert, s.envKey, SourceEnv); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("env tls files: %w", err)
		}
	}
	if err := s.loadFromFiles(s.CertPath(), s.KeyPath(), SourceDataDir); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("datadir tls files: %w", err)
	}
	return s.GenerateSelfSigned(nil, 365*24*time.Hour)
}

func (s *Store) loadFromFiles(certFile, keyFile string, src Source) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse leaf: %w", err)
	}
	cert.Leaf = leaf
	s.cert.Store(&cert)
	srcCopy := src
	s.source.Store(&srcCopy)
	s.parsed.Store(leaf)
	return nil
}

// Current returns the active cert (nil if none). Designed for
// tls.Config.GetCertificate so HTTPS handshakes always see the latest.
func (s *Store) Current() *tls.Certificate {
	return s.cert.Load()
}

// GetCertificate is the callback for *tls.Config.GetCertificate.
func (s *Store) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	c := s.cert.Load()
	if c == nil {
		return nil, errors.New("no TLS certificate configured — upload one via Settings → TLS")
	}
	return c, nil
}

// CurrentInfo returns metadata about the active cert. .Loaded is false
// when no cert is configured.
func (s *Store) CurrentInfo() Info {
	c := s.parsed.Load()
	if c == nil {
		return Info{Loaded: false}
	}
	srcPtr := s.source.Load()
	src := Source("")
	if srcPtr != nil {
		src = *srcPtr
	}
	selfSigned := c.Issuer.String() == c.Subject.String()
	ips := make([]string, 0, len(c.IPAddresses))
	for _, ip := range c.IPAddresses {
		ips = append(ips, ip.String())
	}
	return Info{
		Loaded:     true,
		Source:     src,
		Subject:    c.Subject.String(),
		Issuer:     c.Issuer.String(),
		DNSNames:   append([]string{}, c.DNSNames...),
		IPs:        ips,
		NotBefore:  c.NotBefore,
		NotAfter:   c.NotAfter,
		SelfSigned: selfSigned,
	}
}

// SavePEM writes new cert + key PEM blocks to disk (datadir/tls/) and
// hot-swaps the active certificate. The pair is validated before writing.
func (s *Store) SavePEM(certPEM, keyPEM []byte) error {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("cert/key pair invalid: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse leaf: %w", err)
	}
	cert.Leaf = leaf

	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.dataDir, subdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, certName), certPEM, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, keyName), keyPEM, 0o600); err != nil {
		return err
	}
	s.cert.Store(&cert)
	src := SourceDataDir
	s.source.Store(&src)
	s.parsed.Store(leaf)
	return nil
}

// Reset removes the datadir cert/key files (env-pointed paths are left
// alone) and immediately generates a fresh self-signed cert. Since the
// server is HTTPS-only, we never leave the store without an active cert.
func (s *Store) Reset() error {
	s.mu.Lock()
	if err := os.Remove(s.CertPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.mu.Unlock()
		return err
	}
	if err := os.Remove(s.KeyPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.mu.Unlock()
		return err
	}
	s.cert.Store(nil)
	s.source.Store(nil)
	s.parsed.Store(nil)
	s.mu.Unlock()
	return s.GenerateSelfSigned(nil, 365*24*time.Hour)
}

// GenerateSelfSigned creates a fresh ECDSA P-256 self-signed cert covering
// the given hostnames/IPs (defaults to localhost + 127.0.0.1 if empty),
// writes it to datadir/tls, and activates it.
func (s *Store) GenerateSelfSigned(hosts []string, validity time.Duration) error {
	if len(hosts) == 0 {
		hosts = []string{"localhost", "127.0.0.1"}
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "labextend", Organization: []string{"LabExtend"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := s.SavePEM(certPEM, keyPEM); err != nil {
		return err
	}
	src := SourceSelfSigned
	s.source.Store(&src)
	return nil
}

package leader

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// loadServerTLS reads the leader's cert + key and (optionally) a client CA
// bundle to require mTLS from agents.
//
// Behavior:
//   • cert + key only           → TLS server, agents do not need a client cert
//   • cert + key + clientCA     → full mTLS: agents MUST present a client cert
//                                 signed by clientCA
func loadServerTLS(certPath, keyPath, clientCAPath string) (credentials.TransportCredentials, error) {
	if certPath == "" || keyPath == "" {
		return nil, errors.New("cert + key paths required")
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load keypair: %w", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if clientCAPath != "" {
		pool := x509.NewCertPool()
		caBytes, err := os.ReadFile(clientCAPath)
		if err != nil {
			return nil, fmt.Errorf("read client CA: %w", err)
		}
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, errors.New("client CA: no certs parsed")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(cfg), nil
}

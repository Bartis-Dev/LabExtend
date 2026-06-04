package agent

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Bartis-Dev/LabExtend/internal/config"
)

// buildClientTLS picks a transport credential for the agent's outbound dial.
//
//   • no BPM_GRPC_TLS_* env vars set     → insecure (plaintext h2c; relies on
//                                          the overlay network being trusted)
//   • client CA only                     → server-auth TLS (leader cert checked
//                                          against CA, no client cert sent)
//   • client CA + cert + key             → full mTLS (agent presents cert)
func buildClientTLS(cfg *config.Config) (credentials.TransportCredentials, error) {
	if cfg.GRPCTLSClientCA == "" && cfg.GRPCTLSCert == "" {
		return insecure.NewCredentials(), nil
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.GRPCTLSClientCA != "" {
		caBytes, err := os.ReadFile(cfg.GRPCTLSClientCA)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, errors.New("CA: no certs parsed")
		}
		tlsCfg.RootCAs = pool
	}
	if cfg.GRPCTLSCert != "" && cfg.GRPCTLSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.GRPCTLSCert, cfg.GRPCTLSKey)
		if err != nil {
			return nil, fmt.Errorf("load keypair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return credentials.NewTLS(tlsCfg), nil
}

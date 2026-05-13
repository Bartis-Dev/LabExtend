package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/tlsstore"
)

type tlsUploadInput struct {
	CertPEM string `json:"cert_pem"`
	KeyPEM  string `json:"key_pem"`
}

type tlsSelfSignInput struct {
	Hostnames    []string `json:"hostnames"`
	ValidityDays int      `json:"validity_days"`
}

func (s *Server) handleTLSState(w http.ResponseWriter, _ *http.Request) {
	out := struct {
		tlsstore.Info
		HTTPSEnabled bool   `json:"https_enabled"`
		HTTPSListen  string `json:"https_listen"`
	}{
		Info:         s.TLS.CurrentInfo(),
		HTTPSEnabled: s.HTTPSStarted,
		HTTPSListen:  s.Cfg.TLSListen,
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleTLSUpload(w http.ResponseWriter, r *http.Request) {
	var in tlsUploadInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.CertPEM = strings.TrimSpace(in.CertPEM)
	in.KeyPEM = strings.TrimSpace(in.KeyPEM)
	if !strings.HasPrefix(in.CertPEM, "-----BEGIN") {
		writeError(w, http.StatusBadRequest, "cert_pem must be a PEM-encoded certificate")
		return
	}
	if !strings.HasPrefix(in.KeyPEM, "-----BEGIN") {
		writeError(w, http.StatusBadRequest, "key_pem must be a PEM-encoded private key")
		return
	}
	if err := s.TLS.SavePEM([]byte(in.CertPEM), []byte(in.KeyPEM)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.TLS.CurrentInfo())
}

func (s *Server) handleTLSSelfSign(w http.ResponseWriter, r *http.Request) {
	var in tlsSelfSignInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	clean := []string{}
	for _, h := range in.Hostnames {
		h = strings.TrimSpace(h)
		if h != "" && len(h) <= 253 {
			clean = append(clean, h)
		}
	}
	days := in.ValidityDays
	if days <= 0 {
		days = 365
	}
	if days > 10*365 {
		days = 10 * 365
	}
	if err := s.TLS.GenerateSelfSigned(clean, time.Duration(days)*24*time.Hour); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.TLS.CurrentInfo())
}

func (s *Server) handleTLSDelete(w http.ResponseWriter, _ *http.Request) {
	if err := s.TLS.Delete(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

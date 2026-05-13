package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Bartis-Dev/LabExtend/internal/vault"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleVaultState(w http.ResponseWriter, _ *http.Request) {
	st, err := s.Vault.State()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleVaultSetup(w http.ResponseWriter, r *http.Request) {
	var in vault.SetupInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.KDFSalt) < 16 || len(in.KDFSalt) > 64 {
		writeError(w, http.StatusBadRequest, "kdf_salt length must be 16..64 bytes")
		return
	}
	if in.KDFParamsJSON == "" || len(in.KDFParamsJSON) > 512 {
		writeError(w, http.StatusBadRequest, "kdf_params_json missing or too long")
		return
	}
	if len(in.VerifierCiphertext) == 0 || len(in.VerifierNonce) < 12 {
		writeError(w, http.StatusBadRequest, "verifier ciphertext/nonce required")
		return
	}
	// Lightweight sanity check on the JSON.
	var anyJSON map[string]any
	if err := json.Unmarshal([]byte(in.KDFParamsJSON), &anyJSON); err != nil {
		writeError(w, http.StatusBadRequest, "kdf_params_json not valid JSON")
		return
	}
	if err := s.Vault.Setup(in); err != nil {
		if errors.Is(err, vault.ErrAlreadyInitialized) {
			writeError(w, http.StatusConflict, "vault already initialized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleVaultList(w http.ResponseWriter, _ *http.Request) {
	entries, err := s.Vault.ListEntries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleVaultCreate(w http.ResponseWriter, r *http.Request) {
	in, err := decodeEntryInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !requireInitialized(s, w) {
		return
	}
	e, err := s.Vault.CreateEntry(in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleVaultUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	in, err := decodeEntryInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.Vault.UpdateEntry(id, in)
	if errors.Is(err, vault.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleVaultDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Vault.DeleteEntry(id); err != nil {
		if errors.Is(err, vault.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeEntryInput(r *http.Request) (vault.EntryInput, error) {
	var in vault.EntryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return in, errors.New("invalid json")
	}
	if len(in.PayloadCiphertext) == 0 {
		return in, errors.New("payload_ciphertext required")
	}
	if len(in.PayloadNonce) < 12 {
		return in, errors.New("payload_nonce required")
	}
	// Cap payload at ~64 KiB to prevent abuse; real entries are <<1 KiB.
	if len(in.PayloadCiphertext) > 65_536 {
		return in, errors.New("payload too large")
	}
	return in, nil
}

func requireInitialized(s *Server, w http.ResponseWriter) bool {
	st, err := s.Vault.State()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return false
	}
	if !st.Initialized {
		writeError(w, http.StatusBadRequest, "vault not initialized")
		return false
	}
	return true
}

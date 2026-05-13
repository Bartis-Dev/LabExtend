package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Bartis-Dev/LabExtend/internal/wol"
	"github.com/go-chi/chi/v5"
)

func (s *Server) listWoL(w http.ResponseWriter, _ *http.Request) {
	out, err := s.WoL.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createWoL(w http.ResponseWriter, r *http.Request) {
	var in wol.TargetInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := wol.ValidateInput(&in); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	out, err := s.WoL.Create(in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateWoL(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in wol.TargetInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := wol.ValidateInput(&in); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	out, err := s.WoL.Update(id, in)
	if errors.Is(err, wol.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteWoL(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.WoL.Delete(id); err != nil {
		if errors.Is(err, wol.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) wakeWoL(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	t, err := s.WoL.Get(id)
	if errors.Is(err, wol.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if err := wol.Wake(t); err != nil {
		_ = s.WoL.RecordWakeError(id, err.Error())
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	_ = s.WoL.RecordWakeSuccess(id)
	w.WriteHeader(http.StatusNoContent)
}

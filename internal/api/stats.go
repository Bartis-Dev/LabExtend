package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/stats"
	"github.com/go-chi/chi/v5"
)

// ---- Sources -------------------------------------------------------------

type sourceInput struct {
	Name string `json:"name"`
	Unit string `json:"unit"`
}

func (s *Server) listStatsSources(w http.ResponseWriter, _ *http.Request) {
	out, err := s.Stats.ListSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createStatsSource(w http.ResponseWriter, r *http.Request) {
	in, err := decodeSource(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.Stats.CreateSource(in.Name, in.Unit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateStatsSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	in, err := decodeSource(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.Stats.UpdateSource(id, in.Name, in.Unit)
	if errors.Is(err, stats.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) rotateStatsSourceToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	out, err := s.Stats.RotateSourceToken(id)
	if errors.Is(err, stats.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteStatsSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Stats.DeleteSource(id); err != nil {
		if errors.Is(err, stats.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeSource(r *http.Request) (sourceInput, error) {
	var in sourceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return in, errors.New("invalid json")
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Unit = strings.TrimSpace(in.Unit)
	if l := len(in.Name); l < 1 || l > 64 {
		return in, errors.New("name must be 1..64 chars")
	}
	if len(in.Unit) > 32 {
		return in, errors.New("unit must be at most 32 chars")
	}
	return in, nil
}

// ---- Points query --------------------------------------------------------

func (s *Server) queryStatsPoints(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	now := time.Now().Unix()
	from := now - 3600 // default last hour
	to := now
	if v := r.URL.Query().Get("from"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			from = n
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			to = n
		}
	}
	maxPoints := 500
	if v := r.URL.Query().Get("max"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			maxPoints = n
		}
	}
	pts, err := s.Stats.QueryPoints(sourceID, from, to, maxPoints)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	latest, _ := s.Stats.LatestPoint(sourceID)
	writeJSON(w, http.StatusOK, map[string]any{
		"from":   from,
		"to":     to,
		"points": pts,
		"latest": latest,
	})
}

// ---- Widgets -------------------------------------------------------------

type widgetInput struct {
	SourceID         int64  `json:"source_id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	TimeRangeMinutes int    `json:"time_range_minutes"`
	Position         int    `json:"position"`
	ConfigJSON       string `json:"config_json"`
}

func (s *Server) listStatsWidgets(w http.ResponseWriter, _ *http.Request) {
	out, err := s.Stats.ListWidgets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createStatsWidget(w http.ResponseWriter, r *http.Request) {
	wg, err := decodeWidget(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.Stats.CreateWidget(wg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateStatsWidget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	wg, err := decodeWidget(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.Stats.UpdateWidget(id, wg)
	if errors.Is(err, stats.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteStatsWidget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Stats.DeleteWidget(id); err != nil {
		if errors.Is(err, stats.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeWidget(r *http.Request) (stats.Widget, error) {
	var in widgetInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return stats.Widget{}, errors.New("invalid json")
	}
	in.Name = strings.TrimSpace(in.Name)
	if l := len(in.Name); l < 1 || l > 80 {
		return stats.Widget{}, errors.New("name must be 1..80 chars")
	}
	if in.SourceID <= 0 {
		return stats.Widget{}, errors.New("source_id required")
	}
	if in.Kind == "" {
		in.Kind = "line"
	}
	if in.Kind != "line" && in.Kind != "gauge" {
		return stats.Widget{}, errors.New("kind must be 'line' or 'gauge'")
	}
	if in.TimeRangeMinutes < 5 || in.TimeRangeMinutes > 30*24*60 {
		return stats.Widget{}, errors.New("time_range_minutes must be 5..43200 (5 min .. 30 days)")
	}
	return stats.Widget{
		SourceID:         in.SourceID,
		Name:             in.Name,
		Kind:             in.Kind,
		TimeRangeMinutes: in.TimeRangeMinutes,
		Position:         in.Position,
		ConfigJSON:       in.ConfigJSON,
	}, nil
}

// ---- Ingest (public, token-auth) -----------------------------------------

type ingestInput struct {
	Value float64 `json:"value"`
	TS    *int64  `json:"ts"` // optional unix seconds; defaults to now
}

func (s *Server) ingestStatsPoint(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if l := len(token); l < 16 || l > 64 {
		writeError(w, http.StatusBadRequest, "invalid token")
		return
	}
	src, err := s.Stats.GetSourceByToken(token)
	if errors.Is(err, stats.ErrNotFound) {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	var in ingestInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ts := time.Now().Unix()
	if in.TS != nil {
		ts = *in.TS
	}
	if err := s.Stats.InsertPoint(src.ID, ts, in.Value); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

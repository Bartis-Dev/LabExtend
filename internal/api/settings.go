package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Bartis-Dev/LabExtend/internal/config"
	settingsstore "github.com/Bartis-Dev/LabExtend/internal/settings"
)

// allowedSettings is the public allowlist of keys that GET/PUT settings
// exposes. JWT secret and any other sensitive key MUST stay out of this
// list. The validator runs the input through type and range checks.
var allowedSettings = map[string]func(value string) (string, error){
	settingsstore.KeyGridCols: func(v string) (string, error) {
		n, err := strconv.Atoi(v)
		if err != nil {
			return "", &validationError{Msg: "grid_cols must be an integer"}
		}
		if n < 4 || n > 12 {
			return "", &validationError{Msg: "grid_cols must be between 4 and 12"}
		}
		return strconv.Itoa(n), nil
	},
	settingsstore.KeyHealthcheckInterval: func(v string) (string, error) {
		d, err := config.ParseDuration(v)
		if err != nil {
			return "", &validationError{Msg: "healthcheck_interval must be a duration (e.g. 30s, 1m)"}
		}
		if d < 10_000_000_000 || d > 3_600_000_000_000 { // 10s..1h
			return "", &validationError{Msg: "healthcheck_interval must be between 10s and 1h"}
		}
		return v, nil
	},
}

type validationError struct{ Msg string }

func (e *validationError) Error() string { return e.Msg }

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	all, err := s.Settings.All()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := map[string]string{}
	for k, v := range all {
		if _, ok := allowedSettings[k]; ok {
			out[k] = v
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Validate before writing anything; reject the whole payload on any error.
	clean := map[string]string{}
	for k, v := range in {
		validator, ok := allowedSettings[k]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown setting: "+k)
			return
		}
		cleaned, err := validator(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, k+": "+err.Error())
			return
		}
		clean[k] = cleaned
	}
	for k, v := range clean {
		if err := s.Settings.Set(k, v); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
	// Side effects.
	if v, ok := clean[settingsstore.KeyHealthcheckInterval]; ok && s.Worker != nil {
		if d, err := config.ParseDuration(v); err == nil {
			s.Worker.SetInterval(d)
		}
	}
	s.handleGetSettings(w, r)
}

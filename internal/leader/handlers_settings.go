package leader

import (
	"encoding/json"
	"errors"
	"net/http"
)

// SettingsDeps serves the admin settings page. Values live in system_state.
type SettingsDeps struct {
	Audit *AuditLogger
}

// Get returns the stored settings and what they currently resolve to.
func (d *SettingsDeps) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"audit": d.Audit.State(r.Context())})
}

// Update changes settings. Only audit_mode exists so far.
func (d *SettingsDeps) Update(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AuditMode string `json:"audit_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !validAuditMode(body.AuditMode) {
		writeErr(w, http.StatusBadRequest, errors.New("audit_mode must be auto, on or off"))
		return
	}
	details := map[string]any{"from": d.Audit.State(r.Context()).Mode, "to": body.AuditMode}
	// Switching to off is logged while logging still works, so it leaves a trace.
	if body.AuditMode == auditModeOff {
		d.Audit.Log(r.Context(), r, "settings.update", "setting", auditModeKey, details)
	}
	if err := d.Audit.SetMode(r.Context(), body.AuditMode); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if body.AuditMode != auditModeOff {
		d.Audit.Log(r.Context(), r, "settings.update", "setting", auditModeKey, details)
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": d.Audit.State(r.Context())})
}

// requireFileBrowser closes the file browser routes unless BPM_FILEBROWSER is on.
func requireFileBrowser(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				writeErr(w, http.StatusForbidden, errors.New("file browser is disabled (BPM_FILEBROWSER)"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

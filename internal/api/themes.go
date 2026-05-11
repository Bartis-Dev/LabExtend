package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type Theme struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Palette   map[string]string `json:"palette"`
	CustomCSS string            `json:"custom_css"`
	IsDefault bool              `json:"is_default"`
	IsActive  bool              `json:"is_active"`
}

type themeInput struct {
	Name      string            `json:"name"`
	Palette   map[string]string `json:"palette"`
	CustomCSS string            `json:"custom_css"`
}

func scanTheme(scanner interface{ Scan(...any) error }) (Theme, error) {
	var t Theme
	var paletteStr string
	var isDefault, isActive int
	if err := scanner.Scan(&t.ID, &t.Name, &paletteStr, &t.CustomCSS, &isDefault, &isActive); err != nil {
		return t, err
	}
	t.IsDefault = isDefault == 1
	t.IsActive = isActive == 1
	if err := json.Unmarshal([]byte(paletteStr), &t.Palette); err != nil {
		t.Palette = map[string]string{}
	}
	return t, nil
}

func (s *Server) listThemes(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.DB.Query(`SELECT id, name, palette_json, custom_css, is_default, is_active FROM themes ORDER BY id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	out := []Theme{}
	for rows.Next() {
		t, err := scanTheme(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createTheme(w http.ResponseWriter, r *http.Request) {
	var in themeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if in.Palette == nil {
		in.Palette = map[string]string{}
	}
	paletteBytes, _ := json.Marshal(in.Palette)
	now := time.Now().Unix()
	res, err := s.DB.Exec(
		`INSERT INTO themes (name, palette_json, custom_css, is_default, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, 0, 0, ?, ?)`,
		in.Name, string(paletteBytes), in.CustomCSS, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "theme name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusOK, themeByID(s.DB, id))
}

func (s *Server) updateTheme(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in themeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	paletteBytes, _ := json.Marshal(in.Palette)
	res, err := s.DB.Exec(
		`UPDATE themes SET name=?, palette_json=?, custom_css=?, updated_at=? WHERE id=?`,
		in.Name, string(paletteBytes), in.CustomCSS, time.Now().Unix(), id,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "theme name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, themeByID(s.DB, id))
}

// deleteTheme refuses to drop the default theme. If the deleted theme is
// currently active, the default theme becomes active in the same tx.
func (s *Server) deleteTheme(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var isDefault int
	if err := s.DB.QueryRow(`SELECT is_default FROM themes WHERE id=?`, id).Scan(&isDefault); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if isDefault == 1 {
		writeError(w, http.StatusForbidden, "cannot delete the default theme")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx error")
		return
	}
	res, err := tx.Exec(`DELETE FROM themes WHERE id=?`, id)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_ = tx.Rollback()
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// Activate the default theme if nothing is active.
	var activeCount int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM themes WHERE is_active=1`).Scan(&activeCount)
	if activeCount == 0 {
		if _, err := tx.Exec(`UPDATE themes SET is_active=1 WHERE is_default=1`); err != nil {
			_ = tx.Rollback()
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "commit")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) activateTheme(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx error")
		return
	}
	if _, err := tx.Exec(`UPDATE themes SET is_active=0`); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	res, err := tx.Exec(`UPDATE themes SET is_active=1 WHERE id=?`, id)
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "commit")
		return
	}
	writeJSON(w, http.StatusOK, themeByID(s.DB, id))
}

func themeByID(db *sql.DB, id int64) Theme {
	row := db.QueryRow(`SELECT id, name, palette_json, custom_css, is_default, is_active FROM themes WHERE id=?`, id)
	t, _ := scanTheme(row)
	return t
}


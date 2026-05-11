package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// Service mirrors the row layout but presents `layout` as a nested object
// matching react-grid-layout's shape.
type Service struct {
	ID               int64       `json:"id"`
	Name             string      `json:"name"`
	Description      string      `json:"description"`
	HostPrimary      string      `json:"host_primary"`
	PortPrimary      *int        `json:"port_primary"`
	HostAlt          *string     `json:"host_alt"`
	PortAlt          *int        `json:"port_alt"`
	IconPath         *string     `json:"icon_path"`
	CategoryID       *int64      `json:"category_id"`
	Layout           LayoutRect  `json:"layout"`
	PingPrimary      bool        `json:"ping_primary"`
	PingAlt          bool        `json:"ping_alt"`
	HCPrimaryEnabled bool        `json:"hc_primary_enabled"`
	HCPrimaryURL     *string     `json:"hc_primary_url"`
	HCAltEnabled     bool        `json:"hc_alt_enabled"`
	HCAltURL         *string     `json:"hc_alt_url"`
}

type LayoutRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type serviceInput struct {
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	HostPrimary      string     `json:"host_primary"`
	PortPrimary      *int       `json:"port_primary"`
	HostAlt          *string    `json:"host_alt"`
	PortAlt          *int       `json:"port_alt"`
	CategoryID       *int64     `json:"category_id"`
	Layout           LayoutRect `json:"layout"`
	PingPrimary      bool       `json:"ping_primary"`
	PingAlt          bool       `json:"ping_alt"`
	HCPrimaryEnabled bool       `json:"hc_primary_enabled"`
	HCPrimaryURL     *string    `json:"hc_primary_url"`
	HCAltEnabled     bool       `json:"hc_alt_enabled"`
	HCAltURL         *string    `json:"hc_alt_url"`
}

const serviceSelectCols = `
  id, name, description, host_primary, port_primary, host_alt, port_alt,
  icon_path, category_id,
  layout_x, layout_y, layout_w, layout_h,
  ping_primary, ping_alt,
  hc_primary_enabled, hc_primary_url, hc_alt_enabled, hc_alt_url
`

func scanService(scanner interface{ Scan(...any) error }) (Service, error) {
	var s Service
	var pingP, pingA, hcPE, hcAE int
	err := scanner.Scan(
		&s.ID, &s.Name, &s.Description, &s.HostPrimary, &s.PortPrimary,
		&s.HostAlt, &s.PortAlt, &s.IconPath, &s.CategoryID,
		&s.Layout.X, &s.Layout.Y, &s.Layout.W, &s.Layout.H,
		&pingP, &pingA, &hcPE, &s.HCPrimaryURL, &hcAE, &s.HCAltURL,
	)
	if err != nil {
		return s, err
	}
	s.PingPrimary = pingP == 1
	s.PingAlt = pingA == 1
	s.HCPrimaryEnabled = hcPE == 1
	s.HCAltEnabled = hcAE == 1
	return s, nil
}

func validateServiceInput(in *serviceInput) string {
	if in.Name == "" {
		return "name is required"
	}
	if in.HostPrimary == "" {
		return "host_primary is required"
	}
	if in.Layout.W <= 0 {
		in.Layout.W = 1
	}
	if in.Layout.H <= 0 {
		in.Layout.H = 1
	}
	return ""
}

func (s *Server) listServices(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.DB.Query(`SELECT ` + serviceSelectCols + ` FROM services ORDER BY id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	out := []Service{}
	for rows.Next() {
		svc, err := scanService(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		out = append(out, svc)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	var in serviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := validateServiceInput(&in); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	now := time.Now().Unix()
	res, err := s.DB.Exec(
		`INSERT INTO services (
		  name, description, host_primary, port_primary, host_alt, port_alt,
		  category_id,
		  layout_x, layout_y, layout_w, layout_h,
		  ping_primary, ping_alt,
		  hc_primary_enabled, hc_primary_url, hc_alt_enabled, hc_alt_url,
		  created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.Name, in.Description, in.HostPrimary, in.PortPrimary,
		in.HostAlt, in.PortAlt, in.CategoryID,
		in.Layout.X, in.Layout.Y, in.Layout.W, in.Layout.H,
		boolInt(in.PingPrimary), boolInt(in.PingAlt),
		boolInt(in.HCPrimaryEnabled), in.HCPrimaryURL,
		boolInt(in.HCAltEnabled), in.HCAltURL,
		now, now,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	id, _ := res.LastInsertId()
	svc := serviceByID(s.DB, id)
	writeJSON(w, http.StatusOK, svc)
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	row := s.DB.QueryRow(`SELECT `+serviceSelectCols+` FROM services WHERE id=?`, id)
	svc, err := scanService(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

func (s *Server) updateService(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in serviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := validateServiceInput(&in); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	res, err := s.DB.Exec(
		`UPDATE services SET
		  name=?, description=?, host_primary=?, port_primary=?, host_alt=?, port_alt=?,
		  category_id=?,
		  layout_x=?, layout_y=?, layout_w=?, layout_h=?,
		  ping_primary=?, ping_alt=?,
		  hc_primary_enabled=?, hc_primary_url=?, hc_alt_enabled=?, hc_alt_url=?,
		  updated_at=?
		WHERE id=?`,
		in.Name, in.Description, in.HostPrimary, in.PortPrimary,
		in.HostAlt, in.PortAlt, in.CategoryID,
		in.Layout.X, in.Layout.Y, in.Layout.W, in.Layout.H,
		boolInt(in.PingPrimary), boolInt(in.PingAlt),
		boolInt(in.HCPrimaryEnabled), in.HCPrimaryURL,
		boolInt(in.HCAltEnabled), in.HCAltURL,
		time.Now().Unix(), id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, serviceByID(s.DB, id))
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	res, err := s.DB.Exec(`DELETE FROM services WHERE id=?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func serviceByID(db *sql.DB, id int64) Service {
	row := db.QueryRow(`SELECT `+serviceSelectCols+` FROM services WHERE id=?`, id)
	svc, _ := scanService(row)
	return svc
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

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

type Category struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	BorderColor string     `json:"border_color"`
	Layout      LayoutRect `json:"layout"`
}

type categoryInput struct {
	Name        string     `json:"name"`
	BorderColor string     `json:"border_color"`
	Layout      LayoutRect `json:"layout"`
}

func scanCategory(scanner interface{ Scan(...any) error }) (Category, error) {
	var c Category
	err := scanner.Scan(
		&c.ID, &c.Name, &c.BorderColor,
		&c.Layout.X, &c.Layout.Y, &c.Layout.W, &c.Layout.H,
	)
	return c, err
}

func (s *Server) listCategories(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.DB.Query(`SELECT id, name, border_color, layout_x, layout_y, layout_w, layout_h FROM categories ORDER BY id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if in.BorderColor == "" {
		in.BorderColor = "#475569"
	}
	if in.Layout.W <= 0 {
		in.Layout.W = 3
	}
	if in.Layout.H <= 0 {
		in.Layout.H = 2
	}
	now := time.Now().Unix()
	res, err := s.DB.Exec(
		`INSERT INTO categories (name, border_color, layout_x, layout_y, layout_w, layout_h, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		in.Name, in.BorderColor, in.Layout.X, in.Layout.Y, in.Layout.W, in.Layout.H, now, now,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusOK, categoryByID(s.DB, id))
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in categoryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	res, err := s.DB.Exec(
		`UPDATE categories SET name=?, border_color=?, layout_x=?, layout_y=?, layout_w=?, layout_h=?, updated_at=? WHERE id=?`,
		in.Name, in.BorderColor, in.Layout.X, in.Layout.Y, in.Layout.W, in.Layout.H, time.Now().Unix(), id,
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
	writeJSON(w, http.StatusOK, categoryByID(s.DB, id))
}

// deleteCategory removes the category and detaches all member services
// (sets their category_id = NULL) in a single transaction.
func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
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
	if _, err := tx.Exec(`UPDATE services SET category_id=NULL WHERE category_id=?`, id); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	res, err := tx.Exec(`DELETE FROM categories WHERE id=?`, id)
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
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "commit error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func categoryByID(db *sql.DB, id int64) Category {
	row := db.QueryRow(`SELECT id, name, border_color, layout_x, layout_y, layout_w, layout_h FROM categories WHERE id=?`, id)
	c, _ := scanCategory(row)
	return c
}

var errNotFound = errors.New("not found")

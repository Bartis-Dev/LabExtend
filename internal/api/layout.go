package api

import (
	"encoding/json"
	"net/http"
	"time"
)

type layoutItem struct {
	ID         int64  `json:"id"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	W          int    `json:"w"`
	H          int    `json:"h"`
	CategoryID *int64 `json:"category_id,omitempty"` // optional, only honored on service items
}

type layoutBulkReq struct {
	Services   []layoutItem `json:"services"`
	Categories []layoutItem `json:"categories"`
}

// handleLayoutBulk applies a batched layout update from a drag-end event.
// Service entries may include category_id to move into/out of a category
// in the same transaction. Categories entries only update geometry.
func (s *Server) handleLayoutBulk(w http.ResponseWriter, r *http.Request) {
	var req layoutBulkReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx error")
		return
	}
	now := time.Now().Unix()
	for _, it := range req.Services {
		_, err := tx.Exec(
			`UPDATE services SET layout_x=?, layout_y=?, layout_w=?, layout_h=?, category_id=?, updated_at=? WHERE id=?`,
			it.X, it.Y, it.W, it.H, it.CategoryID, now, it.ID,
		)
		if err != nil {
			_ = tx.Rollback()
			writeError(w, http.StatusInternalServerError, "update service")
			return
		}
	}
	for _, it := range req.Categories {
		_, err := tx.Exec(
			`UPDATE categories SET layout_x=?, layout_y=?, layout_w=?, layout_h=?, updated_at=? WHERE id=?`,
			it.X, it.Y, it.W, it.H, now, it.ID,
		)
		if err != nil {
			_ = tx.Rollback()
			writeError(w, http.StatusInternalServerError, "update category")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "commit")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

package leader

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Bartis-Dev/LabExtend/internal/notify"
)

// listNotifications returns the newest notifications plus the unread count.
// ?limit=<n> (default 50, max 500).
func listNotifications(c *notify.Center) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		list, unread, err := c.List(r.Context(), limit)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"notifications": list, "unread": unread})
	}
}

// markNotificationsRead marks {id} read, or all of them on /read-all.
func markNotificationsRead(c *notify.Center) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id int64
		if s := chi.URLParam(r, "id"); s != "" {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil || n <= 0 {
				writeErr(w, http.StatusBadRequest, errors.New("invalid notification id"))
				return
			}
			id = n
		}
		if err := c.MarkRead(r.Context(), id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

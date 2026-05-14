package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Bartis-Dev/LabExtend/internal/notes"
	"github.com/go-chi/chi/v5"
)

func (s *Server) listNotes(w http.ResponseWriter, _ *http.Request) {
	st, err := s.Notes.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) createNotesCard(w http.ResponseWriter, r *http.Request) {
	var in notes.CardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	out, err := s.Notes.CreateCard(in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateNotesCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in notes.CardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	out, err := s.Notes.UpdateCard(id, in)
	if errors.Is(err, notes.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// patchNotesCardLayout is the lightweight endpoint the canvas hits while
// the user is dragging a card (debounced) and on drop.
func (s *Server) patchNotesCardLayout(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W int     `json:"w"`
		H int     `json:"h"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.W <= 0 {
		in.W = 280
	}
	if in.H <= 0 {
		in.H = 120
	}
	if err := s.Notes.PatchCardLayout(id, in.X, in.Y, in.W, in.H); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteNotesCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Notes.DeleteCard(id); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Items ----------------------------------------------------------------

func (s *Server) createNotesItem(w http.ResponseWriter, r *http.Request) {
	cardID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in notes.ItemInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	out, err := s.Notes.CreateItem(cardID, in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateNotesItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in notes.ItemInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	out, err := s.Notes.UpdateItem(id, in)
	if errors.Is(err, notes.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteNotesItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Notes.DeleteItem(id); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Item move (cross-card reorder) --------------------------------------

func (s *Server) moveNotesItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in struct {
		CardID   int64 `json:"card_id"`
		Position int   `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.CardID <= 0 {
		writeError(w, http.StatusBadRequest, "card_id required")
		return
	}
	if err := s.Notes.MoveItem(id, in.CardID, in.Position); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Boards ---------------------------------------------------------------

func (s *Server) createNotesBoard(w http.ResponseWriter, r *http.Request) {
	var in notes.BoardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Cols < 2 || in.Cols > 10 {
		writeError(w, http.StatusBadRequest, "cols must be 2..10")
		return
	}
	board, err := s.Notes.CreateBoard(in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	// Seed the board with `cols` empty cards so the user has slots to
	// fill in immediately.
	for slot := 0; slot < in.Cols; slot++ {
		boardID := board.ID
		_, err := s.Notes.CreateCard(notes.CardInput{
			Name:      "",
			Color:     in.Color,
			BoardID:   &boardID,
			SlotIndex: slot,
		})
		if err != nil {
			// Best-effort cleanup, then report.
			_ = s.Notes.DeleteBoard(board.ID)
			writeError(w, http.StatusInternalServerError, "db error seeding board")
			return
		}
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) updateNotesBoard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in notes.BoardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	out, err := s.Notes.UpdateBoard(id, in)
	if errors.Is(err, notes.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) patchNotesBoardPosition(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.Notes.PatchBoardPosition(id, in.X, in.Y); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) appendNotesBoardCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	card, board, err := s.Notes.AppendCardToBoard(id)
	if errors.Is(err, notes.ErrNotFound) {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"card": card, "board": board})
}

func (s *Server) deleteNotesBoard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Notes.DeleteBoard(id); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Card slot swap (inside a board) -------------------------------------

func (s *Server) swapNotesCardSlots(w http.ResponseWriter, r *http.Request) {
	var in struct {
		A int64 `json:"a"`
		B int64 `json:"b"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.A == 0 || in.B == 0 || in.A == in.B {
		writeError(w, http.StatusBadRequest, "both a and b ids required and must differ")
		return
	}
	if err := s.Notes.SwapCardSlots(in.A, in.B); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

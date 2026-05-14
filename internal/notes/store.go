// Package notes owns the Notes module: free-canvas Trello-style cards
// with sub-notes, plus optional "canvas boards" — multi-column groups
// that pin 2..5 cards in a horizontal row. Cards positioned with
// (x, y) live freely on the canvas; cards with a board_id are rendered
// inside the board at their slot_index instead.
package notes

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Card struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          int     `json:"w"`
	H          int     `json:"h"`
	Color      string  `json:"color"`
	TitleColor string  `json:"title_color"`
	BoardID    *int64  `json:"board_id"`
	SlotIndex  int     `json:"slot_index"`
	CreatedAt  int64   `json:"created_at"`
	UpdatedAt  int64   `json:"updated_at"`
	Items      []Item  `json:"items"`
}

type Item struct {
	ID         int64  `json:"id"`
	CardID     int64  `json:"card_id"`
	Text       string `json:"text"`
	IsFavorite bool   `json:"is_favorite"`
	Position   int    `json:"position"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type Board struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Cols       int     `json:"cols"`
	Color      string  `json:"color"`
	TitleColor string  `json:"title_color"`
	CreatedAt  int64   `json:"created_at"`
	UpdatedAt  int64   `json:"updated_at"`
}

type CardInput struct {
	Name       string  `json:"name"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          int     `json:"w"`
	H          int     `json:"h"`
	Color      string  `json:"color"`
	TitleColor string  `json:"title_color"`
	BoardID    *int64  `json:"board_id"`
	SlotIndex  int     `json:"slot_index"`
}

type ItemInput struct {
	Text       string `json:"text"`
	IsFavorite bool   `json:"is_favorite"`
	Position   int    `json:"position"`
}

type BoardInput struct {
	Name       string  `json:"name"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Cols       int     `json:"cols"`
	Color      string  `json:"color"`
	TitleColor string  `json:"title_color"`
}

// State is the full Notes graph returned by ListAll.
type State struct {
	Cards  []Card  `json:"cards"`
	Boards []Board `json:"boards"`
}

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

// ListAll returns every card (with items pre-loaded) and every board in
// one go. The frontend gets everything it needs from a single request
// and decides what to render based on Card.BoardID.
func (s *Store) ListAll() (State, error) {
	st := State{Cards: []Card{}, Boards: []Board{}}

	cardRows, err := s.db.Query(
		`SELECT id, name, x_real, y_real, w, h, color, title_color, board_id, slot_index, created_at, updated_at
		 FROM notes_cards ORDER BY id`,
	)
	if err != nil {
		return st, err
	}
	for cardRows.Next() {
		var c Card
		if err := cardRows.Scan(&c.ID, &c.Name, &c.X, &c.Y, &c.W, &c.H, &c.Color, &c.TitleColor,
			&c.BoardID, &c.SlotIndex, &c.CreatedAt, &c.UpdatedAt); err != nil {
			cardRows.Close()
			return st, err
		}
		c.Items = []Item{}
		st.Cards = append(st.Cards, c)
	}
	cardRows.Close()

	itemRows, err := s.db.Query(
		`SELECT id, card_id, text, is_favorite, position, created_at, updated_at FROM notes_items ORDER BY card_id, position, id`,
	)
	if err != nil {
		return st, err
	}
	byCard := map[int64]*Card{}
	for i := range st.Cards {
		byCard[st.Cards[i].ID] = &st.Cards[i]
	}
	for itemRows.Next() {
		var it Item
		var fav int
		if err := itemRows.Scan(&it.ID, &it.CardID, &it.Text, &fav, &it.Position, &it.CreatedAt, &it.UpdatedAt); err != nil {
			itemRows.Close()
			return st, err
		}
		it.IsFavorite = fav == 1
		if c, ok := byCard[it.CardID]; ok {
			c.Items = append(c.Items, it)
		}
	}
	itemRows.Close()

	boardRows, err := s.db.Query(
		`SELECT id, name, x_real, y_real, cols, color, title_color, created_at, updated_at FROM notes_boards ORDER BY id`,
	)
	if err != nil {
		return st, err
	}
	defer boardRows.Close()
	for boardRows.Next() {
		var b Board
		if err := boardRows.Scan(&b.ID, &b.Name, &b.X, &b.Y, &b.Cols, &b.Color, &b.TitleColor, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return st, err
		}
		st.Boards = append(st.Boards, b)
	}
	return st, boardRows.Err()
}

// --- Cards ---------------------------------------------------------------

func (s *Store) CreateCard(in CardInput) (Card, error) {
	now := time.Now().Unix()
	if in.W <= 0 {
		in.W = 280
	}
	if in.H <= 0 {
		in.H = 120
	}
	if in.Color == "" {
		in.Color = "#475569"
	}
	res, err := s.db.Exec(
		`INSERT INTO notes_cards (name, x_real, y_real, w, h, color, title_color, board_id, slot_index, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		in.Name, in.X, in.Y, in.W, in.H, in.Color, in.TitleColor, in.BoardID, in.SlotIndex, now, now,
	)
	if err != nil {
		return Card{}, err
	}
	id, _ := res.LastInsertId()
	return s.getCard(id)
}

func (s *Store) UpdateCard(id int64, in CardInput) (Card, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE notes_cards SET name=?, x_real=?, y_real=?, w=?, h=?, color=?, title_color=?, updated_at=? WHERE id=?`,
		in.Name, in.X, in.Y, in.W, in.H, in.Color, in.TitleColor, now, id,
	)
	if err != nil {
		return Card{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Card{}, ErrNotFound
	}
	return s.getCard(id)
}

func (s *Store) PatchCardLayout(id int64, x, y float64, w, h int) error {
	res, err := s.db.Exec(
		`UPDATE notes_cards SET x_real=?, y_real=?, w=?, h=?, updated_at=? WHERE id=?`,
		x, y, w, h, time.Now().Unix(), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SwapCardSlots swaps two cards' slot_index values inside the same board.
// Used when the user drags a card to a different slot within a board.
func (s *Store) SwapCardSlots(aID, bID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var aBoard, bBoard sql.NullInt64
	var aSlot, bSlot int
	if err := tx.QueryRow(`SELECT board_id, slot_index FROM notes_cards WHERE id=?`, aID).Scan(&aBoard, &aSlot); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT board_id, slot_index FROM notes_cards WHERE id=?`, bID).Scan(&bBoard, &bSlot); err != nil {
		return err
	}
	if !aBoard.Valid || !bBoard.Valid || aBoard.Int64 != bBoard.Int64 {
		return fmt.Errorf("cards must be in the same board to swap")
	}
	now := time.Now().Unix()
	if _, err := tx.Exec(`UPDATE notes_cards SET slot_index=?, updated_at=? WHERE id=?`, bSlot, now, aID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE notes_cards SET slot_index=?, updated_at=? WHERE id=?`, aSlot, now, bID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteCard(id int64) error {
	res, err := s.db.Exec(`DELETE FROM notes_cards WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) getCard(id int64) (Card, error) {
	var c Card
	row := s.db.QueryRow(
		`SELECT id, name, x_real, y_real, w, h, color, title_color, board_id, slot_index, created_at, updated_at FROM notes_cards WHERE id=?`,
		id,
	)
	if err := row.Scan(&c.ID, &c.Name, &c.X, &c.Y, &c.W, &c.H, &c.Color, &c.TitleColor,
		&c.BoardID, &c.SlotIndex, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	c.Items = []Item{}
	rows, err := s.db.Query(
		`SELECT id, card_id, text, is_favorite, position, created_at, updated_at FROM notes_items WHERE card_id=? ORDER BY position, id`,
		id,
	)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		var fav int
		if err := rows.Scan(&it.ID, &it.CardID, &it.Text, &fav, &it.Position, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return c, err
		}
		it.IsFavorite = fav == 1
		c.Items = append(c.Items, it)
	}
	return c, rows.Err()
}

// --- Items ---------------------------------------------------------------

func (s *Store) CreateItem(cardID int64, in ItemInput) (Item, error) {
	now := time.Now().Unix()
	fav := 0
	if in.IsFavorite {
		fav = 1
	}
	var maxPos sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(position) FROM notes_items WHERE card_id=?`, cardID).Scan(&maxPos)
	pos := int(maxPos.Int64) + 1
	if in.Position > 0 {
		pos = in.Position
	}
	res, err := s.db.Exec(
		`INSERT INTO notes_items (card_id, text, is_favorite, position, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		cardID, in.Text, fav, pos, now, now,
	)
	if err != nil {
		return Item{}, err
	}
	id, _ := res.LastInsertId()
	return s.getItem(id)
}

func (s *Store) UpdateItem(id int64, in ItemInput) (Item, error) {
	now := time.Now().Unix()
	fav := 0
	if in.IsFavorite {
		fav = 1
	}
	res, err := s.db.Exec(
		`UPDATE notes_items SET text=?, is_favorite=?, position=?, updated_at=? WHERE id=?`,
		in.Text, fav, in.Position, now, id,
	)
	if err != nil {
		return Item{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Item{}, ErrNotFound
	}
	return s.getItem(id)
}

func (s *Store) DeleteItem(id int64) error {
	res, err := s.db.Exec(`DELETE FROM notes_items WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveItem moves an item to a new card / new position within the same
// card. The positions of all siblings in both source and destination
// cards are rewritten in a single transaction so they stay dense and
// gap-free regardless of how many moves the user does.
func (s *Store) MoveItem(itemID, newCardID int64, newPosition int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldCardID int64
	if err := tx.QueryRow(`SELECT card_id FROM notes_items WHERE id=?`, itemID).Scan(&oldCardID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	// Confirm destination card exists.
	var existsDest int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM notes_cards WHERE id=?`, newCardID).Scan(&existsDest); err != nil {
		return err
	}
	if existsDest == 0 {
		return ErrNotFound
	}

	now := time.Now().Unix()
	if oldCardID == newCardID {
		// Reorder within the same card.
		ids, err := orderedItemIDs(tx, oldCardID)
		if err != nil {
			return err
		}
		ids = removeID(ids, itemID)
		if newPosition > len(ids) {
			newPosition = len(ids)
		}
		if newPosition < 0 {
			newPosition = 0
		}
		ids = insertIDAt(ids, itemID, newPosition)
		if err := renumber(tx, oldCardID, ids, now); err != nil {
			return err
		}
	} else {
		oldIDs, err := orderedItemIDs(tx, oldCardID)
		if err != nil {
			return err
		}
		newIDs, err := orderedItemIDs(tx, newCardID)
		if err != nil {
			return err
		}
		oldIDs = removeID(oldIDs, itemID)
		if newPosition > len(newIDs) {
			newPosition = len(newIDs)
		}
		if newPosition < 0 {
			newPosition = 0
		}
		newIDs = insertIDAt(newIDs, itemID, newPosition)
		if _, err := tx.Exec(`UPDATE notes_items SET card_id=?, updated_at=? WHERE id=?`, newCardID, now, itemID); err != nil {
			return err
		}
		if err := renumber(tx, oldCardID, oldIDs, now); err != nil {
			return err
		}
		if err := renumber(tx, newCardID, newIDs, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func orderedItemIDs(tx *sql.Tx, cardID int64) ([]int64, error) {
	rows, err := tx.Query(`SELECT id FROM notes_items WHERE card_id=? ORDER BY position, id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func removeID(ids []int64, target int64) []int64 {
	out := ids[:0]
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return out
}

func insertIDAt(ids []int64, id int64, pos int) []int64 {
	if pos >= len(ids) {
		return append(ids, id)
	}
	out := make([]int64, 0, len(ids)+1)
	out = append(out, ids[:pos]...)
	out = append(out, id)
	out = append(out, ids[pos:]...)
	return out
}

func renumber(tx *sql.Tx, cardID int64, orderedIDs []int64, now int64) error {
	for pos, id := range orderedIDs {
		if _, err := tx.Exec(`UPDATE notes_items SET position=?, updated_at=? WHERE id=?`, pos, now, id); err != nil {
			return err
		}
	}
	// Card-level touch so refetch picks up "something changed".
	_, err := tx.Exec(`UPDATE notes_cards SET updated_at=? WHERE id=?`, now, cardID)
	return err
}

func (s *Store) getItem(id int64) (Item, error) {
	var it Item
	var fav int
	row := s.db.QueryRow(
		`SELECT id, card_id, text, is_favorite, position, created_at, updated_at FROM notes_items WHERE id=?`,
		id,
	)
	if err := row.Scan(&it.ID, &it.CardID, &it.Text, &fav, &it.Position, &it.CreatedAt, &it.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return it, ErrNotFound
		}
		return it, err
	}
	it.IsFavorite = fav == 1
	return it, nil
}

// --- Boards --------------------------------------------------------------

func (s *Store) CreateBoard(in BoardInput) (Board, error) {
	if in.Cols < 2 || in.Cols > 10 {
		in.Cols = 2
	}
	if in.Color == "" {
		in.Color = "#475569"
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO notes_boards (name, x_real, y_real, cols, color, title_color, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		in.Name, in.X, in.Y, in.Cols, in.Color, in.TitleColor, now, now,
	)
	if err != nil {
		return Board{}, err
	}
	id, _ := res.LastInsertId()
	return s.getBoard(id)
}

func (s *Store) UpdateBoard(id int64, in BoardInput) (Board, error) {
	if in.Cols < 2 || in.Cols > 10 {
		in.Cols = 2
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE notes_boards SET name=?, x_real=?, y_real=?, cols=?, color=?, title_color=?, updated_at=? WHERE id=?`,
		in.Name, in.X, in.Y, in.Cols, in.Color, in.TitleColor, now, id,
	)
	if err != nil {
		return Board{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Board{}, ErrNotFound
	}
	return s.getBoard(id)
}

// AppendCardToBoard creates a fresh card slotted at the end of the
// given board, bumping the board's cols to match. Returns the new card
// (with its assigned slot_index) and the updated board, or an error if
// the board is already at the 10-card cap.
func (s *Store) AppendCardToBoard(boardID int64) (Card, Board, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Card{}, Board{}, err
	}
	defer tx.Rollback()

	var b Board
	if err := tx.QueryRow(
		`SELECT id, name, x_real, y_real, cols, color, title_color, created_at, updated_at FROM notes_boards WHERE id=?`,
		boardID,
	).Scan(&b.ID, &b.Name, &b.X, &b.Y, &b.Cols, &b.Color, &b.TitleColor, &b.CreatedAt, &b.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Card{}, Board{}, ErrNotFound
		}
		return Card{}, Board{}, err
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM notes_cards WHERE board_id=?`, boardID).Scan(&count); err != nil {
		return Card{}, Board{}, err
	}
	if count >= 10 {
		return Card{}, Board{}, fmt.Errorf("board already has 10 cards")
	}
	newSlot := count
	now := time.Now().Unix()
	res, err := tx.Exec(
		`INSERT INTO notes_cards (name, x_real, y_real, w, h, color, title_color, board_id, slot_index, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		"", 0.0, 0.0, 280, 120, b.Color, b.TitleColor, boardID, newSlot, now, now,
	)
	if err != nil {
		return Card{}, Board{}, err
	}
	cardID, _ := res.LastInsertId()
	newCols := newSlot + 1
	if newCols > b.Cols {
		if _, err := tx.Exec(
			`UPDATE notes_boards SET cols=?, updated_at=? WHERE id=?`,
			newCols, now, boardID,
		); err != nil {
			return Card{}, Board{}, err
		}
		b.Cols = newCols
		b.UpdatedAt = now
	}
	if err := tx.Commit(); err != nil {
		return Card{}, Board{}, err
	}
	card, err := s.getCard(cardID)
	if err != nil {
		return Card{}, Board{}, err
	}
	return card, b, nil
}

func (s *Store) getBoard(id int64) (Board, error) {
	var b Board
	row := s.db.QueryRow(
		`SELECT id, name, x_real, y_real, cols, color, title_color, created_at, updated_at FROM notes_boards WHERE id=?`,
		id,
	)
	if err := row.Scan(&b.ID, &b.Name, &b.X, &b.Y, &b.Cols, &b.Color, &b.TitleColor, &b.CreatedAt, &b.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return b, ErrNotFound
		}
		return b, err
	}
	return b, nil
}

// PatchBoardPosition is the lightweight endpoint used while dragging a
// board around the canvas.
func (s *Store) PatchBoardPosition(id int64, x, y float64) error {
	res, err := s.db.Exec(
		`UPDATE notes_boards SET x_real=?, y_real=?, updated_at=? WHERE id=?`,
		x, y, time.Now().Unix(), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteBoard(id int64) error {
	res, err := s.db.Exec(`DELETE FROM notes_boards WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}


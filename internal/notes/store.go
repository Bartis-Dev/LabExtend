// Package notes owns the Notes module: free-canvas Trello-style cards
// with sub-notes. Cards are positioned in continuous space (x/y floats),
// not a grid; the frontend handles pan/zoom and collision detection.
package notes

import (
	"database/sql"
	"errors"
	"time"
)

type Card struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	W         int     `json:"w"`
	H         int     `json:"h"`
	Color     string  `json:"color"`
	CreatedAt int64   `json:"created_at"`
	UpdatedAt int64   `json:"updated_at"`
	Items     []Item  `json:"items"`
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

type CardInput struct {
	Name  string  `json:"name"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     int     `json:"w"`
	H     int     `json:"h"`
	Color string  `json:"color"`
}

type ItemInput struct {
	Text       string `json:"text"`
	IsFavorite bool   `json:"is_favorite"`
	Position   int    `json:"position"`
}

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

// ListAll returns every card with its items pre-loaded. The frontend
// only renders one Notes page so loading everything in one request is
// cheap and avoids N+1 fetches on the floating favourites card.
func (s *Store) ListAll() ([]Card, error) {
	rows, err := s.db.Query(
		`SELECT id, name, x_real, y_real, w, h, color, created_at, updated_at FROM notes_cards ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	cards := []Card{}
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.ID, &c.Name, &c.X, &c.Y, &c.W, &c.H, &c.Color, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		c.Items = []Item{}
		cards = append(cards, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	itemRows, err := s.db.Query(
		`SELECT id, card_id, text, is_favorite, position, created_at, updated_at FROM notes_items ORDER BY card_id, position, id`,
	)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	byCard := map[int64]*Card{}
	for i := range cards {
		byCard[cards[i].ID] = &cards[i]
	}
	for itemRows.Next() {
		var it Item
		var fav int
		if err := itemRows.Scan(&it.ID, &it.CardID, &it.Text, &fav, &it.Position, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		it.IsFavorite = fav == 1
		if c, ok := byCard[it.CardID]; ok {
			c.Items = append(c.Items, it)
		}
	}
	return cards, itemRows.Err()
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
		`INSERT INTO notes_cards (name, x_real, y_real, w, h, color, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		in.Name, in.X, in.Y, in.W, in.H, in.Color, now, now,
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
		`UPDATE notes_cards SET name=?, x_real=?, y_real=?, w=?, h=?, color=?, updated_at=? WHERE id=?`,
		in.Name, in.X, in.Y, in.W, in.H, in.Color, now, id,
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

// PatchCardLayout updates just the spatial fields (x/y/w/h). Used by the
// frontend during/after dragging so we don't have to send the full row.
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
		`SELECT id, name, x_real, y_real, w, h, color, created_at, updated_at FROM notes_cards WHERE id=?`,
		id,
	)
	if err := row.Scan(&c.ID, &c.Name, &c.X, &c.Y, &c.W, &c.H, &c.Color, &c.CreatedAt, &c.UpdatedAt); err != nil {
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
	// Default position: max+1 within card.
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

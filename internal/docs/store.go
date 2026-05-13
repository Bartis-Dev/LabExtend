// Package docs owns the Docs module: user-authored markdown pages and
// external-link bookmarks, grouped by free-form category strings.
package docs

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Page struct {
	ID              int64   `json:"id"`
	Slug            string  `json:"slug"`
	Title           string  `json:"title"`
	Category        string  `json:"category"`
	ContentMarkdown string  `json:"content_markdown"`
	IsLink          bool    `json:"is_link"`
	LinkURL         *string `json:"link_url"`
	Position        int     `json:"position"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
}

type PageInput struct {
	Slug            string  `json:"slug"`
	Title           string  `json:"title"`
	Category        string  `json:"category"`
	ContentMarkdown string  `json:"content_markdown"`
	IsLink          bool    `json:"is_link"`
	LinkURL         *string `json:"link_url"`
	Position        int     `json:"position"`
}

var ErrNotFound = errors.New("page not found")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

const selectCols = `id, slug, title, category, content_markdown, is_link, link_url, position, created_at, updated_at`

func scan(scanner interface{ Scan(...any) error }) (Page, error) {
	var p Page
	var isLink int
	if err := scanner.Scan(
		&p.ID, &p.Slug, &p.Title, &p.Category, &p.ContentMarkdown,
		&isLink, &p.LinkURL, &p.Position, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return p, err
	}
	p.IsLink = isLink == 1
	return p, nil
}

func (s *Store) List() ([]Page, error) {
	rows, err := s.db.Query(`SELECT ` + selectCols + ` FROM docs_pages ORDER BY category ASC, position ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Page{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Page, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM docs_pages WHERE id=?`, id)
	p, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) Create(in PageInput) (Page, error) {
	now := time.Now().Unix()
	isLink := 0
	if in.IsLink {
		isLink = 1
	}
	// Default position: max+1 within category.
	var maxPos sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(position) FROM docs_pages WHERE category=?`, in.Category).Scan(&maxPos)
	pos := int(maxPos.Int64) + 1
	if in.Position > 0 {
		pos = in.Position
	}
	res, err := s.db.Exec(
		`INSERT INTO docs_pages (slug, title, category, content_markdown, is_link, link_url, position, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		in.Slug, in.Title, in.Category, in.ContentMarkdown, isLink, in.LinkURL, pos, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Page{}, errors.New("slug already in use")
		}
		return Page{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

func (s *Store) Update(id int64, in PageInput) (Page, error) {
	now := time.Now().Unix()
	isLink := 0
	if in.IsLink {
		isLink = 1
	}
	res, err := s.db.Exec(
		`UPDATE docs_pages SET slug=?, title=?, category=?, content_markdown=?, is_link=?, link_url=?, position=?, updated_at=?
		 WHERE id=?`,
		in.Slug, in.Title, in.Category, in.ContentMarkdown, isLink, in.LinkURL, in.Position, now, id,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Page{}, errors.New("slug already in use")
		}
		return Page{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Page{}, ErrNotFound
	}
	return s.Get(id)
}

func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM docs_pages WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Package modules manages the navbar module registry: built-in feature
// pages (dashboard, ddns, …) plus user-created iframe embeds. Built-ins
// are seeded by migration; iframes are created at runtime.
package modules

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Kind string

const (
	KindBuiltin Kind = "builtin"
	KindIframe  Kind = "iframe"
)

type Module struct {
	ID         int64   `json:"id"`
	Slug       string  `json:"slug"`
	Kind       Kind    `json:"kind"`
	Name       string  `json:"name"`
	Icon       string  `json:"icon"`
	URL        *string `json:"url,omitempty"`
	Enabled    bool    `json:"enabled"`
	Position   int     `json:"position"`
	BuiltinKey *string `json:"builtin_key,omitempty"`
}

// ModulePatch carries partial updates. Nil fields are left unchanged.
type ModulePatch struct {
	Name     *string
	Icon     *string
	URL      *string
	Enabled  *bool
	Position *int
}

var ErrNotFound = errors.New("module not found")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

const selectCols = `id, slug, kind, name, icon, url, enabled, position, builtin_key`

func scan(scanner interface{ Scan(...any) error }) (Module, error) {
	var m Module
	var enabled int
	err := scanner.Scan(&m.ID, &m.Slug, &m.Kind, &m.Name, &m.Icon, &m.URL, &enabled, &m.Position, &m.BuiltinKey)
	if err != nil {
		return m, err
	}
	m.Enabled = enabled == 1
	return m, nil
}

func (s *Store) List() ([]Module, error) {
	rows, err := s.db.Query(`SELECT ` + selectCols + ` FROM modules ORDER BY position ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Module{}
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Module, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM modules WHERE id=?`, id)
	m, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) GetBySlug(slug string) (Module, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM modules WHERE slug=?`, slug)
	m, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// Create inserts an iframe module. Built-ins are seeded via migration only.
func (s *Store) Create(m Module) (Module, error) {
	if m.Kind != KindIframe {
		return Module{}, fmt.Errorf("only iframe modules can be created at runtime")
	}
	now := time.Now().Unix()
	// Default position: append after the highest existing one.
	var maxPos sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(position) FROM modules`).Scan(&maxPos)
	pos := int(maxPos.Int64) + 1
	if m.Position > 0 {
		pos = m.Position
	}
	enabled := 0
	if m.Enabled {
		enabled = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO modules (slug, kind, name, icon, url, enabled, position, builtin_key, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,NULL,?,?)`,
		m.Slug, string(m.Kind), m.Name, m.Icon, m.URL, enabled, pos, now, now,
	)
	if err != nil {
		// Surface UNIQUE constraint on slug as a clean error string.
		if strings.Contains(err.Error(), "UNIQUE") {
			return Module{}, fmt.Errorf("slug already in use")
		}
		return Module{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

// Update applies a non-nil patch and bumps updated_at.
func (s *Store) Update(id int64, p ModulePatch) (Module, error) {
	sets := []string{}
	args := []any{}
	if p.Name != nil {
		sets = append(sets, "name=?")
		args = append(args, *p.Name)
	}
	if p.Icon != nil {
		sets = append(sets, "icon=?")
		args = append(args, *p.Icon)
	}
	if p.URL != nil {
		sets = append(sets, "url=?")
		args = append(args, *p.URL)
	}
	if p.Enabled != nil {
		sets = append(sets, "enabled=?")
		if *p.Enabled {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if p.Position != nil {
		sets = append(sets, "position=?")
		args = append(args, *p.Position)
	}
	if len(sets) == 0 {
		return s.Get(id)
	}
	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().Unix())
	args = append(args, id)
	q := `UPDATE modules SET ` + strings.Join(sets, ", ") + ` WHERE id=?`
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return Module{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Module{}, ErrNotFound
	}
	return s.Get(id)
}

// Delete removes a module. Callers must enforce "iframes only" before calling.
func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM modules WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

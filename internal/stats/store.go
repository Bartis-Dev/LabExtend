// Package stats owns the Stats module: push-based time-series metric
// sources, raw point storage, and widgets that read from a source over
// a time range. There's intentionally no aggregation table yet — for a
// homelab single-binary, querying raw rows is plenty fast.
package stats

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

type Source struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Unit      string `json:"unit"`
	Token     string `json:"token"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type Point struct {
	TS    int64   `json:"ts"`
	Value float64 `json:"value"`
}

type Widget struct {
	ID               int64  `json:"id"`
	SourceID         int64  `json:"source_id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	TimeRangeMinutes int    `json:"time_range_minutes"`
	Position         int    `json:"position"`
	ConfigJSON       string `json:"config_json"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

// ---- Sources -------------------------------------------------------------

func (s *Store) ListSources() ([]Source, error) {
	rows, err := s.db.Query(`SELECT id, name, unit, token, created_at, updated_at FROM stats_sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var src Source
		if err := rows.Scan(&src.ID, &src.Name, &src.Unit, &src.Token, &src.CreatedAt, &src.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) GetSource(id int64) (Source, error) {
	var src Source
	row := s.db.QueryRow(`SELECT id, name, unit, token, created_at, updated_at FROM stats_sources WHERE id=?`, id)
	if err := row.Scan(&src.ID, &src.Name, &src.Unit, &src.Token, &src.CreatedAt, &src.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return src, ErrNotFound
		}
		return src, err
	}
	return src, nil
}

func (s *Store) GetSourceByToken(token string) (Source, error) {
	var src Source
	row := s.db.QueryRow(`SELECT id, name, unit, token, created_at, updated_at FROM stats_sources WHERE token=?`, token)
	if err := row.Scan(&src.ID, &src.Name, &src.Unit, &src.Token, &src.CreatedAt, &src.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return src, ErrNotFound
		}
		return src, err
	}
	return src, nil
}

func (s *Store) CreateSource(name, unit string) (Source, error) {
	tok, err := newToken()
	if err != nil {
		return Source{}, err
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO stats_sources (name, unit, token, created_at, updated_at) VALUES (?,?,?,?,?)`,
		name, unit, tok, now, now,
	)
	if err != nil {
		return Source{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetSource(id)
}

func (s *Store) UpdateSource(id int64, name, unit string) (Source, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE stats_sources SET name=?, unit=?, updated_at=? WHERE id=?`,
		name, unit, now, id,
	)
	if err != nil {
		return Source{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Source{}, ErrNotFound
	}
	return s.GetSource(id)
}

func (s *Store) RotateSourceToken(id int64) (Source, error) {
	tok, err := newToken()
	if err != nil {
		return Source{}, err
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(`UPDATE stats_sources SET token=?, updated_at=? WHERE id=?`, tok, now, id)
	if err != nil {
		return Source{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Source{}, ErrNotFound
	}
	return s.GetSource(id)
}

func (s *Store) DeleteSource(id int64) error {
	res, err := s.db.Exec(`DELETE FROM stats_sources WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Points --------------------------------------------------------------

func (s *Store) InsertPoint(sourceID int64, ts int64, value float64) error {
	_, err := s.db.Exec(`INSERT INTO stats_points (source_id, ts, value) VALUES (?,?,?)`, sourceID, ts, value)
	return err
}

// QueryPoints returns points in the [from, to] range (inclusive) sorted
// ascending. If the result would exceed maxPoints, we evenly subsample
// to keep the response (and the chart) responsive.
func (s *Store) QueryPoints(sourceID int64, from, to int64, maxPoints int) ([]Point, error) {
	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM stats_points WHERE source_id=? AND ts>=? AND ts<=?`,
		sourceID, from, to,
	).Scan(&total); err != nil {
		return nil, err
	}
	if total == 0 {
		return []Point{}, nil
	}
	stride := 1
	if maxPoints > 0 && total > maxPoints {
		stride = total / maxPoints
		if stride < 1 {
			stride = 1
		}
	}
	q := `SELECT ts, value FROM stats_points WHERE source_id=? AND ts>=? AND ts<=? ORDER BY ts ASC`
	if stride > 1 {
		q = `
			SELECT ts, value FROM (
				SELECT ts, value, ROW_NUMBER() OVER (ORDER BY ts) AS rn
				FROM stats_points
				WHERE source_id=? AND ts>=? AND ts<=?
			) WHERE rn % ? = 1 ORDER BY ts ASC`
	}
	var rows *sql.Rows
	var err error
	if stride > 1 {
		rows, err = s.db.Query(q, sourceID, from, to, stride)
	} else {
		rows, err = s.db.Query(q, sourceID, from, to)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.TS, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) LatestPoint(sourceID int64) (*Point, error) {
	var p Point
	row := s.db.QueryRow(`SELECT ts, value FROM stats_points WHERE source_id=? ORDER BY ts DESC LIMIT 1`, sourceID)
	if err := row.Scan(&p.TS, &p.Value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// ---- Widgets -------------------------------------------------------------

func (s *Store) ListWidgets() ([]Widget, error) {
	rows, err := s.db.Query(
		`SELECT id, source_id, name, kind, time_range_minutes, position, config_json, created_at, updated_at
		 FROM stats_widgets ORDER BY position, id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Widget{}
	for rows.Next() {
		var wg Widget
		if err := rows.Scan(&wg.ID, &wg.SourceID, &wg.Name, &wg.Kind, &wg.TimeRangeMinutes, &wg.Position, &wg.ConfigJSON, &wg.CreatedAt, &wg.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, wg)
	}
	return out, rows.Err()
}

func (s *Store) GetWidget(id int64) (Widget, error) {
	var wg Widget
	row := s.db.QueryRow(
		`SELECT id, source_id, name, kind, time_range_minutes, position, config_json, created_at, updated_at
		 FROM stats_widgets WHERE id=?`, id,
	)
	if err := row.Scan(&wg.ID, &wg.SourceID, &wg.Name, &wg.Kind, &wg.TimeRangeMinutes, &wg.Position, &wg.ConfigJSON, &wg.CreatedAt, &wg.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return wg, ErrNotFound
		}
		return wg, err
	}
	return wg, nil
}

func (s *Store) CreateWidget(wg Widget) (Widget, error) {
	now := time.Now().Unix()
	if wg.Kind == "" {
		wg.Kind = "line"
	}
	if wg.ConfigJSON == "" {
		wg.ConfigJSON = "{}"
	}
	// Append to end if no explicit position given.
	if wg.Position == 0 {
		var maxPos sql.NullInt64
		_ = s.db.QueryRow(`SELECT MAX(position) FROM stats_widgets`).Scan(&maxPos)
		wg.Position = int(maxPos.Int64) + 1
	}
	res, err := s.db.Exec(
		`INSERT INTO stats_widgets (source_id, name, kind, time_range_minutes, position, config_json, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		wg.SourceID, wg.Name, wg.Kind, wg.TimeRangeMinutes, wg.Position, wg.ConfigJSON, now, now,
	)
	if err != nil {
		return Widget{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetWidget(id)
}

func (s *Store) UpdateWidget(id int64, wg Widget) (Widget, error) {
	now := time.Now().Unix()
	if wg.ConfigJSON == "" {
		wg.ConfigJSON = "{}"
	}
	res, err := s.db.Exec(
		`UPDATE stats_widgets SET source_id=?, name=?, kind=?, time_range_minutes=?, position=?, config_json=?, updated_at=? WHERE id=?`,
		wg.SourceID, wg.Name, wg.Kind, wg.TimeRangeMinutes, wg.Position, wg.ConfigJSON, now, id,
	)
	if err != nil {
		return Widget{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Widget{}, ErrNotFound
	}
	return s.GetWidget(id)
}

func (s *Store) DeleteWidget(id int64) error {
	res, err := s.db.Exec(`DELETE FROM stats_widgets WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- helpers -------------------------------------------------------------

func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

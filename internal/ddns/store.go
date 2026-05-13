// Package ddns owns the DDNS module: persistent storage of providers
// (with server-encrypted API tokens), saved cards (zone pins), and the
// auto-update list driven by the background worker.
package ddns

import (
	"database/sql"
	"errors"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/servercrypto"
)

type Provider struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type Card struct {
	ID         int64    `json:"id"`
	ProviderID int64    `json:"provider_id"`
	RemoteID   string   `json:"remote_id"`
	Name       string   `json:"name"`
	ShowTypes  []string `json:"show_types"`
	Layout     Layout   `json:"layout"`
	CreatedAt  int64    `json:"created_at"`
	UpdatedAt  int64    `json:"updated_at"`
}

type Layout struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type AutoUpdate struct {
	ID             int64   `json:"id"`
	CardID         int64   `json:"card_id"`
	RecordRemoteID string  `json:"record_remote_id"`
	RecordName     string  `json:"record_name"`
	RecordType     string  `json:"record_type"`
	LastSyncedIP   *string `json:"last_synced_ip"`
	LastSyncedAt   *int64  `json:"last_synced_at"`
	LastError      *string `json:"last_error"`
	CreatedAt      int64   `json:"created_at"`
}

var ErrNotFound = errors.New("not found")

type Store struct {
	db     *sql.DB
	cipher *servercrypto.Cipher
}

func New(db *sql.DB, cipher *servercrypto.Cipher) *Store {
	return &Store{db: db, cipher: cipher}
}

// --- Providers ------------------------------------------------------------

func (s *Store) ListProviders() ([]Provider, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, created_at, updated_at FROM ddns_providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Provider{}
	for rows.Next() {
		var p Provider
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProviderToken(id int64) (Provider, string, error) {
	var p Provider
	var ct, nonce []byte
	row := s.db.QueryRow(
		`SELECT id, name, kind, api_token_ciphertext, api_token_nonce, created_at, updated_at FROM ddns_providers WHERE id=?`,
		id,
	)
	if err := row.Scan(&p.ID, &p.Name, &p.Kind, &ct, &nonce, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, "", ErrNotFound
		}
		return p, "", err
	}
	tok, err := s.cipher.Decrypt(ct, nonce)
	if err != nil {
		return p, "", err
	}
	return p, string(tok), nil
}

func (s *Store) CreateProvider(name, kind, token string) (Provider, error) {
	ct, nonce, err := s.cipher.Encrypt([]byte(token))
	if err != nil {
		return Provider{}, err
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO ddns_providers (name, kind, api_token_ciphertext, api_token_nonce, created_at, updated_at)
		 VALUES (?,?,?,?,?,?)`,
		name, kind, ct, nonce, now, now,
	)
	if err != nil {
		return Provider{}, err
	}
	id, _ := res.LastInsertId()
	return Provider{ID: id, Name: name, Kind: kind, CreatedAt: now, UpdatedAt: now}, nil
}

// UpdateProvider renames the provider and optionally rotates the token
// (pass empty token to leave it unchanged).
func (s *Store) UpdateProvider(id int64, name, token string) (Provider, error) {
	now := time.Now().Unix()
	if token == "" {
		res, err := s.db.Exec(`UPDATE ddns_providers SET name=?, updated_at=? WHERE id=?`, name, now, id)
		if err != nil {
			return Provider{}, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return Provider{}, ErrNotFound
		}
	} else {
		ct, nonce, err := s.cipher.Encrypt([]byte(token))
		if err != nil {
			return Provider{}, err
		}
		res, err := s.db.Exec(
			`UPDATE ddns_providers SET name=?, api_token_ciphertext=?, api_token_nonce=?, updated_at=? WHERE id=?`,
			name, ct, nonce, now, id,
		)
		if err != nil {
			return Provider{}, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return Provider{}, ErrNotFound
		}
	}
	var p Provider
	row := s.db.QueryRow(`SELECT id, name, kind, created_at, updated_at FROM ddns_providers WHERE id=?`, id)
	if err := row.Scan(&p.ID, &p.Name, &p.Kind, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Provider{}, err
	}
	return p, nil
}

func (s *Store) DeleteProvider(id int64) error {
	res, err := s.db.Exec(`DELETE FROM ddns_providers WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Cards ---------------------------------------------------------------

func (s *Store) ListCards() ([]Card, error) {
	rows, err := s.db.Query(
		`SELECT id, provider_id, remote_id, name, show_types, layout_x, layout_y, layout_w, layout_h, created_at, updated_at
		 FROM ddns_cards ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetCard(id int64) (Card, error) {
	row := s.db.QueryRow(
		`SELECT id, provider_id, remote_id, name, show_types, layout_x, layout_y, layout_w, layout_h, created_at, updated_at
		 FROM ddns_cards WHERE id=?`, id,
	)
	c, err := scanCard(row)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateCard(c Card) (Card, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO ddns_cards (provider_id, remote_id, name, show_types, layout_x, layout_y, layout_w, layout_h, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ProviderID, c.RemoteID, c.Name, encodeJSONArr(c.ShowTypes),
		c.Layout.X, c.Layout.Y, c.Layout.W, c.Layout.H, now, now,
	)
	if err != nil {
		return Card{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetCard(id)
}

func (s *Store) UpdateCard(id int64, name string, showTypes []string, layout Layout) (Card, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE ddns_cards SET name=?, show_types=?, layout_x=?, layout_y=?, layout_w=?, layout_h=?, updated_at=? WHERE id=?`,
		name, encodeJSONArr(showTypes), layout.X, layout.Y, layout.W, layout.H, now, id,
	)
	if err != nil {
		return Card{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Card{}, ErrNotFound
	}
	return s.GetCard(id)
}

func (s *Store) DeleteCard(id int64) error {
	res, err := s.db.Exec(`DELETE FROM ddns_cards WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Auto-update ---------------------------------------------------------

func (s *Store) ListAutoUpdates() ([]AutoUpdate, error) {
	rows, err := s.db.Query(
		`SELECT id, card_id, record_remote_id, record_name, record_type, last_synced_ip, last_synced_at, last_error, created_at
		 FROM ddns_auto_update ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutoUpdate{}
	for rows.Next() {
		var a AutoUpdate
		if err := rows.Scan(
			&a.ID, &a.CardID, &a.RecordRemoteID, &a.RecordName, &a.RecordType,
			&a.LastSyncedIP, &a.LastSyncedAt, &a.LastError, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) EnableAutoUpdate(cardID int64, recordRemoteID, recordName, recordType string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO ddns_auto_update (card_id, record_remote_id, record_name, record_type, created_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT(card_id, record_remote_id) DO UPDATE SET record_name=excluded.record_name, record_type=excluded.record_type`,
		cardID, recordRemoteID, recordName, recordType, now,
	)
	return err
}

func (s *Store) DisableAutoUpdate(cardID int64, recordRemoteID string) error {
	_, err := s.db.Exec(`DELETE FROM ddns_auto_update WHERE card_id=? AND record_remote_id=?`, cardID, recordRemoteID)
	return err
}

func (s *Store) RecordSyncSuccess(id int64, ip string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`UPDATE ddns_auto_update SET last_synced_ip=?, last_synced_at=?, last_error=NULL WHERE id=?`,
		ip, now, id,
	)
	return err
}

func (s *Store) RecordSyncError(id int64, msg string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`UPDATE ddns_auto_update SET last_error=?, last_synced_at=? WHERE id=?`,
		msg, now, id,
	)
	return err
}

// --- helpers --------------------------------------------------------------

func scanCard(scanner interface{ Scan(...any) error }) (Card, error) {
	var c Card
	var showTypes string
	if err := scanner.Scan(
		&c.ID, &c.ProviderID, &c.RemoteID, &c.Name, &showTypes,
		&c.Layout.X, &c.Layout.Y, &c.Layout.W, &c.Layout.H, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return c, err
	}
	c.ShowTypes = decodeJSONArr(showTypes)
	return c, nil
}

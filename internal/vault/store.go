// Package vault stores the encrypted secrets vault. The backend never
// sees plaintext: clients derive a key from the master password client-
// side (Argon2id) and encrypt entries with AES-GCM before POSTing them.
//
// The backend's only job is to persist opaque blobs and let the client
// prove it has the right key via the verifier ciphertext.
package vault

import (
	"database/sql"
	"errors"
	"time"
)

type State struct {
	Initialized        bool   `json:"initialized"`
	KDFSalt            []byte `json:"kdf_salt,omitempty"`
	KDFParamsJSON      string `json:"kdf_params_json,omitempty"`
	VerifierCiphertext []byte `json:"verifier_ciphertext,omitempty"`
	VerifierNonce      []byte `json:"verifier_nonce,omitempty"`
}

type Entry struct {
	ID                int64  `json:"id"`
	PayloadCiphertext []byte `json:"payload_ciphertext"`
	PayloadNonce      []byte `json:"payload_nonce"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

type SetupInput struct {
	KDFSalt            []byte `json:"kdf_salt"`
	KDFParamsJSON      string `json:"kdf_params_json"`
	VerifierCiphertext []byte `json:"verifier_ciphertext"`
	VerifierNonce      []byte `json:"verifier_nonce"`
}

type EntryInput struct {
	PayloadCiphertext []byte `json:"payload_ciphertext"`
	PayloadNonce      []byte `json:"payload_nonce"`
}

var ErrNotFound = errors.New("entry not found")
var ErrAlreadyInitialized = errors.New("vault already initialized")
var ErrNotInitialized = errors.New("vault not initialized")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

// State returns whether the vault is initialized and, if so, the KDF
// parameters and verifier ciphertext the client needs to attempt unlock.
func (s *Store) State() (State, error) {
	var st State
	var hasSalt sql.NullString // dummy: we only need to know if a row exists
	row := s.db.QueryRow(
		`SELECT kdf_salt, kdf_params_json, verifier_ciphertext, verifier_nonce FROM vault_state WHERE id=1`,
	)
	err := row.Scan(&st.KDFSalt, &st.KDFParamsJSON, &st.VerifierCiphertext, &st.VerifierNonce)
	if errors.Is(err, sql.ErrNoRows) {
		return State{Initialized: false}, nil
	}
	if err != nil {
		return State{}, err
	}
	_ = hasSalt
	st.Initialized = true
	return st, nil
}

func (s *Store) Setup(in SetupInput) error {
	cur, err := s.State()
	if err != nil {
		return err
	}
	if cur.Initialized {
		return ErrAlreadyInitialized
	}
	now := time.Now().Unix()
	_, err = s.db.Exec(
		`INSERT INTO vault_state (id, kdf_salt, kdf_params_json, verifier_ciphertext, verifier_nonce, created_at, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?)`,
		in.KDFSalt, in.KDFParamsJSON, in.VerifierCiphertext, in.VerifierNonce, now, now,
	)
	return err
}

func (s *Store) ListEntries() ([]Entry, error) {
	rows, err := s.db.Query(
		`SELECT id, payload_ciphertext, payload_nonce, created_at, updated_at FROM vault_entries ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.PayloadCiphertext, &e.PayloadNonce, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEntry(id int64) (Entry, error) {
	var e Entry
	row := s.db.QueryRow(
		`SELECT id, payload_ciphertext, payload_nonce, created_at, updated_at FROM vault_entries WHERE id=?`,
		id,
	)
	err := row.Scan(&e.ID, &e.PayloadCiphertext, &e.PayloadNonce, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (s *Store) CreateEntry(in EntryInput) (Entry, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO vault_entries (payload_ciphertext, payload_nonce, created_at, updated_at) VALUES (?,?,?,?)`,
		in.PayloadCiphertext, in.PayloadNonce, now, now,
	)
	if err != nil {
		return Entry{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetEntry(id)
}

func (s *Store) UpdateEntry(id int64, in EntryInput) (Entry, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE vault_entries SET payload_ciphertext=?, payload_nonce=?, updated_at=? WHERE id=?`,
		in.PayloadCiphertext, in.PayloadNonce, now, id,
	)
	if err != nil {
		return Entry{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Entry{}, ErrNotFound
	}
	return s.GetEntry(id)
}

func (s *Store) DeleteEntry(id int64) error {
	res, err := s.db.Exec(`DELETE FROM vault_entries WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

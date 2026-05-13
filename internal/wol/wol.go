// Package wol owns the Wake-on-LAN module: stored targets plus the
// magic-packet sender. A magic packet is 6 bytes of 0xFF followed by
// the 6-byte MAC repeated 16 times; it's sent via UDP to a broadcast
// address (or a directed unicast, but broadcast is the usual case).
package wol

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

type Target struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	MAC           string  `json:"mac"`
	BroadcastAddr string  `json:"broadcast_addr"`
	Port          int     `json:"port"`
	LastSentAt    *int64  `json:"last_sent_at"`
	LastError     *string `json:"last_error"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

type TargetInput struct {
	Name          string `json:"name"`
	MAC           string `json:"mac"`
	BroadcastAddr string `json:"broadcast_addr"`
	Port          int    `json:"port"`
}

var ErrNotFound = errors.New("wol target not found")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

const selectCols = `id, name, mac, broadcast_addr, port, last_sent_at, last_error, created_at, updated_at`

func scan(scanner interface{ Scan(...any) error }) (Target, error) {
	var t Target
	err := scanner.Scan(&t.ID, &t.Name, &t.MAC, &t.BroadcastAddr, &t.Port,
		&t.LastSentAt, &t.LastError, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func (s *Store) List() ([]Target, error) {
	rows, err := s.db.Query(`SELECT ` + selectCols + ` FROM wol_targets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Target{}
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Target, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM wol_targets WHERE id=?`, id)
	t, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (s *Store) Create(in TargetInput) (Target, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO wol_targets (name, mac, broadcast_addr, port, created_at, updated_at)
		 VALUES (?,?,?,?,?,?)`,
		in.Name, in.MAC, in.BroadcastAddr, in.Port, now, now,
	)
	if err != nil {
		return Target{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

func (s *Store) Update(id int64, in TargetInput) (Target, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE wol_targets SET name=?, mac=?, broadcast_addr=?, port=?, updated_at=? WHERE id=?`,
		in.Name, in.MAC, in.BroadcastAddr, in.Port, now, id,
	)
	if err != nil {
		return Target{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Target{}, ErrNotFound
	}
	return s.Get(id)
}

func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM wol_targets WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecordWakeSuccess(id int64) error {
	_, err := s.db.Exec(
		`UPDATE wol_targets SET last_sent_at=?, last_error=NULL WHERE id=?`,
		time.Now().Unix(), id,
	)
	return err
}

func (s *Store) RecordWakeError(id int64, msg string) error {
	_, err := s.db.Exec(
		`UPDATE wol_targets SET last_sent_at=?, last_error=? WHERE id=?`,
		time.Now().Unix(), msg, id,
	)
	return err
}

// ---- Validation + magic packet ------------------------------------------

var macRe = regexp.MustCompile(`^[0-9a-fA-F]{12}$`)

// NormalizeMAC accepts colons or hyphens between bytes and returns a
// canonical lowercase hex form. Returns "" on invalid input.
func NormalizeMAC(s string) string {
	clean := strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(strings.TrimSpace(s))
	if !macRe.MatchString(clean) {
		return ""
	}
	return strings.ToLower(clean)
}

// ValidateInput cleans and validates a TargetInput in place; returns the
// first user-friendly error message or "" on success.
func ValidateInput(in *TargetInput) string {
	in.Name = strings.TrimSpace(in.Name)
	if l := len(in.Name); l < 1 || l > 64 {
		return "name must be 1..64 chars"
	}
	mac := NormalizeMAC(in.MAC)
	if mac == "" {
		return "mac must be 6 hex bytes (e.g. AA:BB:CC:DD:EE:FF)"
	}
	in.MAC = mac
	if in.BroadcastAddr == "" {
		in.BroadcastAddr = "255.255.255.255"
	}
	if ip := net.ParseIP(in.BroadcastAddr); ip == nil {
		return "broadcast_addr must be an IPv4 or IPv6 address"
	}
	if in.Port == 0 {
		in.Port = 9
	}
	if in.Port < 1 || in.Port > 65535 {
		return "port must be 1..65535"
	}
	return ""
}

// BuildMagicPacket assembles the 102-byte WoL magic packet for the given
// canonical-hex MAC. Caller must have validated the MAC first.
func BuildMagicPacket(canonicalMAC string) ([]byte, error) {
	macBytes, err := hex.DecodeString(canonicalMAC)
	if err != nil || len(macBytes) != 6 {
		return nil, fmt.Errorf("invalid mac")
	}
	buf := make([]byte, 6+16*6)
	for i := 0; i < 6; i++ {
		buf[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		copy(buf[6+i*6:], macBytes)
	}
	return buf, nil
}

// Wake builds and sends the magic packet. A short UDP write timeout
// keeps a misconfigured broadcast from hanging the request.
func Wake(target Target) error {
	pkt, err := BuildMagicPacket(target.MAC)
	if err != nil {
		return err
	}
	addr := &net.UDPAddr{IP: net.ParseIP(target.BroadcastAddr), Port: target.Port}
	if addr.IP == nil {
		return fmt.Errorf("invalid broadcast_addr")
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("udp dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(pkt); err != nil {
		return fmt.Errorf("udp write: %w", err)
	}
	return nil
}

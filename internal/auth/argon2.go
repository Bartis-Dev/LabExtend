// Package auth holds password hashing, session management, and TOTP. The
// password hash format is PHC-encoded Argon2id; sessions are opaque random
// tokens stored server-side; TOTP secrets are AES-GCM encrypted at rest.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2idParams are the cost knobs we use for every password hash.
// Conservative defaults from OWASP / argon2 reference: time=2, memory=64MiB,
// parallelism=1. Tune if servers are CPU-starved.
type Argon2idParams struct {
	Time    uint32
	Memory  uint32
	Threads uint8
	KeyLen  uint32
	SaltLen uint32
}

// DefaultArgon2idParams returns sensible defaults.
func DefaultArgon2idParams() Argon2idParams {
	return Argon2idParams{
		Time:    2,
		Memory:  64 * 1024, // KiB
		Threads: 1,
		KeyLen:  32,
		SaltLen: 16,
	}
}

// HashPassword returns a PHC-encoded string suitable for storing in
// users.password_hash.
func HashPassword(password string) (string, error) {
	return HashPasswordWithParams(password, DefaultArgon2idParams())
}

// HashPasswordWithParams is the explicit-params variant of HashPassword.
func HashPasswordWithParams(password string, p Argon2idParams) (string, error) {
	if password == "" {
		return "", errors.New("empty password")
	}
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt rand: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)

	// PHC format: $argon2id$v=19$m=<memory>,t=<time>,p=<threads>$<salt>$<hash>
	enc := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return enc, nil
}

// VerifyPassword returns true if password matches the encoded PHC hash.
// Constant-time comparison; safe to call on attacker input.
//
// TODO(phase 4): also implement upgradePolicy(): if the stored params are
// weaker than current defaults, re-hash transparently after a successful
// verify.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// expected: ["", "argon2id", "v=19", "m=...,t=...,p=...", "<salt>", "<hash>"]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("not an argon2id hash")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	if version != argon2.Version {
		return false, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("salt b64: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("hash b64: %w", err)
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(want, got) == 1, nil
}

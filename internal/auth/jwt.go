package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the LabExtend session token payload. Fields are kept short to
// minimise cookie size.
type Claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"u"`
	jwt.RegisteredClaims
}

// Issue returns an HS256-signed JWT valid for ttl.
func Issue(secret []byte, userID int64, username string, ttl time.Duration) (string, error) {
	now := time.Now()
	c := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "labextend",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(secret)
}

// Verify parses and validates a token issued by Issue, rejecting any
// non-HMAC signing methods (alg substitution defence).
func Verify(secret []byte, tokenStr string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

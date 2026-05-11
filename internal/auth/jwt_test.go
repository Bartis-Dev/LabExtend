package auth

import (
	"testing"
	"time"
)

var testSecret = []byte("test-secret-test-secret-test-secret-32b")

func TestJWTRoundtrip(t *testing.T) {
	tok, err := Issue(testSecret, 42, "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Verify(testSecret, tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != 42 || c.Username != "alice" {
		t.Errorf("claims mismatch: %+v", c)
	}
}

func TestJWTRejectsWrongSecret(t *testing.T) {
	tok, _ := Issue(testSecret, 1, "alice", time.Hour)
	if _, err := Verify([]byte("different-secret-different-secret"), tok); err == nil {
		t.Error("expected verify failure with wrong secret")
	}
}

func TestJWTRejectsExpired(t *testing.T) {
	tok, _ := Issue(testSecret, 1, "alice", -time.Second)
	if _, err := Verify(testSecret, tok); err == nil {
		t.Error("expected verify failure for expired token")
	}
}

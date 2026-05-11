package auth

import (
	"testing"
	"time"
)

func TestLimiterAllowsUpToMax(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Errorf("attempt %d should be allowed", i)
		}
	}
	if l.Allow("k") {
		t.Error("4th attempt should be blocked")
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	if !l.Allow("a") {
		t.Error("a should pass")
	}
	if !l.Allow("b") {
		t.Error("b should pass independently of a")
	}
	if l.Allow("a") {
		t.Error("a should now be blocked")
	}
}

func TestLimiterWindowExpiry(t *testing.T) {
	l := NewLimiter(1, 5*time.Millisecond)
	if !l.Allow("k") {
		t.Error("first attempt should pass")
	}
	if l.Allow("k") {
		t.Error("immediate retry should be blocked")
	}
	time.Sleep(10 * time.Millisecond)
	if !l.Allow("k") {
		t.Error("attempt after window should pass")
	}
}

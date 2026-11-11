package leader

import (
	"testing"
	"time"
)

func TestAttemptLimiter(t *testing.T) {
	l := newAttemptLimiter(3, time.Minute)

	for i := 1; i <= 3; i++ {
		if l.blocked("a") {
			t.Fatalf("blocked before failure %d", i)
		}
		l.fail("a")
	}
	if !l.blocked("a") {
		t.Error("three failures must block the key")
	}
	// Other keys are independent — one attacker must not lock out everyone.
	if l.blocked("b") {
		t.Error("a different key must not be affected")
	}
	// A success clears the counter so earlier typos never lock a real user out.
	l.reset("a")
	if l.blocked("a") {
		t.Error("reset must clear the counter")
	}
}

func TestAttemptLimiterOnlyCountsFailures(t *testing.T) {
	l := newAttemptLimiter(3, time.Minute)
	for i := 0; i < 50; i++ {
		if l.blocked("office-nat") {
			t.Fatal("checking must never count as an attempt")
		}
	}
}

func TestAttemptLimiterWindowExpiry(t *testing.T) {
	l := newAttemptLimiter(1, 20*time.Millisecond)
	l.fail("k")
	if !l.blocked("k") {
		t.Fatal("the failure inside the window should block")
	}
	time.Sleep(30 * time.Millisecond)
	if l.blocked("k") {
		t.Error("a new window must start once the old one expired")
	}
}

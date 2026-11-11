package leader

import (
	"testing"
	"time"
)

func TestAttemptLimiter(t *testing.T) {
	l := newAttemptLimiter(3, time.Minute)

	for i := 1; i <= 3; i++ {
		if !l.allow("a") {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}
	if l.allow("a") {
		t.Error("the 4th attempt must be refused")
	}
	// Other keys are independent — one attacker must not lock out everyone.
	if !l.allow("b") {
		t.Error("a different key must not be affected")
	}
	// A success clears the counter so earlier typos never lock a real user out.
	l.reset("a")
	if !l.allow("a") {
		t.Error("reset must clear the counter")
	}
}

func TestAttemptLimiterWindowExpiry(t *testing.T) {
	l := newAttemptLimiter(1, 20*time.Millisecond)
	if !l.allow("k") {
		t.Fatal("first attempt should be allowed")
	}
	if l.allow("k") {
		t.Fatal("second attempt inside the window should be refused")
	}
	time.Sleep(30 * time.Millisecond)
	if !l.allow("k") {
		t.Error("a new window must start once the old one expired")
	}
}

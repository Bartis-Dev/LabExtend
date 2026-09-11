package leader

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func usersDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY, is_admin INTEGER NOT NULL, is_active INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func addUser(t *testing.T, db *sql.DB, id int, admin, active bool) {
	t.Helper()
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if _, err := db.Exec(`INSERT INTO users (id, is_admin, is_active) VALUES (?,?,?)`,
		id, b(admin), b(active)); err != nil {
		t.Fatal(err)
	}
}

// The guard decides whether an account may be demoted, deactivated or deleted.
// Getting it wrong in the permissive direction locks every admin out of the
// instance for good — the setup wizard is closed and requireAdmin re-reads the
// flag per request, so recovery means editing the SQLite file by hand.
func TestIsLastActiveAdmin(t *testing.T) {
	tests := []struct {
		name   string
		users  [][3]any // id, admin, active
		target string
		want   bool
	}{
		{
			name:   "sole admin is the last one",
			users:  [][3]any{{1, true, true}},
			target: "1",
			want:   true,
		},
		{
			name:   "two active admins: neither is the last",
			users:  [][3]any{{1, true, true}, {2, true, true}},
			target: "1",
			want:   false,
		},
		{
			name:   "non-admin is never the last admin",
			users:  [][3]any{{1, true, true}, {2, false, true}},
			target: "2",
			want:   false,
		},
		{
			// The old count-and-compare form refused this: it counted only
			// active admins (1) and read the target's is_admin (1) without ever
			// looking at its is_active, so removing an already-disabled admin
			// was rejected as "the last admin".
			name:   "deactivated admin may go while an active one remains",
			users:  [][3]any{{1, true, true}, {2, true, false}},
			target: "2",
			want:   false,
		},
		{
			name:   "sole admin who is deactivated is not blocking anything",
			users:  [][3]any{{1, true, false}},
			target: "1",
			want:   false,
		},
		{
			name:   "unknown id",
			users:  [][3]any{{1, true, true}},
			target: "99",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := usersDB(t)
			for _, u := range tc.users {
				addUser(t, db, u[0].(int), u[1].(bool), u[2].(bool))
			}
			got, err := isLastActiveAdmin(context.Background(), db, tc.target)
			if err != nil {
				t.Fatalf("isLastActiveAdmin: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

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

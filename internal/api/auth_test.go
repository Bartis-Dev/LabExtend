package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/db"
	"github.com/Bartis-Dev/LabExtend/internal/ddns"
	"github.com/Bartis-Dev/LabExtend/internal/docs"
	"github.com/Bartis-Dev/LabExtend/internal/modules"
	"github.com/Bartis-Dev/LabExtend/internal/notes"
	"github.com/Bartis-Dev/LabExtend/internal/servercrypto"
	"github.com/Bartis-Dev/LabExtend/internal/stats"
	"github.com/Bartis-Dev/LabExtend/internal/tlsstore"
	"github.com/Bartis-Dev/LabExtend/internal/settings"
	"github.com/Bartis-Dev/LabExtend/internal/vault"
	"github.com/Bartis-Dev/LabExtend/internal/wol"
)

// newTestServer spins up an httptest.Server backed by a fresh migrated DB
// and a client whose cookie jar will carry the session cookie through
// subsequent requests.
func newTestServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	st := settings.New(d)
	secret, err := st.GetOrCreateJWTSecret()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Listen:         "127.0.0.1:0",
		DataDir:        dir,
		SessionTimeout: 1_000_000_000_000, // 1000s
	}
	mods := modules.New(d)
	vlt := vault.New(d)
	cipher, err := servercrypto.New([]byte(secret), "test")
	if err != nil {
		t.Fatal(err)
	}
	dd := ddns.New(d, cipher)
	wl := wol.New(d)
	dx := docs.New(d)
	nt := notes.New(d)
	sts := stats.New(d)
	tlsS := tlsstore.New(dir, "", "")
	srv := New(d, cfg, st, mods, vlt, dd, wl, dx, nt, sts, tlsS, []byte(secret))
	handler := srv.Routes(http.NotFoundHandler())

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	return ts, client
}

func postJSON(t *testing.T, c *http.Client, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := c.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestSetupLoginMeFlow(t *testing.T) {
	ts, c := newTestServer(t)

	// Bootstrap before setup.
	resp, err := c.Get(ts.URL + "/api/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var boot struct{ NeedsSetup bool `json:"needs_setup"` }
	_ = json.NewDecoder(resp.Body).Decode(&boot)
	resp.Body.Close()
	if !boot.NeedsSetup {
		t.Error("needs_setup should be true on fresh DB")
	}

	// Run setup.
	resp = postJSON(t, c, ts.URL+"/api/setup", map[string]string{
		"username": "alice", "password": "hunter22", "password_confirm": "hunter22",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("setup status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Bootstrap after setup.
	resp, _ = c.Get(ts.URL + "/api/bootstrap")
	_ = json.NewDecoder(resp.Body).Decode(&boot)
	resp.Body.Close()
	if boot.NeedsSetup {
		t.Error("needs_setup should be false after setup")
	}

	// /auth/me works now that we have the cookie.
	resp, err = c.Get(ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("me status %d", resp.StatusCode)
	}
	var me struct{ Username string }
	_ = json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	if me.Username != "alice" {
		t.Errorf("me.username = %q", me.Username)
	}

	// Logout invalidates the cookie locally; /auth/me should 401 after.
	resp = postJSON(t, c, ts.URL+"/api/auth/logout", nil)
	if resp.StatusCode != 204 {
		t.Errorf("logout status %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp, _ = c.Get(ts.URL + "/api/auth/me")
	if resp.StatusCode != 401 {
		t.Errorf("post-logout me status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSetupRejectsSecondCall(t *testing.T) {
	ts, c := newTestServer(t)
	resp := postJSON(t, c, ts.URL+"/api/setup", map[string]string{
		"username": "a", "password": "12345678", "password_confirm": "12345678",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("first setup status %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = postJSON(t, c, ts.URL+"/api/setup", map[string]string{
		"username": "b", "password": "12345678", "password_confirm": "12345678",
	})
	if resp.StatusCode != 409 {
		t.Errorf("second setup status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSetupValidates(t *testing.T) {
	ts, c := newTestServer(t)
	cases := []map[string]string{
		{"username": "", "password": "longenough", "password_confirm": "longenough"},
		{"username": "a", "password": "short", "password_confirm": "short"},
		{"username": "a", "password": "longenough", "password_confirm": "different1"},
	}
	for i, body := range cases {
		resp := postJSON(t, c, ts.URL+"/api/setup", body)
		if resp.StatusCode != 400 {
			t.Errorf("case %d: status %d, want 400", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestLoginWrongPassword(t *testing.T) {
	ts, c := newTestServer(t)
	postJSON(t, c, ts.URL+"/api/setup", map[string]string{
		"username": "alice", "password": "hunter22", "password_confirm": "hunter22",
	}).Body.Close()

	resp := postJSON(t, c, ts.URL+"/api/auth/login", map[string]string{
		"username": "alice", "password": "wrong",
	})
	if resp.StatusCode != 401 {
		t.Errorf("login wrong pw status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

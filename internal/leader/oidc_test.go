package leader

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/db"
)

// fakeIDP is a minimal OIDC provider: discovery, JWKS and a token endpoint
// that checks client auth and the PKCE verifier and returns a signed ID token.
type fakeIDP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu        sync.Mutex
	subject   string
	nonce     string // nonce to put into the next ID token
	challenge string // expected S256 code_challenge
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIDP{key: key, subject: "sub-alice"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		if id != "labextend" || secret != "s3cret" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
			return
		}
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		f.mu.Lock()
		challenge, nonce, sub := f.challenge, f.nonce, f.subject
		f.mu.Unlock()
		if b64(sum[:]) != challenge {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600,
			"id_token": f.sign(t, map[string]any{
				"iss": f.srv.URL, "sub": sub, "aud": "labextend", "nonce": nonce,
				"iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
			}),
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIDP) sign(t *testing.T, claims map[string]any) string {
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	body, _ := json.Marshal(claims)
	input := b64(hdr) + "." + b64(body)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

type oidcEnv struct {
	idp   *fakeIDP
	db    *sql.DB
	deps  *OIDCDeps
	store *auth.SessionStore
}

func newOIDCEnv(t *testing.T) *oidcEnv {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	idp := newFakeIDP(t)
	store := auth.NewSessionStore(d, "bpm_session", time.Hour, false)
	authDeps := &AuthDeps{DB: d, Sessions: store}
	return &oidcEnv{
		idp: idp, db: d, store: store,
		deps: &OIDCDeps{
			Auth: authDeps, Account: &AccountDeps{AuthDeps: authDeps},
			Audit:   newAuditLogger(d, false),
			Enabled: true, Name: "Pocket ID", Issuer: idp.srv.URL,
			ClientID: "labextend", ClientSecret: "s3cret",
			RedirectURL: "http://labextend.test/api/auth/oidc/callback",
		},
	}
}

func (e *oidcEnv) addUser(t *testing.T, email, password string, active bool, subject string) int64 {
	t.Helper()
	hash := "x"
	if password != "" {
		var err error
		if hash, err = auth.HashPassword(password); err != nil {
			t.Fatal(err)
		}
	}
	var iss, sub any
	if subject != "" {
		iss, sub = e.idp.srv.URL, subject
	}
	act := 0
	if active {
		act = 1
	}
	res, err := e.db.Exec(`INSERT INTO users (email, password_hash, is_active, created_at, updated_at, oidc_issuer, oidc_subject)
		VALUES (?, ?, ?, 0, 0, ?, ?)`, email, hash, act, iss, sub)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// started is the browser's view after the redirect to the provider.
type started struct {
	state  string
	cookie *http.Cookie
}

// arm tells the fake IdP which challenge to expect and which nonce to sign,
// taken from the authorize URL the leader produced.
func (e *oidcEnv) arm(t *testing.T, authURL string, nonceOverride string) started {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("authorize URL lacks S256 PKCE: %s", authURL)
	}
	if q.Get("nonce") == "" || q.Get("state") == "" {
		t.Fatalf("authorize URL lacks state/nonce: %s", authURL)
	}
	nonce := q.Get("nonce")
	if nonceOverride != "" {
		nonce = nonceOverride
	}
	e.idp.mu.Lock()
	e.idp.challenge, e.idp.nonce = q.Get("code_challenge"), nonce
	e.idp.mu.Unlock()
	return started{state: q.Get("state")}
}

func (e *oidcEnv) startLogin(t *testing.T, nonceOverride string) started {
	t.Helper()
	rec := httptest.NewRecorder()
	e.deps.Login(rec, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login start: %d %s", rec.Code, rec.Body)
	}
	s := e.arm(t, rec.Header().Get("Location"), nonceOverride)
	s.cookie = findCookie(rec, oidcStateCookie)
	if s.cookie == nil || !s.cookie.HttpOnly || s.cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("state cookie missing or not HttpOnly+Lax: %+v", s.cookie)
	}
	return s
}

func (e *oidcEnv) callback(t *testing.T, state string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=c0de&state="+url.QueryEscape(state), nil)
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	sessionMiddleware(e.store)(http.HandlerFunc(e.deps.Callback)).ServeHTTP(rec, req)
	return rec
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name && c.MaxAge >= 0 {
			return c
		}
	}
	return nil
}

func sessionCount(t *testing.T, d *sql.DB) int {
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func wantRejected(t *testing.T, rec *httptest.ResponseRecorder, page string) {
	t.Helper()
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.HasPrefix(loc, page+"?oidc_error=") {
		t.Fatalf("want redirect to %s with error, got %d %q", page, rec.Code, loc)
	}
	if findCookie(rec, "bpm_session") != nil {
		t.Fatal("a session cookie was issued on a rejected callback")
	}
}

func TestOIDCLoginLinkedUser(t *testing.T) {
	e := newOIDCEnv(t)
	id := e.addUser(t, "alice@example.com", "", true, "sub-alice")
	// TOTP on the account must not turn the OIDC session into a pending one.
	if _, err := e.db.Exec(`INSERT INTO totp_secrets (user_id, secret_enc, enabled) VALUES (?, X'00', 1)`, id); err != nil {
		t.Fatal(err)
	}

	s := e.startLogin(t, "")
	rec := e.callback(t, s.state, s.cookie)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/nodes" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	c := findCookie(rec, "bpm_session")
	if c == nil {
		t.Fatal("no session cookie")
	}
	var uid int64
	var pending int
	if err := e.db.QueryRow(`SELECT user_id, is_2fa_pending FROM sessions WHERE id = ?`, c.Value).Scan(&uid, &pending); err != nil {
		t.Fatal(err)
	}
	if uid != id || pending != 0 {
		t.Fatalf("session user=%d pending=%d, want user=%d pending=0", uid, pending, id)
	}
	var action string
	_ = e.db.QueryRow(`SELECT action FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&action)
	if action != "auth.oidc.login" {
		t.Fatalf("last audit action = %q", action)
	}
}

func TestOIDCUnknownSubjectRejected(t *testing.T) {
	e := newOIDCEnv(t)
	e.addUser(t, "alice@example.com", "", true, "someone-else")
	s := e.startLogin(t, "")
	wantRejected(t, e.callback(t, s.state, s.cookie), "/login")
	if sessionCount(t, e.db) != 0 {
		t.Fatal("session created for unknown subject")
	}
}

func TestOIDCBadStateRejected(t *testing.T) {
	e := newOIDCEnv(t)
	e.addUser(t, "alice@example.com", "", true, "sub-alice")
	s := e.startLogin(t, "")

	// Valid state but no cookie: a callback link handed to another browser.
	wantRejected(t, e.callback(t, s.state), "/login")
	// Cookie and query disagree.
	wantRejected(t, e.callback(t, "forged", s.cookie), "/login")
	// Cookie and query agree but the server never issued that state.
	wantRejected(t, e.callback(t, "forged", &http.Cookie{Name: oidcStateCookie, Value: "forged"}), "/login")
	if sessionCount(t, e.db) != 0 {
		t.Fatal("session created on bad state")
	}
}

func TestOIDCReplayedStateRejected(t *testing.T) {
	e := newOIDCEnv(t)
	e.addUser(t, "alice@example.com", "", true, "sub-alice")
	s := e.startLogin(t, "")
	if rec := e.callback(t, s.state, s.cookie); rec.Header().Get("Location") != "/nodes" {
		t.Fatalf("first callback failed: %q", rec.Header().Get("Location"))
	}
	wantRejected(t, e.callback(t, s.state, s.cookie), "/login")
	if sessionCount(t, e.db) != 1 {
		t.Fatalf("sessions = %d, want 1", sessionCount(t, e.db))
	}
}

func TestOIDCNonceMismatchRejected(t *testing.T) {
	e := newOIDCEnv(t)
	e.addUser(t, "alice@example.com", "", true, "sub-alice")
	s := e.startLogin(t, "not-the-nonce")
	wantRejected(t, e.callback(t, s.state, s.cookie), "/login")
	if sessionCount(t, e.db) != 0 {
		t.Fatal("session created despite nonce mismatch")
	}
}

func TestOIDCDisabledUserRejected(t *testing.T) {
	e := newOIDCEnv(t)
	e.addUser(t, "alice@example.com", "", false, "sub-alice")
	s := e.startLogin(t, "")
	rec := e.callback(t, s.state, s.cookie)
	wantRejected(t, rec, "/login")
	if sessionCount(t, e.db) != 0 {
		t.Fatal("session created for disabled user")
	}

	// Indistinguishable from an unknown subject, like the password login.
	e2 := newOIDCEnv(t)
	s2 := e2.startLogin(t, "")
	unknown := e2.callback(t, s2.state, s2.cookie)
	if rec.Header().Get("Location") != unknown.Header().Get("Location") {
		t.Fatalf("disabled and unknown answer differently:\n%s\n%s",
			rec.Header().Get("Location"), unknown.Header().Get("Location"))
	}
}

func TestOIDCLinkStoresSubject(t *testing.T) {
	e := newOIDCEnv(t)
	id := e.addUser(t, "alice@example.com", "correct horse battery", true, "")

	// Signed-in browser.
	login := httptest.NewRecorder()
	sess, err := e.store.Create(context.Background(), login, httptest.NewRequest(http.MethodPost, "/", nil), id, false)
	if err != nil {
		t.Fatal(err)
	}
	sessCookie := findCookie(login, "bpm_session")

	link := func(pw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/account/oidc/link", strings.NewReader(`{"password":"`+pw+`"}`))
		req = req.WithContext(context.WithValue(req.Context(), authCtxKey{}, sess))
		rec := httptest.NewRecorder()
		e.deps.Link(rec, req)
		return rec
	}
	if rec := link("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("link with wrong password: %d", rec.Code)
	}
	rec := link("correct horse battery")
	if rec.Code != http.StatusOK {
		t.Fatalf("link start: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		RedirectURL string `json:"redirect_url"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	s := e.arm(t, body.RedirectURL, "")
	stateCookie := findCookie(rec, oidcStateCookie)

	// Finishing the link in a browser that is not signed in as that user fails.
	wantRejected(t, e.callback(t, s.state, stateCookie), "/account")

	rec = link("correct horse battery")
	s = e.arm(t, func() string { _ = json.Unmarshal(rec.Body.Bytes(), &body); return body.RedirectURL }(), "")
	cb := e.callback(t, s.state, findCookie(rec, oidcStateCookie), sessCookie)
	if cb.Code != http.StatusFound || cb.Header().Get("Location") != "/account?oidc=linked" {
		t.Fatalf("link callback: %d %q", cb.Code, cb.Header().Get("Location"))
	}
	var iss, sub string
	if err := e.db.QueryRow(`SELECT oidc_issuer, oidc_subject FROM users WHERE id = ?`, id).Scan(&iss, &sub); err != nil {
		t.Fatal(err)
	}
	if iss != e.idp.srv.URL || sub != "sub-alice" {
		t.Fatalf("stored (%q, %q)", iss, sub)
	}

	// And the linked subject now signs in.
	ls := e.startLogin(t, "")
	if loc := e.callback(t, ls.state, ls.cookie).Header().Get("Location"); loc != "/nodes" {
		t.Fatalf("login after link: %q", loc)
	}
}

func TestPasswordLoginForbiddenWhenDisabled(t *testing.T) {
	e := newOIDCEnv(t)
	e.addUser(t, "alice@example.com", "correct horse battery", true, "")
	e.deps.Auth.PasswordLoginDisabled = true
	e.deps.Auth.SSOName = "Pocket ID"

	rec := httptest.NewRecorder()
	e.deps.Auth.Login(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"identifier":"alice@example.com","password":"correct horse battery"}`)))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Pocket ID") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if sessionCount(t, e.db) != 0 {
		t.Fatal("session created although password login is disabled")
	}
}

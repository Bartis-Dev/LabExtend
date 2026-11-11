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
	claims    map[string]any // extra ID token claims (email, groups, ...)
	nonce     string         // nonce to put into the next ID token
	challenge string         // expected S256 code_challenge
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIDP{key: key, subject: "sub-alice", claims: map[string]any{
		"email": "alice@example.com", "name": "Alice", "preferred_username": "alice",
		"groups": []string{"labextend"},
	}}
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
		claims := map[string]any{}
		for k, v := range f.claims {
			claims[k] = v
		}
		f.mu.Unlock()
		if b64(sum[:]) != challenge {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600,
			"id_token": f.sign(t, func() map[string]any {
				claims["iss"], claims["sub"], claims["aud"], claims["nonce"] = f.srv.URL, sub, "labextend", nonce
				claims["iat"], claims["exp"] = time.Now().Unix(), time.Now().Add(5*time.Minute).Unix()
				return claims
			}()),
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
	audit := newAuditLogger(d)
	authDeps := &AuthDeps{DB: d, Sessions: store, Audit: audit}
	return &oidcEnv{
		idp: idp, db: d, store: store,
		deps: &OIDCDeps{
			Auth: authDeps, Audit: audit,
			Enabled: true, Name: "Pocket ID", Issuer: idp.srv.URL,
			ClientID: "labextend", ClientSecret: "s3cret",
			RedirectURL: "http://labextend.test/api/auth/oidc/callback",
			UserGroup:   "labextend", AdminGroup: "labextend-admins",
		},
	}
}

// setClaims replaces the extra claims of the next ID token.
func (e *oidcEnv) setClaims(c map[string]any) {
	e.idp.mu.Lock()
	e.idp.claims = c
	e.idp.mu.Unlock()
}

func (e *oidcEnv) setGroups(groups ...string) {
	e.idp.mu.Lock()
	e.idp.claims["groups"] = groups
	e.idp.mu.Unlock()
}

// addUser inserts a user, linked to the fake issuer when subject is set.
func (e *oidcEnv) addUser(t *testing.T, email, username string, admin bool, subject string) int64 {
	t.Helper()
	var iss, sub, uname any
	if subject != "" {
		iss, sub = e.idp.srv.URL, subject
	}
	if username != "" {
		uname = username
	}
	res, err := e.db.Exec(`INSERT INTO users (email, username, is_admin, created_at, updated_at, oidc_issuer, oidc_subject)
		VALUES (?, ?, ?, 0, 0, ?, ?)`, email, uname, boolInt(admin), iss, sub)
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

// login runs one full flow and returns the callback response.
func (e *oidcEnv) login(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	s := e.startLogin(t, "")
	return e.callback(t, s.state, s.cookie)
}

func wantSignedIn(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/nodes" {
		t.Fatalf("want sign-in, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	c := findCookie(rec, "bpm_session")
	if c == nil {
		t.Fatal("no session cookie")
	}
	return c
}

type userRow struct {
	ID       int64
	Email    string
	Username string
	Name     string
	Admin    bool
}

func (e *oidcEnv) userBySubject(t *testing.T, sub string) userRow {
	t.Helper()
	var u userRow
	var admin int
	if err := e.db.QueryRow(`SELECT id, email, COALESCE(username,''), display_name, is_admin
		FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`, e.idp.srv.URL, sub).
		Scan(&u.ID, &u.Email, &u.Username, &u.Name, &admin); err != nil {
		t.Fatalf("user %s: %v", sub, err)
	}
	u.Admin = admin == 1
	return u
}

func userCount(t *testing.T, d *sql.DB) int {
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func lastAudit(t *testing.T, d *sql.DB, action string) string {
	t.Helper()
	var details string
	_ = d.QueryRow(`SELECT details_json FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`, action).Scan(&details)
	return details
}

// adminCheck runs a request with the given session cookie through the real
// middleware chain and returns the status requireAdmin answers with.
func (e *oidcEnv) adminCheck(t *testing.T, c *http.Cookie) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	sessionMiddleware(e.store)(requireAuth(requireAdmin(e.db)(ok))).ServeHTTP(rec, req)
	return rec.Code
}

func TestOIDCProvisionsUserInGroup(t *testing.T) {
	e := newOIDCEnv(t)
	wantSignedIn(t, e.login(t))
	u := e.userBySubject(t, "sub-alice")
	if u.Email != "alice@example.com" || u.Username != "alice" || u.Name != "Alice" || u.Admin {
		t.Fatalf("provisioned %+v", u)
	}
	if lastAudit(t, e.db, "auth.oidc.provision") == "" {
		t.Fatal("no auth.oidc.provision audit row")
	}
}

func TestOIDCProvisionsAdmin(t *testing.T) {
	e := newOIDCEnv(t)
	e.setGroups("labextend-admins")
	wantSignedIn(t, e.login(t))
	if !e.userBySubject(t, "sub-alice").Admin {
		t.Fatal("member of the admin group was not made admin")
	}
}

func TestOIDCRejectsWithoutGroup(t *testing.T) {
	e := newOIDCEnv(t)
	e.setGroups("someone-elses-app")
	wantRejected(t, e.login(t), "/login")
	if sessionCount(t, e.db) != 0 || userCount(t, e.db) != 0 {
		t.Fatal("a user outside both groups got a session or a user row")
	}
	if !strings.Contains(lastAudit(t, e.db, "auth.oidc.failed"), "not in group") {
		t.Fatalf("audit reason = %s", lastAudit(t, e.db, "auth.oidc.failed"))
	}

	// No groups claim at all (scope not granted) is the same refusal.
	e.setClaims(map[string]any{"email": "alice@example.com"})
	wantRejected(t, e.login(t), "/login")
	if userCount(t, e.db) != 0 {
		t.Fatal("user created without groups claim")
	}
}

func TestOIDCRemovalFromGroupsRevokesSessions(t *testing.T) {
	e := newOIDCEnv(t)
	c := wantSignedIn(t, e.login(t))
	e.setGroups()
	wantRejected(t, e.login(t), "/login")
	if sessionCount(t, e.db) != 0 {
		t.Fatal("old session survived removal from the groups")
	}
	if code := e.adminCheck(t, c); code != http.StatusUnauthorized {
		t.Fatalf("old cookie still accepted: %d", code)
	}
}

func TestOIDCAdminPromotionAndDemotion(t *testing.T) {
	e := newOIDCEnv(t)
	userCookie := wantSignedIn(t, e.login(t))
	if code := e.adminCheck(t, userCookie); code != http.StatusForbidden {
		t.Fatalf("non-admin passed requireAdmin: %d", code)
	}

	e.setGroups("labextend", "labextend-admins")
	wantSignedIn(t, e.login(t))
	if !e.userBySubject(t, "sub-alice").Admin {
		t.Fatal("not promoted")
	}
	// The role lives in the DB and is read per request, so a session from
	// before the promotion gets it too.
	if code := e.adminCheck(t, userCookie); code != http.StatusOK {
		t.Fatalf("existing session not promoted: %d", code)
	}
	if !strings.Contains(lastAudit(t, e.db, "auth.oidc.role_change"), `"admin":true`) {
		t.Fatal("promotion not audited")
	}

	e.setGroups("labextend")
	wantSignedIn(t, e.login(t))
	if e.userBySubject(t, "sub-alice").Admin {
		t.Fatal("not demoted after leaving the admin group")
	}
	if code := e.adminCheck(t, userCookie); code != http.StatusForbidden {
		t.Fatalf("existing session kept admin after demotion: %d", code)
	}
}

func TestOIDCExistingSubjectMatched(t *testing.T) {
	e := newOIDCEnv(t)
	// The production shape: one admin, already linked, with a username.
	id := e.addUser(t, "old@example.com", "bartis", true, "sub-alice")
	// An unlinked user holding the email the provider now reports must not be
	// matched by that email.
	other := e.addUser(t, "alice@example.com", "", false, "")
	e.setGroups("labextend-admins")

	wantSignedIn(t, e.login(t))
	u := e.userBySubject(t, "sub-alice")
	if u.ID != id || !u.Admin || u.Username != "bartis" || u.Name != "Alice" {
		t.Fatalf("matched %+v, want id %d kept as admin bartis", u, id)
	}
	// alice@example.com is taken by the unlinked row, so the placeholder is used.
	if u.Email != "sub-alice@oidc.invalid" {
		t.Fatalf("email = %q", u.Email)
	}
	if userCount(t, e.db) != 2 {
		t.Fatal("a new user row was created for a known subject")
	}
	var sessUser int64
	_ = e.db.QueryRow(`SELECT user_id FROM sessions`).Scan(&sessUser)
	if sessUser != id {
		t.Fatalf("session user = %d, want %d", sessUser, id)
	}

	// Once the address is free, the next login picks it up.
	if _, err := e.db.Exec(`DELETE FROM users WHERE id = ?`, other); err != nil {
		t.Fatal(err)
	}
	wantSignedIn(t, e.login(t))
	if got := e.userBySubject(t, "sub-alice").Email; got != "alice@example.com" {
		t.Fatalf("email not refreshed: %q", got)
	}
}

func TestOIDCUsernameCollision(t *testing.T) {
	e := newOIDCEnv(t)
	e.addUser(t, "a@x", "alice", false, "sub-other")
	e.addUser(t, "b@x", "alice-2", false, "sub-other2")
	wantSignedIn(t, e.login(t))
	if got := e.userBySubject(t, "sub-alice").Username; got != "alice-3" {
		t.Fatalf("username = %q, want alice-3", got)
	}
}

func TestOIDCBadStateRejected(t *testing.T) {
	e := newOIDCEnv(t)
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
	s := e.startLogin(t, "")
	wantSignedIn(t, e.callback(t, s.state, s.cookie))
	wantRejected(t, e.callback(t, s.state, s.cookie), "/login")
	if sessionCount(t, e.db) != 1 {
		t.Fatalf("sessions = %d, want 1", sessionCount(t, e.db))
	}
}

func TestOIDCNonceMismatchRejected(t *testing.T) {
	e := newOIDCEnv(t)
	s := e.startLogin(t, "not-the-nonce")
	wantRejected(t, e.callback(t, s.state, s.cookie), "/login")
	if sessionCount(t, e.db) != 0 {
		t.Fatal("session created despite nonce mismatch")
	}
}

func TestOIDCPKCEVerifierChecked(t *testing.T) {
	e := newOIDCEnv(t)
	s := e.startLogin(t, "")
	// The IdP now expects a challenge the leader's verifier does not hash to.
	e.idp.mu.Lock()
	e.idp.challenge = b64([]byte("some-other-challenge"))
	e.idp.mu.Unlock()
	wantRejected(t, e.callback(t, s.state, s.cookie), "/login")
	if sessionCount(t, e.db) != 0 {
		t.Fatal("session created despite PKCE mismatch")
	}
}

func TestOIDCNotConfigured(t *testing.T) {
	e := newOIDCEnv(t)
	e.deps.Enabled = false
	rec := httptest.NewRecorder()
	e.deps.Config(rec, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/config", nil))
	if !strings.Contains(rec.Body.String(), "BPM_OIDC_ISSUER") {
		t.Fatalf("config body = %s", rec.Body)
	}
	rec = httptest.NewRecorder()
	e.deps.Login(rec, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil))
	wantRejected(t, rec, "/login")
}

// Production's admin predates usernames; the first OIDC login must fill it.
func TestOIDCFillsMissingUsername(t *testing.T) {
	e := newOIDCEnv(t)
	e.addUser(t, "old@example.com", "", true, "sub-alice")
	e.setGroups("labextend-admins")

	wantSignedIn(t, e.login(t))
	if u := e.userBySubject(t, "sub-alice"); u.Username == "" {
		t.Fatalf("username still empty: %+v", u)
	}
}

package leader

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/config"
)

const (
	oidcStateCookie = "bpm_oidc_state"
	oidcFlowTTL     = 10 * time.Minute
	// Upper bound on in-flight logins, so the public start endpoint cannot be
	// used to grow memory without limit.
	oidcMaxFlows = 10000
	// A failed discovery is retried on use, but not more often than this, so a
	// down issuer is not hammered by every page load.
	oidcRetryAfter = 10 * time.Second
)

// oidcNotConfigured is shown on the login page while OIDC is not set up.
const oidcNotConfigured = "Login is not configured (set BPM_OIDC_ISSUER, BPM_OIDC_CLIENT_ID, BPM_OIDC_CLIENT_SECRET and EXTERNAL_URL on the leader)."

// oidcFlow is the server-side half of one authorization request. It lives in
// memory rather than in a cookie: removing it on first use is what makes the
// state single-use, which a self-contained cookie cannot be. There is exactly
// one leader and sessions are local to it anyway, so memory is the right scope.
// The browser holds only the state value in a cookie, which binds the callback
// to the browser that started the flow (no login CSRF via a crafted link).
type oidcFlow struct {
	verifier string
	nonce    string
	expires  time.Time
}

// OIDCDeps serves "Sign in with Pocket ID" (any OIDC provider, really), the
// only way into LabExtend. Access and role come from provider groups.
type OIDCDeps struct {
	Auth    *AuthDeps
	Audit   *AuditLogger
	Limiter *attemptLimiter

	Enabled      bool
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	SecureCookie bool
	UserGroup    string
	AdminGroup   string

	provMu   sync.Mutex
	provider *oidc.Provider
	provErr  error
	provAt   time.Time

	flowMu sync.Mutex
	flows  map[string]oidcFlow
}

// newOIDCDeps wires OIDC from config. Missing configuration is logged loudly
// but does not stop the leader: agents keep reporting and the login page says
// what is missing.
func newOIDCDeps(ctx context.Context, cfg *config.Config, authDeps *AuthDeps, audit *AuditLogger) *OIDCDeps {
	d := &OIDCDeps{
		Auth: authDeps, Audit: audit,
		Limiter:      newAttemptLimiter(10, 5*time.Minute),
		Name:         cfg.OIDCName,
		Issuer:       cfg.OIDCIssuer,
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  strings.TrimSuffix(cfg.ExternalURL, "/") + "/api/auth/oidc/callback",
		SecureCookie: cfg.SessionSecureCookie,
		UserGroup:    cfg.OIDCUserGroup,
		AdminGroup:   cfg.OIDCAdminGroup,
	}

	switch {
	case !cfg.OIDCEnabled():
		slog.Error("oidc: not configured, nobody can sign in (need BPM_OIDC_ISSUER, BPM_OIDC_CLIENT_ID and BPM_OIDC_CLIENT_SECRET)")
	case cfg.ExternalURL == "":
		slog.Error("oidc: EXTERNAL_URL is not set, so there is no redirect URL; nobody can sign in")
	default:
		d.Enabled = true
	}

	if d.Enabled {
		slog.Info("oidc: enabled", "issuer", d.Issuer, "redirect_url", d.RedirectURL,
			"user_group", d.UserGroup, "admin_group", d.AdminGroup)
		go func() {
			if _, err := d.getProvider(ctx); err != nil {
				slog.Warn("oidc: provider discovery failed, retrying on first use", "err", err)
			}
		}()
	}
	return d
}

// getProvider runs discovery lazily, so an unreachable issuer at boot only
// disables OIDC until it comes back instead of failing startup.
func (d *OIDCDeps) getProvider(ctx context.Context) (*oidc.Provider, error) {
	d.provMu.Lock()
	defer d.provMu.Unlock()
	if d.provider != nil {
		return d.provider, nil
	}
	if d.provErr != nil && time.Since(d.provAt) < oidcRetryAfter {
		return nil, d.provErr
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(cctx, d.Issuer)
	d.provAt = time.Now()
	if err != nil {
		d.provErr = fmt.Errorf("oidc discovery %s: %w", d.Issuer, err)
		return nil, d.provErr
	}
	d.provider, d.provErr = p, nil
	return p, nil
}

func (d *OIDCDeps) oauthConfig(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     d.ClientID,
		ClientSecret: d.ClientSecret,
		RedirectURL:  d.RedirectURL,
		Endpoint:     p.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
	}
}

// begin records a new flow, sets the state cookie and returns the provider URL.
func (d *OIDCDeps) begin(ctx context.Context, w http.ResponseWriter) (string, error) {
	p, err := d.getProvider(ctx)
	if err != nil {
		return "", err
	}
	state, err := auth.NewSessionID()
	if err != nil {
		return "", err
	}
	nonce, err := auth.NewSessionID()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()

	now := time.Now()
	d.flowMu.Lock()
	if d.flows == nil {
		d.flows = make(map[string]oidcFlow)
	}
	for k, f := range d.flows {
		if now.After(f.expires) {
			delete(d.flows, k)
		}
	}
	if len(d.flows) >= oidcMaxFlows {
		d.flowMu.Unlock()
		return "", errors.New("too many sign-ins in progress; try again shortly")
	}
	d.flows[state] = oidcFlow{verifier: verifier, nonce: nonce, expires: now.Add(oidcFlowTTL)}
	d.flowMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     "/api/auth/oidc",
		MaxAge:   int(oidcFlowTTL / time.Second),
		HttpOnly: true,
		Secure:   d.SecureCookie,
		// Lax, not Strict: the callback is a top-level redirect from the
		// provider's origin, which Strict would strip the cookie from.
		SameSite: http.SameSiteLaxMode,
	})
	return d.oauthConfig(p).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// takeFlow removes and returns the flow for state; a second call for the same
// state finds nothing.
func (d *OIDCDeps) takeFlow(state string) (oidcFlow, bool) {
	d.flowMu.Lock()
	defer d.flowMu.Unlock()
	f, ok := d.flows[state]
	delete(d.flows, state)
	if !ok || time.Now().After(f.expires) {
		return oidcFlow{}, false
	}
	return f, true
}

// Config is public: the login page needs it before anyone is signed in.
func (d *OIDCDeps) Config(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{"enabled": d.Enabled, "name": d.Name}
	if !d.Enabled {
		body["error"] = oidcNotConfigured
	}
	writeJSON(w, http.StatusOK, body)
}

// Login starts the flow and redirects to the provider.
func (d *OIDCDeps) Login(w http.ResponseWriter, r *http.Request) {
	if !d.Enabled {
		redirectWithError(w, r, "/login", oidcNotConfigured)
		return
	}
	if d.Limiter != nil && d.Limiter.blocked(clientIPFromRequest(r)) {
		redirectWithError(w, r, "/login", "Too many failed sign-in attempts. Try again in a few minutes.")
		return
	}
	u, err := d.begin(r.Context(), w)
	if err != nil {
		slog.Warn("oidc: login start failed", "err", err)
		redirectWithError(w, r, "/login", d.Name+" is unreachable right now. Try again later.")
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

// oidcIdentity is what a verified ID token says about the user.
type oidcIdentity struct {
	Issuer            string   `json:"-"`
	Subject           string   `json:"-"`
	Email             string   `json:"email"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
}

// Callback finishes the flow. Every failure ends in a redirect back to the
// login page, carrying a readable message.
func (d *OIDCDeps) Callback(w http.ResponseWriter, r *http.Request) {
	// The state cookie has done its job either way; never leave it behind.
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1,
		HttpOnly: true, Secure: d.SecureCookie, SameSite: http.SameSiteLaxMode,
	})

	limitKey := clientIPFromRequest(r)
	fail := func(reason, msg string) {
		if d.Limiter != nil {
			d.Limiter.fail(limitKey)
		}
		slog.Warn("oidc: callback rejected", "reason", reason, "ip", limitKey)
		d.Audit.Log(r.Context(), r, "auth.oidc.failed", "oidc", d.Issuer, map[string]any{"reason": reason})
		redirectWithError(w, r, "/login", msg)
	}

	if !d.Enabled {
		fail("not configured", oidcNotConfigured)
		return
	}
	if d.Limiter != nil && d.Limiter.blocked(limitKey) {
		slog.Warn("oidc: callback rejected", "reason", "rate limited", "ip", limitKey)
		d.Audit.Log(r.Context(), r, "auth.oidc.failed", "oidc", d.Issuer, map[string]any{"reason": "rate limited"})
		redirectWithError(w, r, "/login", "Too many failed sign-in attempts. Try again in a few minutes.")
		return
	}

	q := r.URL.Query()
	state := q.Get("state")
	c, err := r.Cookie(oidcStateCookie)
	if state == "" || err != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		fail("state mismatch", "Sign-in expired or was started in another browser. Please try again.")
		return
	}
	flow, ok := d.takeFlow(state)
	if !ok {
		fail("unknown or reused state", "Sign-in expired or was already used. Please try again.")
		return
	}
	if e := q.Get("error"); e != "" {
		fail("provider error: "+e, d.Name+" did not complete the sign-in ("+e+").")
		return
	}

	id, err := d.exchange(r.Context(), q.Get("code"), flow)
	if err != nil {
		fail(err.Error(), d.Name+" sign-in could not be verified. Please try again.")
		return
	}
	d.finishLogin(w, r, id, limitKey, fail)
}

// exchange redeems the code with the PKCE verifier and validates the ID token
// (signature, issuer, audience, expiry via go-oidc; nonce here).
func (d *OIDCDeps) exchange(ctx context.Context, code string, flow oidcFlow) (oidcIdentity, error) {
	var id oidcIdentity
	if code == "" {
		return id, errors.New("missing code")
	}
	p, err := d.getProvider(ctx)
	if err != nil {
		return id, err
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tok, err := d.oauthConfig(p).Exchange(cctx, code, oauth2.VerifierOption(flow.verifier))
	if err != nil {
		return id, fmt.Errorf("token exchange: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return id, errors.New("no id_token in token response")
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: d.ClientID}).Verify(cctx, raw)
	if err != nil {
		return id, fmt.Errorf("id token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(flow.nonce)) != 1 {
		return id, errors.New("nonce mismatch")
	}
	if idt.Subject == "" {
		return id, errors.New("empty subject")
	}
	// Pocket ID puts the groups claim (group names) into the ID token when the
	// groups scope is requested, so no userinfo round trip is needed.
	if err := idt.Claims(&id); err != nil {
		return id, fmt.Errorf("id token claims: %w", err)
	}
	id.Issuer, id.Subject = idt.Issuer, idt.Subject
	return id, nil
}

func (d *OIDCDeps) finishLogin(w http.ResponseWriter, r *http.Request, id oidcIdentity, limitKey string, fail func(string, string)) {
	ctx := r.Context()
	isAdmin := slices.Contains(id.Groups, d.AdminGroup)
	if !isAdmin && !slices.Contains(id.Groups, d.UserGroup) {
		// A user dropped from both groups also loses the sessions they still
		// hold, so removal in the provider bites on their next attempt rather
		// than at session expiry.
		_, _ = d.Auth.DB.ExecContext(ctx, `
			DELETE FROM sessions WHERE user_id IN
				(SELECT id FROM users WHERE oidc_issuer = ? AND oidc_subject = ?)`, id.Issuer, id.Subject)
		fail(fmt.Sprintf("not in group %q or %q", d.UserGroup, d.AdminGroup),
			fmt.Sprintf("Your %s account is not in the %s group. Ask an administrator for access.", d.Name, d.UserGroup))
		return
	}

	res, err := upsertOIDCUser(ctx, d.Auth.DB, id, isAdmin)
	if err != nil {
		fail("user upsert: "+err.Error(), "Sign-in failed. Please try again.")
		return
	}

	sess, err := d.Auth.Sessions.Create(ctx, w, r, res.userID)
	if err != nil {
		fail("create session: "+err.Error(), "Sign-in failed. Please try again.")
		return
	}
	if d.Limiter != nil {
		d.Limiter.reset(limitKey)
	}
	actx := context.WithValue(ctx, authCtxKey{}, sess)
	switch {
	case res.created:
		d.Audit.Log(actx, r, "auth.oidc.provision", "user", res.email, map[string]any{"admin": isAdmin, "groups": id.Groups})
	case res.wasAdmin != isAdmin:
		d.Audit.Log(actx, r, "auth.oidc.role_change", "user", res.email, map[string]any{"admin": isAdmin, "groups": id.Groups})
	}
	d.Audit.Log(actx, r, "auth.oidc.login", "user", res.email, nil)
	http.Redirect(w, r, "/nodes", http.StatusFound)
}

type upsertResult struct {
	userID   int64
	email    string
	created  bool
	wasAdmin bool
}

// upsertOIDCUser finds the user by (issuer, subject) only, never by email: an
// email claim is whatever the provider says it is, and matching on it would
// hand an existing account to whoever controls that address. The user is
// created on first login; role and profile are refreshed on every other.
func upsertOIDCUser(ctx context.Context, db *sql.DB, id oidcIdentity, isAdmin bool) (upsertResult, error) {
	var res upsertResult
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var admin int
	err = tx.QueryRowContext(ctx,
		`SELECT id, is_admin FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`,
		id.Issuer, id.Subject).Scan(&res.userID, &admin)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res.created = true
	case err != nil:
		return res, fmt.Errorf("lookup: %w", err)
	}
	res.wasAdmin = admin == 1

	if res.email, err = freeEmail(ctx, tx, id, res.userID); err != nil {
		return res, err
	}
	name := strings.TrimSpace(id.Name)
	if name == "" {
		name = strings.TrimSpace(id.PreferredUsername)
	}
	now := time.Now().Unix()

	if res.created {
		username, err := freeUsername(ctx, tx, id)
		if err != nil {
			return res, err
		}
		r, err := tx.ExecContext(ctx, `
			INSERT INTO users (email, username, display_name, is_admin, created_at, updated_at,
			                   last_login_at, oidc_issuer, oidc_subject)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			res.email, username, name, boolInt(isAdmin), now, now, now, id.Issuer, id.Subject)
		if err != nil {
			return res, fmt.Errorf("insert user: %w", err)
		}
		if res.userID, err = r.LastInsertId(); err != nil {
			return res, fmt.Errorf("insert user id: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET email = ?, display_name = ?, is_admin = ?, updated_at = ?, last_login_at = ?
			WHERE id = ?`, res.email, name, boolInt(isAdmin), now, now, res.userID); err != nil {
			return res, fmt.Errorf("update user: %w", err)
		}
		// Accounts from the password era may have no username; fill it once.
		var current sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ?`, res.userID).Scan(&current); err != nil {
			return res, fmt.Errorf("read username: %w", err)
		}
		if strings.TrimSpace(current.String) == "" {
			username, err := freeUsername(ctx, tx, id)
			if err != nil {
				return res, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET username = ? WHERE id = ?`, username, res.userID); err != nil {
				return res, fmt.Errorf("set username: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// freeEmail returns the email claim unless it is empty or already used by
// another user (users.email is UNIQUE); then a stable per-subject placeholder.
func freeEmail(ctx context.Context, tx *sql.Tx, id oidcIdentity, selfID int64) (string, error) {
	if want := strings.TrimSpace(id.Email); want != "" {
		var n int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE email = ? AND id != ?`, want, selfID).Scan(&n); err != nil {
			return "", fmt.Errorf("email check: %w", err)
		}
		if n == 0 {
			return want, nil
		}
	}
	return id.Subject + "@oidc.invalid", nil
}

// freeUsername picks preferred_username (or the email's local part), adding
// -2, -3, ... until it is unused, so a collision resolves the same way each time.
func freeUsername(ctx context.Context, tx *sql.Tx, id oidcIdentity) (string, error) {
	base := strings.TrimSpace(id.PreferredUsername)
	if base == "" {
		base, _, _ = strings.Cut(strings.TrimSpace(id.Email), "@")
	}
	if base == "" {
		base = "user"
	}
	for i := 1; i <= 1000; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s-%d", base, i)
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, cand).Scan(&n); err != nil {
			return "", fmt.Errorf("username check: %w", err)
		}
		if n == 0 {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no free username for %q", base)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func redirectWithError(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?oidc_error="+url.QueryEscape(msg), http.StatusFound)
}

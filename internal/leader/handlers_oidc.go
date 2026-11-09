package leader

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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

const (
	oidcModeLogin = "login"
	oidcModeLink  = "link"
)

// oidcFlow is the server-side half of one authorization request. It lives in
// memory rather than in a cookie: removing it on first use is what makes the
// state single-use, which a self-contained cookie cannot be. There is exactly
// one leader and sessions are local to it anyway, so memory is the right scope.
// The browser holds only the state value in a cookie, which binds the callback
// to the browser that started the flow (no login CSRF via a crafted link).
type oidcFlow struct {
	verifier string
	nonce    string
	mode     string
	userID   int64 // link mode: the account the subject gets attached to
	expires  time.Time
}

// OIDCDeps serves "Sign in with Pocket ID" (any OIDC provider, really).
type OIDCDeps struct {
	Auth    *AuthDeps
	Account *AccountDeps
	Audit   *AuditLogger
	Limiter *attemptLimiter

	Enabled      bool
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	SecureCookie bool

	provMu   sync.Mutex
	provider *oidc.Provider
	provErr  error
	provAt   time.Time

	flowMu sync.Mutex
	flows  map[string]oidcFlow
}

// newOIDCDeps wires OIDC from config and settles the BPM_PASSWORD_LOGIN flag,
// which may only take effect while OIDC is actually available.
func newOIDCDeps(ctx context.Context, cfg *config.Config, authDeps *AuthDeps, account *AccountDeps, audit *AuditLogger) *OIDCDeps {
	d := &OIDCDeps{
		Auth: authDeps, Account: account, Audit: audit,
		Limiter:      newAttemptLimiter(10, 5*time.Minute),
		Name:         cfg.OIDCName,
		Issuer:       cfg.OIDCIssuer,
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  strings.TrimSuffix(cfg.ExternalURL, "/") + "/api/auth/oidc/callback",
		SecureCookie: cfg.SessionSecureCookie,
	}
	authDeps.SSOName = cfg.OIDCName

	switch {
	case !cfg.OIDCEnabled():
		if cfg.OIDCIssuer != "" || cfg.OIDCClientID != "" || cfg.OIDCClientSecret != "" {
			slog.Warn("oidc: partially configured, disabled (need BPM_OIDC_ISSUER, BPM_OIDC_CLIENT_ID and BPM_OIDC_CLIENT_SECRET)")
		}
	case cfg.ExternalURL == "":
		slog.Warn("oidc: EXTERNAL_URL is not set, so there is no redirect URL; oidc disabled")
	default:
		d.Enabled = true
	}

	if !cfg.PasswordLogin {
		if d.Enabled {
			authDeps.PasswordLoginDisabled = true
		} else {
			slog.Warn("BPM_PASSWORD_LOGIN=false ignored: oidc is not enabled, and turning off the password form would lock everyone out")
		}
	}

	if d.Enabled {
		slog.Info("oidc: enabled", "issuer", d.Issuer, "redirect_url", d.RedirectURL,
			"password_login", !authDeps.PasswordLoginDisabled)
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
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

// begin records a new flow, sets the state cookie and returns the provider URL.
func (d *OIDCDeps) begin(ctx context.Context, w http.ResponseWriter, mode string, userID int64) (string, error) {
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
	d.flows[state] = oidcFlow{verifier: verifier, nonce: nonce, mode: mode, userID: userID, expires: now.Add(oidcFlowTTL)}
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
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        d.Enabled,
		"name":           d.Name,
		"password_login": !d.Auth.PasswordLoginDisabled,
	})
}

// Login starts the flow in login mode and redirects to the provider.
func (d *OIDCDeps) Login(w http.ResponseWriter, r *http.Request) {
	if !d.Enabled {
		writeErr(w, http.StatusNotFound, errors.New("oidc login is not configured"))
		return
	}
	if d.Limiter != nil && !d.Limiter.allow(clientIPFromRequest(r)) {
		redirectWithError(w, r, "/login", "Too many sign-in attempts. Try again in a few minutes.")
		return
	}
	u, err := d.begin(r.Context(), w, oidcModeLogin, 0)
	if err != nil {
		slog.Warn("oidc: login start failed", "err", err)
		redirectWithError(w, r, "/login", d.Name+" is unreachable right now. Try again later.")
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

// Status reports whether OIDC is available and linked for the current user.
func (d *OIDCDeps) Status(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	var subject sql.NullString
	_ = d.Auth.DB.QueryRowContext(r.Context(),
		`SELECT oidc_subject FROM users WHERE id = ?`, sess.UserID).Scan(&subject)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": d.Enabled,
		"name":    d.Name,
		"linked":  subject.Valid && subject.String != "",
	})
}

// Link starts the flow in link mode. It re-authenticates with the password:
// attaching an external identity is a persistent backdoor if a stolen session
// alone could do it, and the TOTP endpoints already set this bar.
func (d *OIDCDeps) Link(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	if !d.Enabled {
		writeErr(w, http.StatusNotFound, errors.New("oidc login is not configured"))
		return
	}
	var body passwordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.Account.reauth(r, sess.UserID, body.Password); err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	u, err := d.begin(r.Context(), w, oidcModeLink, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"redirect_url": u})
}

// Unlink removes the stored subject.
func (d *OIDCDeps) Unlink(w http.ResponseWriter, r *http.Request) {
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, nil)
		return
	}
	var body passwordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.Account.reauth(r, sess.UserID, body.Password); err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	// With the password form off, the link is this account's only way in.
	if d.Auth.PasswordLoginDisabled {
		writeErr(w, http.StatusConflict, errors.New("password login is disabled, so disconnecting would lock you out"))
		return
	}
	if _, err := d.Auth.DB.ExecContext(r.Context(),
		`UPDATE users SET oidc_issuer = NULL, oidc_subject = NULL WHERE id = ?`, sess.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "auth.oidc.unlink", "user", emailString("", sess.UserID), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Callback finishes either mode. Every failure ends in a redirect back to the
// page that started it, carrying a readable message.
func (d *OIDCDeps) Callback(w http.ResponseWriter, r *http.Request) {
	// The state cookie has done its job either way; never leave it behind.
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1,
		HttpOnly: true, Secure: d.SecureCookie, SameSite: http.SameSiteLaxMode,
	})

	back := "/login"
	fail := func(reason, msg string) {
		slog.Warn("oidc: callback rejected", "reason", reason, "ip", clientIPFromRequest(r))
		d.Audit.Log(r.Context(), r, "auth.oidc.failed", "oidc", d.Issuer, map[string]any{"reason": reason})
		redirectWithError(w, r, back, msg)
	}

	if !d.Enabled {
		fail("not configured", "Single sign-on is not configured.")
		return
	}
	limitKey := clientIPFromRequest(r)
	if d.Limiter != nil && !d.Limiter.allow(limitKey) {
		fail("rate limited", "Too many sign-in attempts. Try again in a few minutes.")
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
	if flow.mode == oidcModeLink {
		back = "/account"
	}
	if e := q.Get("error"); e != "" {
		fail("provider error: "+e, d.Name+" did not complete the sign-in ("+e+").")
		return
	}

	issuer, subject, err := d.exchange(r.Context(), q.Get("code"), flow)
	if err != nil {
		fail(err.Error(), d.Name+" sign-in could not be verified. Please try again.")
		return
	}

	switch flow.mode {
	case oidcModeLink:
		d.finishLink(w, r, flow, issuer, subject, fail)
	default:
		d.finishLogin(w, r, issuer, subject, limitKey, fail)
	}
}

// exchange redeems the code with the PKCE verifier and validates the ID token
// (signature, issuer, audience, expiry via go-oidc; nonce here).
func (d *OIDCDeps) exchange(ctx context.Context, code string, flow oidcFlow) (issuer, subject string, err error) {
	if code == "" {
		return "", "", errors.New("missing code")
	}
	p, err := d.getProvider(ctx)
	if err != nil {
		return "", "", err
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tok, err := d.oauthConfig(p).Exchange(cctx, code, oauth2.VerifierOption(flow.verifier))
	if err != nil {
		return "", "", fmt.Errorf("token exchange: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return "", "", errors.New("no id_token in token response")
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: d.ClientID}).Verify(cctx, raw)
	if err != nil {
		return "", "", fmt.Errorf("id token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(flow.nonce)) != 1 {
		return "", "", errors.New("nonce mismatch")
	}
	if idt.Subject == "" {
		return "", "", errors.New("empty subject")
	}
	return idt.Issuer, idt.Subject, nil
}

func (d *OIDCDeps) finishLogin(w http.ResponseWriter, r *http.Request, issuer, subject, limitKey string, fail func(string, string)) {
	var (
		id       int64
		email    string
		isActive int
	)
	err := d.Auth.DB.QueryRowContext(r.Context(),
		`SELECT id, email, is_active FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`,
		issuer, subject).Scan(&id, &email, &isActive)
	// Unknown and disabled answer alike, as the password login does; the audit
	// row keeps the real reason.
	const notLinked = "This %s account is not linked to an active LabExtend user. Sign in with your password and connect it under Account."
	if errors.Is(err, sql.ErrNoRows) {
		fail("unknown subject", fmt.Sprintf(notLinked, d.Name))
		return
	}
	if err != nil {
		fail("user lookup: "+err.Error(), "Sign-in failed. Please try again.")
		return
	}
	if isActive != 1 {
		fail("user disabled", fmt.Sprintf(notLinked, d.Name))
		return
	}

	// Same session as a password login. No TOTP step: the provider's passkey
	// already is the second factor.
	sess, err := d.Auth.Sessions.Create(r.Context(), w, r, id, false)
	if err != nil {
		fail("create session: "+err.Error(), "Sign-in failed. Please try again.")
		return
	}
	if d.Limiter != nil {
		d.Limiter.reset(limitKey)
	}
	_, _ = d.Auth.DB.ExecContext(r.Context(),
		`UPDATE users SET last_login_at = ? WHERE id = ?`, time.Now().Unix(), id)
	ctx := context.WithValue(r.Context(), authCtxKey{}, sess)
	d.Audit.Log(ctx, r, "auth.oidc.login", "user", email, nil)
	http.Redirect(w, r, "/nodes", http.StatusFound)
}

func (d *OIDCDeps) finishLink(w http.ResponseWriter, r *http.Request, flow oidcFlow, issuer, subject string, fail func(string, string)) {
	// The flow was started by flow.userID; the browser finishing it has to
	// still be signed in as that same user.
	sess, _ := r.Context().Value(authCtxKey{}).(*auth.Session)
	if sess == nil || sess.Is2FAPending || sess.UserID != flow.userID {
		fail("link session mismatch", "Your session changed during the connect. Please sign in and try again.")
		return
	}
	_, err := d.Auth.DB.ExecContext(r.Context(),
		`UPDATE users SET oidc_issuer = ?, oidc_subject = ? WHERE id = ?`, issuer, subject, flow.userID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail("subject already linked", "This "+d.Name+" account is already connected to another LabExtend user.")
			return
		}
		fail("link: "+err.Error(), "Connecting failed. Please try again.")
		return
	}
	d.Audit.Log(r.Context(), r, "auth.oidc.link", "user", emailString("", flow.userID), nil)
	http.Redirect(w, r, "/account?oidc=linked", http.StatusFound)
}

func redirectWithError(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?oidc_error="+url.QueryEscape(msg), http.StatusFound)
}

package leader

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Bartis-Dev/LabExtend/internal/s3"
)

// S3Deps groups what the S3 handlers need.
type S3Deps struct {
	DB         *sql.DB
	SecretsKey string // 32-byte hex, AES-256-GCM key for credentials at rest
	Audit      *AuditLogger
}

// S3Endpoint is the JSON shape (secrets MASKED on the wire — never raw).
type S3Endpoint struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	Region        string `json:"region"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key,omitempty"` // empty in list views
	PathStyle     bool   `json:"path_style"`
	DefaultBucket string `json:"default_bucket"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// ─── endpoint CRUD ──────────────────────────────────────────────────────────

func (d *S3Deps) List(w http.ResponseWriter, r *http.Request) {
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT id, name, endpoint, region, access_key, path_style,
		       COALESCE(default_bucket,''), created_at, updated_at
		FROM s3_endpoints ORDER BY name
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []S3Endpoint{}
	for rows.Next() {
		var e S3Endpoint
		var ps int
		if err := rows.Scan(&e.ID, &e.Name, &e.Endpoint, &e.Region, &e.AccessKey,
			&ps, &e.DefaultBucket, &e.CreatedAt, &e.UpdatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		e.PathStyle = ps == 1
		// access_key is half-public (like AWS); show it. secret_key never goes on the wire.
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"endpoints": out})
}

type s3Req struct {
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	Region        string `json:"region"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
	PathStyle     bool   `json:"path_style"`
	DefaultBucket string `json:"default_bucket"`
}

func (d *S3Deps) Create(w http.ResponseWriter, r *http.Request) {
	var req s3Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.Endpoint == "" || req.AccessKey == "" || req.SecretKey == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name, endpoint, access_key, secret_key required"))
		return
	}
	if req.Region == "" {
		req.Region = "eu-central"
	}
	encSecret, err := d.encrypt(req.SecretKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	id := uuid.NewString()
	now := time.Now().Unix()
	if _, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO s3_endpoints
			(id, name, endpoint, region, access_key, secret_key, path_style, default_bucket, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, req.Name, req.Endpoint, req.Region, req.AccessKey, encSecret,
		boolI(req.PathStyle), req.DefaultBucket, now, now); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "s3.endpoint.create", "s3_endpoint", id, map[string]any{"name": req.Name})
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (d *S3Deps) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req s3Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// access_key is stored plain — update if provided.
	if req.AccessKey != "" {
		_, _ = d.DB.ExecContext(r.Context(), `UPDATE s3_endpoints SET access_key = ? WHERE id = ?`, req.AccessKey, id)
	}
	if req.SecretKey != "" {
		enc, err := d.encrypt(req.SecretKey)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		_, _ = d.DB.ExecContext(r.Context(), `UPDATE s3_endpoints SET secret_key = ? WHERE id = ?`, enc, id)
	}
	res, err := d.DB.ExecContext(r.Context(), `
		UPDATE s3_endpoints SET name = ?, endpoint = ?, region = ?, path_style = ?,
		                        default_bucket = ?, updated_at = ?
		WHERE id = ?
	`, req.Name, req.Endpoint, req.Region, boolI(req.PathStyle), req.DefaultBucket, time.Now().Unix(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, http.StatusNotFound, errors.New("endpoint not found"))
		return
	}
	d.Audit.Log(r.Context(), r, "s3.endpoint.update", "s3_endpoint", id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (d *S3Deps) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := d.DB.ExecContext(r.Context(), `DELETE FROM s3_endpoints WHERE id = ?`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "s3.endpoint.delete", "s3_endpoint", id, nil)
	n, _ := res.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// Test verifies the endpoint credentials. Tries ListBuckets first; if that
// fails (typical for Hetzner / R2 bucket-scoped credentials with
// AccessDenied) AND a default_bucket is configured on the endpoint, falls
// back to a HeadBucket on the default bucket as a connectivity probe.
//
// Returns one of:
//
//	{"ok": true, "bucket_count": N}                  — ListBuckets worked
//	{"ok": true, "tested_bucket": "<name>"}          — fallback worked
//	502 + {"error": "<aws message>"}                 — both failed
//
// Every failure path is logged with the endpoint URL + region so the
// operator can `docker service logs labextend_labextend-leader` and see
// exactly what AWS / Hetzner / R2 replied.
func (d *S3Deps) Test(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var name, endpoint, region, defaultBucket string
	_ = d.DB.QueryRowContext(r.Context(), `
		SELECT name, endpoint, region, COALESCE(default_bucket,'')
		FROM s3_endpoints WHERE id = ?
	`, id).Scan(&name, &endpoint, &region, &defaultBucket)

	c, err := d.clientFor(r, id)
	if err != nil {
		slog.Warn("s3.test: client build failed",
			"endpoint_id", id, "name", name, "endpoint", endpoint, "region", region, "err", err)
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	buckets, err := c.ListBuckets(r.Context())

	// Happy path: ListBuckets returned a non-empty list. We're done.
	if err == nil && len(buckets) > 0 {
		slog.Info("s3.test: ListBuckets ok",
			"endpoint_id", id, "name", name, "endpoint", endpoint, "bucket_count", len(buckets))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bucket_count": len(buckets)})
		return
	}

	// Either ListBuckets errored (Hetzner: 403 AccessDenied), OR it
	// succeeded but returned an empty list (Cloudflare R2: object-scoped
	// tokens see no buckets even though they can access their own). In
	// both cases, fall back to a HeadBucket on the configured default.
	if err != nil {
		slog.Warn("s3.test: ListBuckets failed — trying default_bucket fallback",
			"endpoint_id", id, "name", name, "endpoint", endpoint, "region", region,
			"default_bucket", defaultBucket, "err", err)
	} else {
		slog.Info("s3.test: ListBuckets returned 0 — probing default_bucket",
			"endpoint_id", id, "name", name, "endpoint", endpoint,
			"default_bucket", defaultBucket)
	}

	if defaultBucket != "" {
		if perr := c.HeadBucket(r.Context(), defaultBucket); perr == nil {
			slog.Info("s3.test: HeadBucket fallback ok",
				"endpoint_id", id, "name", name, "bucket", defaultBucket)
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": true, "tested_bucket": defaultBucket,
			})
			return
		} else {
			slog.Warn("s3.test: HeadBucket fallback also failed",
				"endpoint_id", id, "name", name, "bucket", defaultBucket, "err", perr)
			if err == nil {
				err = fmt.Errorf("ListBuckets returned 0 buckets")
			}
			writeErr(w, http.StatusBadGateway, fmt.Errorf(
				"%v; HeadBucket(%q): %v", err, defaultBucket, perr))
			return
		}
	}

	// No default_bucket configured.
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf(
			"%w — set 'default_bucket' on this endpoint so we can probe it instead", err))
		return
	}
	// ListBuckets succeeded with empty list, no default configured. Tell
	// the user that's not actually verified connectivity.
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "bucket_count": 0,
		"hint": "ListBuckets returned 0 buckets (R2 object-scoped tokens behave this way). Set 'default_bucket' to actually verify a bucket is reachable.",
	})
}

// ─── bucket / object browsing ───────────────────────────────────────────────

// Buckets lists every bucket on the endpoint. For credentials that aren't
// allowed to ListAllMyBuckets (Hetzner & R2 default), we fall back to
// returning the configured `default_bucket` as the only visible bucket so
// the UI can still render a normal dropdown instead of an error.
func (d *S3Deps) Buckets(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var name, endpoint, defaultBucket string
	_ = d.DB.QueryRowContext(r.Context(), `
		SELECT name, endpoint, COALESCE(default_bucket,'')
		FROM s3_endpoints WHERE id = ?
	`, id).Scan(&name, &endpoint, &defaultBucket)

	c, err := d.clientFor(r, id)
	if err != nil {
		slog.Warn("s3.buckets: client build failed",
			"endpoint_id", id, "name", name, "endpoint", endpoint, "err", err)
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	buckets, err := c.ListBuckets(r.Context())

	// If a default_bucket is configured and it's not already in the
	// visible list, splice it in (after HeadBucket-verifying it's
	// reachable). This handles two cases the same way:
	//   - Hetzner: ListBuckets → 403 → buckets is empty, err != nil
	//   - R2 object-scoped: ListBuckets → 200 [] → buckets empty, err nil
	//   - Account-scoped token but user wants to pin one bucket → splice
	if defaultBucket != "" && !containsString(buckets, defaultBucket) {
		if perr := c.HeadBucket(r.Context(), defaultBucket); perr == nil {
			buckets = append(buckets, defaultBucket)
			err = nil // the bucket is reachable, that's all the UI needs
			slog.Info("s3.buckets: spliced default_bucket into list",
				"endpoint_id", id, "name", name, "bucket", defaultBucket)
		} else if err != nil {
			slog.Warn("s3.buckets: ListBuckets failed and default_bucket also unreachable",
				"endpoint_id", id, "name", name, "bucket", defaultBucket,
				"list_err", err, "head_err", perr)
			writeErr(w, http.StatusBadGateway, fmt.Errorf(
				"ListBuckets: %v; HeadBucket(%q): %v", err, defaultBucket, perr))
			return
		} else {
			slog.Warn("s3.buckets: default_bucket unreachable but ListBuckets ok with 0",
				"endpoint_id", id, "name", name, "bucket", defaultBucket, "err", perr)
		}
	}

	if err != nil {
		slog.Warn("s3.buckets: ListBuckets failed and no default_bucket configured",
			"endpoint_id", id, "name", name, "err", err)
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": buckets})
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (d *S3Deps) Objects(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	bucket := chi.URLParam(r, "bucket")
	prefix := r.URL.Query().Get("prefix")
	cont := r.URL.Query().Get("continuation")
	c, err := d.clientFor(r, id)
	if err != nil {
		slog.Warn("s3.objects: client build failed",
			"endpoint_id", id, "bucket", bucket, "err", err)
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	objs, next, err := c.ListObjects(r.Context(), bucket, prefix, cont)
	if err != nil {
		slog.Warn("s3.objects: ListObjects failed",
			"endpoint_id", id, "bucket", bucket, "prefix", prefix, "err", err)
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"objects": objs, "next": next})
}

func (d *S3Deps) DeleteObjects(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	bucket := chi.URLParam(r, "bucket")
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	c, err := d.clientFor(r, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, err := c.DeleteObjects(r.Context(), bucket, body.Keys)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	d.Audit.Log(r.Context(), r, "s3.object.delete", "s3_bucket", bucket, map[string]any{"count": n})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// ─── encryption + decode helpers ────────────────────────────────────────────

// clientFor decrypts the row and returns a connected s3.Client.
func (d *S3Deps) clientFor(r *http.Request, endpointID string) (*s3.Client, error) {
	var access, encS, endpoint, region string
	var ps int
	if err := d.DB.QueryRowContext(r.Context(), `
		SELECT access_key, secret_key, endpoint, region, path_style
		FROM s3_endpoints WHERE id = ?
	`, endpointID).Scan(&access, &encS, &endpoint, &region, &ps); err != nil {
		return nil, fmt.Errorf("endpoint lookup: %w", err)
	}
	secret, err := d.decrypt(encS)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret_key: %w", err)
	}
	return s3.NewClient(r.Context(), s3.EndpointConfig{
		Endpoint:  endpoint,
		Region:    region,
		PathStyle: ps == 1,
		Creds:     s3.Credentials{AccessKey: access, SecretKey: secret},
	})
}

// encrypt wraps a string with AES-256-GCM using SecretsKey, returns base64 (nonce || ciphertext).
func (d *S3Deps) encrypt(plaintext string) (string, error) {
	gcm, err := newGCM(d.SecretsKey)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, ct...)), nil
}

func (d *S3Deps) decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(d.SecretsKey)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func newGCM(hexKey string) (cipher.AEAD, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes (got %d)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// looksMasked is reserved for future use (currently no fields are masked
// since access_key is shown in clear).
func looksMasked(s string) bool { return strings.Contains(s, "•••") }

var _ = looksMasked

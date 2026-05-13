package ddns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const cloudflareBase = "https://api.cloudflare.com/client/v4"

// Cloudflare is a thin client for the Cloudflare DNS API. It's intentionally
// stateless: callers construct one per request (or once per process if they
// like) and pass the token at construction time. The http.Client is
// injectable so tests can swap a fake.
type Cloudflare struct {
	Token  string
	HTTP   *http.Client
	Base   string // override for tests; empty = cloudflareBase
}

func NewCloudflare(token string) *Cloudflare {
	return &Cloudflare{
		Token: token,
		HTTP:  &http.Client{Timeout: 15 * time.Second},
	}
}

type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Record struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment,omitempty"`
}

type cfResp[T any] struct {
	Success bool       `json:"success"`
	Errors  []cfError  `json:"errors"`
	Result  T          `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Cloudflare) base() string {
	if c.Base != "" {
		return c.Base
	}
	return cloudflareBase
}

func (c *Cloudflare) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("cloudflare response (status %d): %s", resp.StatusCode, string(raw))
	}
	return nil
}

// Verify checks the token by calling /user/tokens/verify.
func (c *Cloudflare) Verify(ctx context.Context) error {
	var out cfResp[map[string]any]
	if err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil, &out); err != nil {
		return err
	}
	if !out.Success {
		return fmt.Errorf("token verify failed: %s", joinErrs(out.Errors))
	}
	return nil
}

func (c *Cloudflare) ListZones(ctx context.Context) ([]Zone, error) {
	var out cfResp[[]Zone]
	if err := c.do(ctx, http.MethodGet, "/zones?per_page=100", nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, fmt.Errorf("list zones: %s", joinErrs(out.Errors))
	}
	return out.Result, nil
}

func (c *Cloudflare) ListRecords(ctx context.Context, zoneID string) ([]Record, error) {
	var out cfResp[[]Record]
	if err := c.do(ctx, http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/dns_records?per_page=200", nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, fmt.Errorf("list records: %s", joinErrs(out.Errors))
	}
	return out.Result, nil
}

func (c *Cloudflare) CreateRecord(ctx context.Context, zoneID string, r Record) (Record, error) {
	var out cfResp[Record]
	if err := c.do(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", r, &out); err != nil {
		return Record{}, err
	}
	if !out.Success {
		return Record{}, fmt.Errorf("create record: %s", joinErrs(out.Errors))
	}
	return out.Result, nil
}

func (c *Cloudflare) UpdateRecord(ctx context.Context, zoneID, recordID string, r Record) (Record, error) {
	var out cfResp[Record]
	if err := c.do(ctx, http.MethodPut, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), r, &out); err != nil {
		return Record{}, err
	}
	if !out.Success {
		return Record{}, fmt.Errorf("update record: %s", joinErrs(out.Errors))
	}
	return out.Result, nil
}

// PatchRecordContent updates only the content of a record. Used by the
// auto-update worker so it doesn't risk overwriting flags the user set
// elsewhere (e.g. proxied, comment) by sending stale values.
func (c *Cloudflare) PatchRecordContent(ctx context.Context, zoneID, recordID, content string) error {
	var out cfResp[Record]
	body := map[string]string{"content": content}
	if err := c.do(ctx, http.MethodPatch, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), body, &out); err != nil {
		return err
	}
	if !out.Success {
		return fmt.Errorf("patch record: %s", joinErrs(out.Errors))
	}
	return nil
}

func (c *Cloudflare) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	var out cfResp[map[string]any]
	if err := c.do(ctx, http.MethodDelete, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), nil, &out); err != nil {
		return err
	}
	if !out.Success {
		return fmt.Errorf("delete record: %s", joinErrs(out.Errors))
	}
	return nil
}

func joinErrs(errs []cfError) string {
	if len(errs) == 0 {
		return "no error details"
	}
	out := ""
	for i, e := range errs {
		if i > 0 {
			out += "; "
		}
		out += fmt.Sprintf("%d %s", e.Code, e.Message)
	}
	return out
}

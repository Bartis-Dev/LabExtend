// Package discord posts payloads to a Discord webhook. Event notifications
// (internal/notify) and backup success summaries both go through it.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ColorGreen  = 0x2ECC71
	ColorYellow = 0xF1C40F
	ColorRed    = 0xE74C3C
	ColorBlue   = 0x3B82F6
)

type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type Footer struct {
	Text string `json:"text"`
}

type Embed struct {
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Color       int     `json:"color,omitempty"`
	Timestamp   string  `json:"timestamp,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
	Footer      *Footer `json:"footer,omitempty"`
}

type Payload struct {
	Username  string  `json:"username,omitempty"`
	AvatarURL string  `json:"avatar_url,omitempty"`
	Content   string  `json:"content,omitempty"`
	Embeds    []Embed `json:"embeds,omitempty"`
}

// Client is a thin wrapper around the Discord webhook URL. Use NewClient.
type Client struct {
	url        string
	httpClient *http.Client
}

func NewClient(url string) *Client {
	return &Client{
		url: url,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			// A redirect would carry the payload to wherever the response
			// points, so a URL that passed validation could still forward
			// messages to an arbitrary host.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Post sends the payload to the webhook. Retries up to 3 times on HTTP 429
// (rate limited), honoring the Retry-After header. Returns nil on 2xx.
func (c *Client) Post(ctx context.Context, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("post: %w", stripURL(err))
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryAfter):
			}
			continue
		}
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return fmt.Errorf("discord %d: %s", resp.StatusCode, string(buf))
	}
	return fmt.Errorf("discord: still rate-limited after retries")
}

// stripURL drops the request URL from a transport error. *url.Error prints it
// in full, and a Discord webhook URL carries its token, while these errors end
// up in notifications.delivery_error, API responses and the log.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// ValidWebhookURL accepts only https Discord webhook URLs. Anything else
// would let the leader post (with retries) to an arbitrary host of the
// caller's choosing.
func ValidWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("webhook url is not a valid URL")
	}
	if u.Scheme != "https" {
		return errors.New("webhook url must use https")
	}
	switch u.Host {
	case "discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com":
	default:
		return errors.New("webhook url must point to discord.com (or discordapp.com, ptb./canary.discord.com)")
	}
	if u.User != nil || !strings.HasPrefix(u.Path, "/api/webhooks/") {
		return errors.New("webhook url must be a Discord webhook (https://discord.com/api/webhooks/...)")
	}
	return nil
}

func parseRetryAfter(s string) time.Duration {
	if s == "" {
		return 2 * time.Second
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n*1000) * time.Millisecond
	}
	return 2 * time.Second
}

// BackupSummary builds a Payload for a finished backup run.
// status ∈ {"success","partial","failed"}.
func BackupSummary(planName, runID, status string, fields []Field, footerVersion string, when time.Time) Payload {
	title := "Backup " + status + " · " + planName
	color := ColorGreen
	switch status {
	case "partial":
		color = ColorYellow
	case "failed":
		color = ColorRed
	}
	return Payload{
		Username: "LabExtend",
		Embeds: []Embed{{
			Title:     title,
			Color:     color,
			Timestamp: when.UTC().Format(time.RFC3339),
			Fields:    fields,
			Footer:    &Footer{Text: "LabExtend " + footerVersion},
		}},
	}
}

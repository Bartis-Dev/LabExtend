// Package discord posts payloads to a Discord webhook. The interface is also
// reused for backup-result embeds; future channels (Slack/Teams/generic JSON)
// should introduce a notifier.Notifier interface and rename this file.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
			return fmt.Errorf("post: %w", err)
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

func parseRetryAfter(s string) time.Duration {
	if s == "" {
		return 2 * time.Second
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n*1000) * time.Millisecond
	}
	return 2 * time.Second
}

// AlertEmbed builds a Payload for an alert trigger or recover.
// state ∈ {"triggered","recovered"}.
func AlertEmbed(state, ruleName, scope, message string, value float64, when time.Time) Payload {
	color := ColorRed
	title := "ALERT FIRED · " + ruleName
	if state == "recovered" {
		color = ColorGreen
		title = "Alert recovered · " + ruleName
	}
	fields := []Field{
		{Name: "Scope", Value: scope, Inline: true},
		{Name: "Value", Value: fmt.Sprintf("%.2f", value), Inline: true},
	}
	if message != "" {
		fields = append(fields, Field{Name: "Detail", Value: message})
	}
	return Payload{
		Username: "labextend",
		Embeds: []Embed{{
			Title:     title,
			Color:     color,
			Timestamp: when.UTC().Format(time.RFC3339),
			Fields:    fields,
			Footer:    &Footer{Text: "labextend"},
		}},
	}
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
		Username: "labextend",
		Embeds: []Embed{{
			Title:     title,
			Color:     color,
			Timestamp: when.UTC().Format(time.RFC3339),
			Fields:    fields,
			Footer:    &Footer{Text: "labextend " + footerVersion},
		}},
	}
}

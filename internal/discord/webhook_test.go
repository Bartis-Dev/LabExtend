package discord

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostErrorDoesNotLeakWebhookToken(t *testing.T) {
	// A closed port gives a transport error, which net/http wraps in a
	// *url.Error that prints the full URL.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	const token = "s3cr3t-webhook-token"
	err = NewClient("http://"+addr+"/api/webhooks/1/"+token).Post(context.Background(), Payload{Content: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "/api/webhooks/") {
		t.Fatalf("error leaks the webhook URL: %v", err)
	}
}

func TestPostDoesNotFollowRedirects(t *testing.T) {
	var hit bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redir.Close()

	if err := NewClient(redir.URL).Post(context.Background(), Payload{Content: "x"}); err == nil {
		t.Error("a redirect must not count as delivered")
	}
	if hit {
		t.Error("the client followed the redirect")
	}
}

func TestValidWebhookURL(t *testing.T) {
	ok := []string{
		"https://discord.com/api/webhooks/123/abc",
		"https://discordapp.com/api/webhooks/123/abc",
		"https://ptb.discord.com/api/webhooks/123/abc",
		"https://canary.discord.com/api/webhooks/123/abc",
	}
	for _, u := range ok {
		if err := ValidWebhookURL(u); err != nil {
			t.Errorf("%s rejected: %v", u, err)
		}
	}
	bad := []string{
		"http://discord.com/api/webhooks/123/abc",
		"https://evil.example/api/webhooks/123/abc",
		"https://discord.com.evil.example/api/webhooks/1/a",
		"https://discord.com:8443/api/webhooks/1/a",
		"https://user@discord.com/api/webhooks/1/a",
		"https://discord.com/api/users/@me",
		"https://169.254.169.254/latest/meta-data",
		"discord.com/api/webhooks/1/a",
		"",
	}
	for _, u := range bad {
		if err := ValidWebhookURL(u); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}

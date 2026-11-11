package s3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The retention prune keeps DB rows for keys the provider did not confirm, so
// per-key errors in the DeleteObjects response must reach the caller.
func TestDeleteKeysReportsPerKeyErrors(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<DeleteResult><Error><Key>b</Key><Code>AccessDenied</Code><Message>Access Denied</Message></Error></DeleteResult>`)
	}))
	defer srv.Close()

	c, err := NewClient(context.Background(), EndpointConfig{
		Endpoint: srv.URL, Region: "auto", PathStyle: true, Creds: Credentials{AccessKey: "ak", SecretKey: "sk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := c.DeleteKeys(context.Background(), "bucket", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].Key != "b" || failed[0].Code != "AccessDenied" {
		t.Errorf("failed = %+v", failed)
	}
	if !strings.Contains(body, "<Key>a</Key>") || !strings.Contains(body, "<Quiet>true</Quiet>") {
		t.Errorf("request body = %s", body)
	}
}

package agent

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A failed inspect ended the tail goroutine while its entry stayed in
// lc.streams, so Sync never started that container's logs again.
func TestTailRetriesFailedInspect(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix socket not available: %v", err)
	}
	var inspects, logs atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logs") {
			logs.Add(1)
			<-r.Context().Done()
			return
		}
		if inspects.Add(1) == 1 {
			http.Error(w, "busy", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"Id":"c1","Name":"/web","Config":{"Tty":true}}`))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	lc := newLogCollector(newDockerClient(sock))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lc.Sync(ctx, map[string]containerSummary{"c1": {ID: "c1", State: "running"}})

	deadline := time.Now().Add(5 * time.Second)
	for logs.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("log stream never opened after a failed inspect (inspects=%d)", inspects.Load())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// Docker answers stats?stream=0 only after it has two samples, about a second
// per container. Done one after another inside the 5 s sample budget, only the
// first few containers of a host were ever reported, and every container after
// them (Traefik on the manager, for one) was invisible to the leader.
func TestSampleReportsEveryContainerWhenStatsAreSlow(t *testing.T) {
	const n = 10
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix socket not available: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		var list []map[string]any
		for i := 0; i < n; i++ {
			list = append(list, map[string]any{"Id": fmt.Sprintf("c%d", i), "Names": []string{fmt.Sprintf("/c%d", i)}, "Image": "img", "State": "running"})
		}
		json.NewEncoder(w).Encode(list)
	})
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stats") {
			select {
			case <-time.After(time.Second):
			case <-r.Context().Done():
				return
			}
			w.Write([]byte(`{"memory_stats":{"usage":1}}`))
			return
		}
		w.Write([]byte(`{"Id":"x","State":{"Status":"running"},"HostConfig":{},"Config":{}}`))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	c := newContainerCollector(newDockerClient(sock), 5*time.Second)
	var got *pb.ContainerReport
	c.sampleOnce(context.Background(), func(m *pb.AgentMessage) error {
		got = m.GetEvent().GetContainerReport()
		return nil
	}, nil)
	if got == nil {
		t.Fatal("no report emitted")
	}
	if len(got.Containers) != n {
		t.Fatalf("reported %d of %d containers", len(got.Containers), n)
	}
	for _, s := range got.Containers {
		if s.MemUsedBytes != 1 {
			t.Fatalf("container %s reported without stats", s.ContainerId)
		}
	}
}

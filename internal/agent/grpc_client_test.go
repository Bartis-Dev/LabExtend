package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/config"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// chanStream feeds recvLoop from a channel; the rest of the interface is unused.
type chanStream struct {
	pb.ManagerAgent_ChannelClient
	in chan *pb.LeaderMessage
}

func (s chanStream) Recv() (*pb.LeaderMessage, error) {
	m, ok := <-s.in
	if !ok {
		return nil, io.EOF
	}
	return m, nil
}

// Commands ran inline in recvLoop, so a running backup blocked every other
// command, including the CancelBackup meant to stop it.
func TestRecvLoopDoesNotBlockOnLongCommand(t *testing.T) {
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stall:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(stall)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Client{handler: &Handler{cfg: &config.Config{}, monitor: &monitor{}}}
	in := make(chan *pb.LeaderMessage, 2)
	results := make(chan string, 2)
	send := func(m *pb.AgentMessage) error {
		results <- m.RequestId
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.recvLoop(ctx, chanStream{in: in}, send)

	in <- &pb.LeaderMessage{RequestId: "backup", Kind: &pb.LeaderMessage_Command{Command: &pb.Command{
		Op: &pb.Command_RunBackup{RunBackup: &pb.RunBackupReq{
			PlanId: "p", RunId: "r1", Sources: []string{src},
			S3Endpoint: srv.URL, S3Region: "auto", S3Bucket: "b", S3Key: "k",
			S3AccessKey: "a", S3SecretKey: "s", S3PathStyle: true,
		}},
	}}}
	time.Sleep(200 * time.Millisecond)
	in <- &pb.LeaderMessage{RequestId: "cancel", Kind: &pb.LeaderMessage_Command{Command: &pb.Command{
		Op: &pb.Command_CancelBackup{CancelBackup: &pb.CancelBackupReq{RunId: "r1"}},
	}}}

	got := map[string]bool{}
	timeout := time.After(10 * time.Second)
	for len(got) < 2 {
		select {
		case id := <-results:
			got[id] = true
		case <-timeout:
			t.Fatalf("results so far %v: a running backup blocks other commands", got)
		}
	}
}

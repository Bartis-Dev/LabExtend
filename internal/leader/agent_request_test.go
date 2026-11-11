package leader

import (
	"context"
	"testing"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// nopStream accepts every Send; the rest of the interface is never called.
type nopStream struct{ pb.ManagerAgent_ChannelServer }

func (nopStream) Send(*pb.LeaderMessage) error { return nil }

// A backup Request used to wait for its 4h run deadline when the agent's
// stream dropped mid-request, leaving the run on "running" all that time.
func TestRequestFailsWhenStreamEnds(t *testing.T) {
	c := &AgentConn{ID: "n1", stream: nopStream{}, pending: map[string]chan *pb.AgentMessage{}, gone: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		_, err := c.Request(context.Background(), &pb.Command{})
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	c.markGone()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Request returned no error for a dropped stream")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Request still waiting after the stream ended")
	}
	// A Request on a conn that is already gone must not wait either.
	if _, err := c.Request(context.Background(), &pb.Command{}); err == nil {
		t.Fatal("Request on a gone conn succeeded")
	}
}

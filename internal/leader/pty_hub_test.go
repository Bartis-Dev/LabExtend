package leader

import (
	"testing"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

func TestPtyHubRoutesToItsOwnSession(t *testing.T) {
	h := newPtyHub()
	a, stopA := h.subscribe("a")
	defer stopA()
	b, stopB := h.subscribe("b")
	defer stopB()

	h.deliver(&pb.PtyOutput{SessionId: "a", Data: []byte("for-a")})
	h.deliver(&pb.PtyOutput{SessionId: "nobody", Data: []byte("dropped")})

	select {
	case got := <-a.out:
		if string(got.GetData()) != "for-a" {
			t.Fatalf("got %q", got.GetData())
		}
	default:
		t.Fatal("session a received nothing")
	}
	select {
	case got := <-b.out:
		t.Fatalf("session b received %q — output leaked across sessions", got.GetData())
	default:
	}
}

// A terminal cannot survive a hole in its byte stream, so an overflowing
// subscriber has to be killed rather than silently skipped.
func TestPtyHubKillsOverflowingSubscriber(t *testing.T) {
	h := newPtyHub()
	sub, stop := h.subscribe("s")
	defer stop()

	for i := 0; i < cap(sub.out)+10; i++ {
		h.deliver(&pb.PtyOutput{SessionId: "s", Data: []byte("x")})
	}

	select {
	case <-sub.dead:
	default:
		t.Fatal("subscriber was overrun but never marked dead")
	}
}

func TestPtyHubUnsubscribeStopsDelivery(t *testing.T) {
	h := newPtyHub()
	sub, stop := h.subscribe("s")
	stop()

	h.deliver(&pb.PtyOutput{SessionId: "s", Data: []byte("late")})

	select {
	case got := <-sub.out:
		t.Fatalf("delivered %q after unsubscribe", got.GetData())
	default:
	}
}

// SSE tails and terminal sessions outlive any sane request deadline; putting
// them under the router timeout tore healthy connections down after 2 minutes.
func TestStreamingPathsAreExemptFromTheRequestTimeout(t *testing.T) {
	exempt := []string{
		"/api/events",
		"/api/containers/node-1/abc123/logs/stream",
		"/api/nodes/node-1/terminal",
	}
	for _, p := range exempt {
		if !isStreamingPath(p) {
			t.Errorf("%s must be exempt from the request timeout", p)
		}
	}

	timed := []string{
		"/api/nodes",
		"/api/containers/node-1/abc123/logs",
		"/api/nodes/node-1/files",
		"/api/backups/plans",
	}
	for _, p := range timed {
		if isStreamingPath(p) {
			t.Errorf("%s is an ordinary request and must keep its deadline", p)
		}
	}
}

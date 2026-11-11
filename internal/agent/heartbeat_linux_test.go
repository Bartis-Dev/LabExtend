//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

const netDevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:     100       1    0    0    0     0          0         0      100       1    0    0    0     0       0          0
  eth0:    5000      10    0    0    0     0          0         0     7000      12    0    0    0     0       0          0
veth12:     900       9    0    0    0     0          0         0      900       9    0    0    0     0       0          0
`

// The agent container sits in its own netns; /proc/net/dev there shows the
// container's overlay traffic, not the host's.
func TestReadNetBytesPrefersHostInit(t *testing.T) {
	prefix := t.TempDir()
	p := filepath.Join(prefix, "proc", "1", "net", "dev")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(netDevFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	rx, tx, err := readNetBytes(prefix+"/proc/1/net/dev", "/proc/net/dev")
	if err != nil || rx != 5000 || tx != 7000 {
		t.Fatalf("rx=%d tx=%d err=%v, want the host file's eth0 only", rx, tx, err)
	}
	// Without a host mount the agent's own view is still better than nothing.
	if _, _, err := readNetBytes(prefix+"/missing", "/proc/net/dev"); err != nil {
		t.Fatalf("fallback: %v", err)
	}
}

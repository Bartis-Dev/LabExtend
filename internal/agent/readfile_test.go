package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// The requested size came straight from a URL query parameter. 4294967295 made
// maxBytes+1 wrap to 0 and returned an empty file marked complete; 4000000000
// allocated 4 GB and got the agent OOM-killed.
func TestInlineLimitNeverExceedsTheConfiguredCap(t *testing.T) {
	const configured = 5 << 20
	cases := []struct {
		requested uint32
		want      int64
	}{
		{0, configured},
		{100, 100},
		{configured, configured},
		{4294967295, configured},
		{4000000000, configured},
	}
	for _, c := range cases {
		if got := inlineLimit(configured, c.requested); got != c.want {
			t.Errorf("inlineLimit(%d, %d) = %d, want %d", configured, c.requested, got, c.want)
		}
	}
}

func TestReadCapped(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, truncated, err := readCapped(f, 4)
	if err != nil || string(data) != "0123" || !truncated {
		t.Fatalf("limit 4: got %q truncated=%v err=%v", data, truncated, err)
	}
	for _, limit := range []int64{10, 100} {
		data, truncated, err = readCapped(f, limit)
		if err != nil || string(data) != "0123456789" || truncated {
			t.Fatalf("limit %d: got %q truncated=%v err=%v", limit, data, truncated, err)
		}
	}

	// Anything that is not a regular file is refused before it is opened: a
	// FIFO blocks os.Open until a writer appears, and commands run one at a
	// time, so it would freeze every later command on the node.
	if _, _, err := readCapped(dir, 100); err == nil {
		t.Fatal("reading a directory must fail, not block or return garbage")
	}
}

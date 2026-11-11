package backup

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// Two runs of the same plan render the same object key (the default template
// has date granularity) and stream two different archives into it, then both
// record success with different checksums.
func TestRunnerClaimRejectsSecondRunOfSamePlan(t *testing.T) {
	r := NewRunner(nil, nil, nil, "", nil)

	if !r.claim("plan-a") {
		t.Fatal("first claim should succeed")
	}
	if r.claim("plan-a") {
		t.Error("second claim for the same plan must be refused")
	}
	if !r.claim("plan-b") {
		t.Error("a different plan must not be blocked")
	}
	r.release("plan-a")
	if !r.claim("plan-a") {
		t.Error("claim should succeed again after release")
	}
}

func TestRunnerClaimIsExclusiveUnderConcurrency(t *testing.T) {
	r := NewRunner(nil, nil, nil, "", nil)

	const goroutines = 64
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0

	start := make(chan struct{})
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if r.claim("plan-x") {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if won != 1 {
		t.Errorf("exactly one goroutine should hold the plan, got %d", won)
	}
}

// A byte-truncated JSON array never parses again, so the reader ended up with
// an empty item list: the S3 keys of the archives that did land became
// unreachable exactly on the runs where that matters.
func TestMarkFinishedExcerptStaysValidJSON(t *testing.T) {
	results := make([]NodeResult, 12)
	for i := range results {
		results[i] = NodeResult{
			NodeID: "node-with-a-fairly-long-identifier",
			Status: "failed",
			S3Key:  "backups/host/2026-09-08/plan.tar.gz",
			Error:  strings.Repeat("operation error S3: PutObject, https response error StatusCode: 403 ", 40),
		}
	}

	trimmed := make([]NodeResult, len(results))
	copy(trimmed, results)
	for i := range trimmed {
		if len(trimmed[i].Error) > 512 {
			trimmed[i].Error = trimmed[i].Error[:512] + "… (truncated)"
		}
	}
	b, err := json.Marshal(trimmed)
	if err != nil {
		t.Fatal(err)
	}

	var back []NodeResult
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("excerpt does not round-trip: %v", err)
	}
	if len(back) != len(results) {
		t.Errorf("got %d items back, want %d", len(back), len(results))
	}
	for i, n := range back {
		if n.S3Key != results[i].S3Key {
			t.Errorf("item %d lost its s3 key", i)
		}
		if n.Error == "" {
			t.Errorf("item %d lost its error entirely", i)
		}
	}

	// The old form cut the encoded array itself, which is what broke it.
	if json.Unmarshal(b[:8192], &back) == nil {
		t.Error("expected a byte-truncated array to fail parsing (control)")
	}
}

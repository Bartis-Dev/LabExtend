package agent

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// Archiving a live tree means files change size between the walk's stat and
// the read. The header has already committed to the stat'd size, so the entry
// must receive exactly that many bytes. These tests pin both directions plus
// the failure they replaced.

// writeEntry writes one header and copies body into it under the given
// declared size, then closes the archive. It returns the bytes the entry
// actually holds.
func writeEntry(t *testing.T, body string, declared int64) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "f", Mode: 0o644, Size: declared}); err != nil {
		t.Fatalf("header: %v", err)
	}
	if _, err := copyExactly(tw, strings.NewReader(body), declared); err != nil {
		return nil, err
	}
	// Close is where tar reports an entry that received too few bytes, so a
	// test that skips it would pass on a broken pad.
	if err := tw.Close(); err != nil {
		return nil, err
	}

	tr := tar.NewReader(&buf)
	if _, err := tr.Next(); err != nil {
		t.Fatalf("next: %v", err)
	}
	got, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return got, nil
}

// The incident: Postgres appended to a file after it was stat'd.
func TestCopyExactlyTruncatesGrownFile(t *testing.T) {
	got, err := writeEntry(t, "0123456789", 4)
	if err != nil {
		t.Fatalf("copyExactly on a grown file: %v", err)
	}
	if string(got) != "0123" {
		t.Errorf("entry = %q, want %q", got, "0123")
	}
}

// The mirror case: a file truncated after the walk saw it. tar refuses to
// close an entry that received fewer bytes than its header promised, so the
// remainder has to be padded rather than left short.
func TestCopyExactlyPadsShrunkFile(t *testing.T) {
	got, err := writeEntry(t, "abc", 8)
	if err != nil {
		t.Fatalf("copyExactly on a shrunk file: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("entry length = %d, want 8", len(got))
	}
	if string(got[:3]) != "abc" {
		t.Errorf("entry prefix = %q, want %q", got[:3], "abc")
	}
	if !bytes.Equal(got[3:], make([]byte, 5)) {
		t.Errorf("padding is not zeroed: %q", got[3:])
	}
}

func TestCopyExactlyUnchangedFile(t *testing.T) {
	got, err := writeEntry(t, "hello", 5)
	if err != nil {
		t.Fatalf("copyExactly: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("entry = %q, want %q", got, "hello")
	}
}

// The behaviour being replaced, kept as a control: an unbounded io.Copy of a
// file that grew returns ErrWriteTooLong. That error travelled out of the
// walk callback and abandoned the whole node's archive - five nights of
// supaserver backups uploaded 0 bytes because of it.
func TestUnboundedCopyStillFailsTheOldWay(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "f", Mode: 0o644, Size: 4}); err != nil {
		t.Fatalf("header: %v", err)
	}
	_, err := io.Copy(tw, strings.NewReader("0123456789"))
	if !errors.Is(err, tar.ErrWriteTooLong) {
		t.Fatalf("expected ErrWriteTooLong from an unbounded copy, got %v", err)
	}
}

package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// TestTarOneRootArchiveIsComplete guards the header/body invariant of
// tarOneRoot: archive/tar accumulates a sticky "missed writing N bytes" error
// as soon as a header is written without exactly hdr.Size bytes behind it, and
// tw.Close() is where that surfaces. A regression there produced archives that
// were uploaded and recorded as successful backups while being truncated.
// This covers the normal path and the end-of-archive marker; the vanished-file
// case is the test below.
func TestTarOneRootArchiveIsComplete(t *testing.T) {
	root := t.TempDir()
	want := map[string]string{
		"a.txt":         "alpha",
		"b.txt":         "",
		"sub/c.txt":     "gamma gamma gamma",
		"sub/deep/d.md": "# delta",
	}
	for name, content := range want {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	var bytesProcessed, filesDone atomic.Uint64

	if err := tarOneRoot(context.Background(), tw, root, &bytesProcessed, &filesDone, nil, "test-run"); err != nil {
		t.Fatalf("tarOneRoot: %v", err)
	}
	// The sticky error lands here if any header went out without its body.
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	base := filepath.Base(root)
	got := map[string]string{}
	tr := tar.NewReader(&buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // proper end-of-archive marker
		}
		if err != nil {
			t.Fatalf("reading archive: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("reading %q: %v", hdr.Name, err)
		}
		if int64(len(body)) != hdr.Size {
			t.Errorf("%q: header says %d bytes, body has %d", hdr.Name, hdr.Size, len(body))
		}
		got[hdr.Name] = string(body)
	}

	for name, content := range want {
		key := base + "/" + name
		if got[key] != content {
			t.Errorf("%q: got %q, want %q", key, got[key], content)
		}
	}
	if n := filesDone.Load(); n == 0 {
		t.Error("filesDone was never incremented")
	}
}

// TestTarOneRootSkipsVanishedFileCleanly reproduces the actual failure: the
// walk stats a file, the file is gone by the time it is opened. Writing the
// header before the open leaves archive/tar permanently short by hdr.Size,
// which fails every later entry and — when the vanished file is the last one —
// yields an archive with no end-of-archive marker that still reports success.
func TestTarOneRootSkipsVanishedFileCleanly(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "gone.txt", "z.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("payload-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	orig := openFile
	openFile = func(p string) (*os.File, error) {
		if filepath.Base(p) == "gone.txt" {
			return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
		}
		return orig(p)
	}
	t.Cleanup(func() { openFile = orig })

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	var bytesProcessed, filesDone atomic.Uint64

	if err := tarOneRoot(context.Background(), tw, root, &bytesProcessed, &filesDone, nil, "test-run"); err != nil {
		t.Fatalf("tarOneRoot: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v (a header was written without its body)", err)
	}

	names := map[string]bool{}
	tr := tar.NewReader(&buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading archive: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("reading %q: %v", hdr.Name, err)
		}
		if int64(len(body)) != hdr.Size {
			t.Errorf("%q: header says %d bytes, body has %d", hdr.Name, hdr.Size, len(body))
		}
		names[filepath.Base(hdr.Name)] = true
	}

	if names["gone.txt"] {
		t.Error("vanished file must not appear in the archive at all")
	}
	for _, want := range []string{"a.txt", "z.txt"} {
		if !names[want] {
			t.Errorf("%q missing from the archive", want)
		}
	}
}

func TestValidSecretName(t *testing.T) {
	// A traversing value here made pg_dump read an arbitrary host file and
	// ship it as PGPASSWORD to an attacker-chosen host.
	bad := []string{"", ".", "..", "../../etc/shadow", "a/b", `a\b`, "sub/secret", "x\x00y"}
	for _, s := range bad {
		if validSecretName(s) {
			t.Errorf("validSecretName(%q) = true, want false", s)
		}
	}
	good := []string{"supabase-postgres-password", "pg_pw", "db.prod.pw", "A1"}
	for _, s := range good {
		if !validSecretName(s) {
			t.Errorf("validSecretName(%q) = false, want true", s)
		}
	}
}

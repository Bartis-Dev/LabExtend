package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// The agent runs in an Alpine container, so os/user resolves the image's
// accounts rather than the host's — every real host account came back
// "unknown user", which is the only case anyone looks a name up for.
func TestLookupInPasswd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passwd")
	content := "" +
		"# a comment line\n" +
		"root:x:0:0:root:/root:/bin/bash\n" +
		"\n" +
		"www-data:x:33:33:www-data:/var/www:/usr/sbin/nologin\n" +
		"postgres:x:999:999:PostgreSQL administrator,,,:/var/lib/postgresql:/bin/bash\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		wantUID  uint32
		wantGID  uint32
		wantHome string
	}{
		{"root", 0, 0, "/root"},
		{"www-data", 33, 33, "/var/www"},
		{"postgres", 999, 999, "/var/lib/postgresql"},
	} {
		got, err := lookupInPasswd(path, tc.name)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Uid != tc.wantUID || got.Gid != tc.wantGID || got.Home != tc.wantHome {
			t.Errorf("%s: got uid=%d gid=%d home=%q, want %d/%d/%q",
				tc.name, got.Uid, got.Gid, got.Home, tc.wantUID, tc.wantGID, tc.wantHome)
		}
	}

	if _, err := lookupInPasswd(path, "nobody-here"); err == nil {
		t.Error("expected an error for an unknown user")
	}
	// A prefix of a real name must not match ("www" is not "www-data").
	if _, err := lookupInPasswd(path, "www"); err == nil {
		t.Error("expected an error for a partial name match")
	}
	if _, err := lookupInPasswd(filepath.Join(dir, "does-not-exist"), "root"); err == nil {
		t.Error("expected an error for a missing passwd file")
	}
}

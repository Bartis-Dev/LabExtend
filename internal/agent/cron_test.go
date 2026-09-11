package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// crontab(5) turns an unescaped % into a newline and hands the rest to the
// command's stdin, so `date +%Y%m%d` ran as `date +`. The command on disk has
// to be escaped, and reading the file back has to give the UI's version again.
func TestCronPercentIsEscapedOnDiskAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bpm")
	m := newCronManager(path, "/bin/sh")
	cmd := `pg_dump db > /backup/db-$(date +%Y%m%d).sql`

	if _, err := m.Apply([]*pb.CronEntry{{Id: "j1", Schedule: "0 3 * * *", Command: cmd, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `date +\%Y\%m\%d`) {
		t.Fatalf("%% not escaped on disk:\n%s", raw)
	}
	if !strings.Contains(string(raw), "SHELL=/bin/sh\n") {
		t.Fatalf("SHELL line missing or wrong:\n%s", raw)
	}

	got, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != cmd {
		t.Fatalf("round trip: got %+v, want command %q", got, cmd)
	}
}

// SHELL=/bin/bash on a host without bash fails every job with "no such file"
// in the host's own log; hosts with bash keep it so bash-syntax jobs survive.
func TestCronShellFallsBackToShWithoutBash(t *testing.T) {
	root := t.TempDir()
	if got := cronShell(root); got != "/bin/sh" {
		t.Fatalf("host without bash: got %q, want /bin/sh", got)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "bash"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := cronShell(root); got != "/bin/bash" {
		t.Fatalf("host with bash: got %q, want /bin/bash", got)
	}
}

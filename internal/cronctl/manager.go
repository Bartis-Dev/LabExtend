// Package cronctl writes /etc/cron.d/bpm atomically on the host. Entries are
// authoritative in the leader DB; this package is the renderer/applier that
// runs inside the agent.
//
// Why /etc/cron.d/bpm (not systemd, not internal scheduler):
//   - one file = one atomic replace (tmp + rename), no per-entry state
//   - works on every distro that runs cron/cronie/dcron (no systemd dep)
//   - each line carries its own user column natively
//   - operators can `cat /etc/cron.d/bpm` to debug
package cronctl

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Entry mirrors pb.CronEntry. Kept here so this package has no proto dep.
type Entry struct {
	ID       string
	Schedule string // 5-field standard cron expression
	Command  string // single-line shell command
	User     string // typically "root"
	Comment  string
	Enabled  bool
}

// DefaultPath is the on-host file we own.
const DefaultPath = "/etc/cron.d/bpm"

// Manager applies a full set of entries to the cron.d drop-in file.
type Manager struct {
	path string
}

// NewManager returns a Manager writing to /etc/cron.d/bpm.
func NewManager() *Manager { return &Manager{path: DefaultPath} }

// NewManagerAt returns a Manager writing to the given path (for tests).
func NewManagerAt(path string) *Manager { return &Manager{path: path} }

// Apply renders entries to a fresh /etc/cron.d/bpm and atomically replaces
// the existing file via tmp + rename. Disabled entries are omitted.
//
// TODO(phase 7): also chown 0:0, chmod 0644 to satisfy cron's strict
// permission requirements.
func (m *Manager) Apply(entries []Entry) (installed uint32, err error) {
	var buf bytes.Buffer
	buf.WriteString("# Managed by labextend — DO NOT EDIT MANUALLY\n")
	buf.WriteString("# Generated at " + time.Now().UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("SHELL=/bin/sh\n")
	buf.WriteString("PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin\n\n")

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	count := uint32(0)
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		if _, perr := parser.Parse(e.Schedule); perr != nil {
			return 0, fmt.Errorf("bad schedule for %s: %w", e.ID, perr)
		}
		if strings.ContainsAny(e.Command, "\n\r") {
			return 0, fmt.Errorf("entry %s: command must be single-line", e.ID)
		}
		user := e.User
		if user == "" {
			user = "root"
		}
		fmt.Fprintf(&buf, "# bpm:%s %s\n", e.ID, strings.ReplaceAll(e.Comment, "\n", " "))
		fmt.Fprintf(&buf, "%s %s %s\n\n", e.Schedule, user, e.Command)
		count++
	}

	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return 0, fmt.Errorf("write tmp: %w", err)
	}
	// TODO(phase 7): os.Chown(tmp, 0, 0)
	if err := os.Rename(tmp, m.path); err != nil {
		return 0, fmt.Errorf("rename: %w", err)
	}
	return count, nil
}

// List parses the current /etc/cron.d/bpm back into entries. Best-effort:
// only entries that match our `# bpm:<id>` header pattern are returned.
// TODO(phase 7): full parser; used for drift detection.
func (m *Manager) List() ([]Entry, error) {
	return nil, errors.New("cronctl.Manager.List: TODO(phase 7)")
}

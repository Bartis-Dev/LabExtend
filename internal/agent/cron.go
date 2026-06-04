package agent

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// cronManager owns the atomic write/read of the agent's cron file. We only
// ever touch ONE file (`/etc/cron.d/bpm`) so labextend never collides with
// system cronjobs or other tools.
type cronManager struct {
	path string
	mu   sync.Mutex
}

func newCronManager(path string) *cronManager {
	return &cronManager{path: path}
}

const cronHeader = "# Managed by labextend — DO NOT EDIT MANUALLY\n" +
	"# Entries are written from the labextend UI / API.\n" +
	"# Re-edit via: https://<leader>/cronjobs\n\n" +
	"SHELL=/bin/bash\n" +
	"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n\n"

// Apply writes the given entries atomically to /etc/cron.d/bpm. Entries with
// enabled=false are commented out so the operator can see them in the file
// but cron skips them. Returns the count of *enabled* entries actually
// installed (i.e. that will fire on schedule).
func (c *cronManager) Apply(entries []*pb.CronEntry) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return 0, err
	}

	var buf strings.Builder
	buf.WriteString(cronHeader)

	var installed uint32
	for _, e := range entries {
		if err := validateCronEntry(e); err != nil {
			return installed, fmt.Errorf("entry %q: %w", e.Id, err)
		}
		comment := strings.TrimSpace(e.Comment)
		if comment != "" {
			fmt.Fprintf(&buf, "# %s\n", strings.ReplaceAll(comment, "\n", " "))
		}
		fmt.Fprintf(&buf, "# id=%s\n", e.Id)
		user := strings.TrimSpace(e.User)
		if user == "" {
			user = "root"
		}
		line := fmt.Sprintf("%s %s %s\n", e.Schedule, user, e.Command)
		if !e.Enabled {
			line = "# DISABLED " + line
		} else {
			installed++
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(buf.String()), 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return installed, nil
}

// List parses /etc/cron.d/bpm back into entries. Best-effort: if the file
// was hand-edited and doesn't match the expected layout, unknown lines are
// returned as comments only.
func (c *cronManager) List() ([]*pb.CronEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	f, err := os.Open(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []*pb.CronEntry
	cur := &pb.CronEntry{Enabled: true}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# id="):
			cur.Id = strings.TrimPrefix(line, "# id=")
		case strings.HasPrefix(line, "# DISABLED "):
			cur.Enabled = false
			rest := strings.TrimPrefix(line, "# DISABLED ")
			parseCronLine(rest, cur)
			out = append(out, cur)
			cur = &pb.CronEntry{Enabled: true}
		case strings.HasPrefix(line, "#"):
			text := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if !looksLikeMetaComment(text) && cur.Comment == "" {
				cur.Comment = text
			}
		case strings.HasPrefix(line, "SHELL=") || strings.HasPrefix(line, "PATH="):
			// env line — ignore
		default:
			parseCronLine(line, cur)
			out = append(out, cur)
			cur = &pb.CronEntry{Enabled: true}
		}
	}
	return out, sc.Err()
}

// parseCronLine splits a cron line "min hr dom mon dow user cmd" into the
// CronEntry struct. Schedule = first 5 fields, user = 6th, cmd = remainder.
func parseCronLine(line string, e *pb.CronEntry) {
	fields := strings.Fields(line)
	if len(fields) < 7 {
		return
	}
	e.Schedule = strings.Join(fields[:5], " ")
	e.User = fields[5]
	e.Command = strings.Join(fields[6:], " ")
}

// looksLikeMetaComment ignores the header/marker comments we generate.
func looksLikeMetaComment(s string) bool {
	low := strings.ToLower(s)
	if strings.Contains(low, "managed by labextend") {
		return true
	}
	if strings.HasPrefix(low, "entries are written") {
		return true
	}
	if strings.HasPrefix(low, "re-edit") {
		return true
	}
	return false
}

// validateCronEntry rejects empty/malformed entries to avoid writing a cron
// file cron will reject (and then no jobs run).
//
// Schedule: 5 fields, each non-empty, no newlines.
// Command:  non-empty, no newlines (cron file is line-based).
// User:     no whitespace, alphanumeric + a few allowed chars.
func validateCronEntry(e *pb.CronEntry) error {
	if e == nil {
		return fmt.Errorf("nil")
	}
	if strings.ContainsAny(e.Schedule, "\n\r") || strings.ContainsAny(e.Command, "\n\r") {
		return fmt.Errorf("newlines not allowed")
	}
	if len(strings.Fields(e.Schedule)) != 5 {
		return fmt.Errorf("schedule must have 5 fields (got %q)", e.Schedule)
	}
	if strings.TrimSpace(e.Command) == "" {
		return fmt.Errorf("empty command")
	}
	if e.User != "" && !userRegex.MatchString(e.User) {
		return fmt.Errorf("invalid user %q", e.User)
	}
	return nil
}

var userRegex = regexp.MustCompile(`^[a-z_][a-z0-9_-]*\$?$`)

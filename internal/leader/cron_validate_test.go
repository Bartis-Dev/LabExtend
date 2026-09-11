package leader

import "testing"

// The agent rewrites a node's whole cron file and refuses the write if any
// entry is invalid. A job the leader stored but the agent rejects therefore
// froze every other job on that node — so the leader has to reject exactly
// what the agent would.
func TestValidateCronJobMatchesWhatTheAgentAccepts(t *testing.T) {
	ok := []struct{ schedule, command, user string }{
		{"0 3 * * *", "echo hi", "root"},
		{"*/5 * * * *", "date +%Y", "www-data"},
		{"0 0 1 jan mon", "x", "backup"},
		{"0 3 * * 7", "x", "root"},
		{"0-30/10 1,2 * * *", "x", "root"},
	}
	for _, c := range ok {
		if err := validateCronJob(c.schedule, c.command, c.user); err != nil {
			t.Errorf("%q / %q / %q rejected: %v", c.schedule, c.command, c.user, err)
		}
	}

	bad := []struct{ name, schedule, command, user string }{
		{"descriptor", "@daily", "x", "root"},
		{"six fields", "0 0 3 * * *", "x", "root"},
		{"four fields", "0 3 * *", "x", "root"},
		{"newline in command", "0 3 * * *", "x\nrm -rf /", "root"},
		{"empty command", "0 3 * * *", "  ", "root"},
		{"bad user", "0 3 * * *", "x", "root; rm"},
		{"junk in a field", "0 3 * * $(id)", "x", "root"},
	}
	for _, c := range bad {
		if err := validateCronJob(c.schedule, c.command, c.user); err == nil {
			t.Errorf("%s: %q / %q / %q accepted", c.name, c.schedule, c.command, c.user)
		}
	}
}

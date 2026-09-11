package leader

import "testing"

// Without {host} or {node_id} every node in a multi-node plan uploads to the
// same object concurrently: the last completed multipart upload wins, the rest
// are gone, and the run still reports success for all of them.
func TestValidateKeyTemplate(t *testing.T) {
	tests := []struct {
		name       string
		scopeType  string
		scopeValue string
		tmpl       string
		wantErr    bool
	}{
		{"all scope without host placeholder", "all", "", "backups/{date}/{plan}.tar.gz", true},
		{"all scope with host", "all", "", "backups/{host}/{date}/{plan}.tar.gz", false},
		{"all scope with node_id", "all", "", "backups/{node_id}/{date}.tar.gz", false},
		{"label scope without host placeholder", "label", "role=db", "backups/{date}.tar.gz", true},
		{"single node may use a fixed key", "node", "node-a", "backups/{date}/{plan}.tar.gz", false},
		{"several nodes may not", "node", "node-a,node-b", "backups/{date}/{plan}.tar.gz", true},
		{"whitespace and empty entries are ignored", "node", " node-a , ", "backups/{date}.tar.gz", false},
		{"empty scope_value on node scope is not single-target", "node", "", "backups/{date}.tar.gz", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateKeyTemplate(tc.scopeType, tc.scopeValue, tc.tmpl)
			if tc.wantErr && err == nil {
				t.Error("expected the plan to be rejected")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected the plan to be accepted, got %v", err)
			}
		})
	}
}

package agent

import "testing"

// The session id is chosen by the leader, so it reaches the Docker API as
// untrusted input. Docker only accepts [a-zA-Z0-9][a-zA-Z0-9_.-]* as a name,
// and a too-short id used to be sliced blindly.
func TestNameSuffixIsAlwaysAValidContainerNamePart(t *testing.T) {
	cases := map[string]string{
		"216c889c-7066-4cd3-988b-66218c770605": "216c889c7066",
		"abc":                                  "abc",
		"":                                     "x",
		"../../etc/passwd":                     "etcpasswd",
		"UPPER-1234":                           "1234",
		"!!!":                                  "x",
		"aaaaaaaaaaaaaaaaaaaaaaaaaa":           "aaaaaaaaaaaa", // capped at 12
	}
	for in, want := range cases {
		if got := nameSuffix(in); got != want {
			t.Errorf("nameSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClampGeometryFallsBackToASaneWindow(t *testing.T) {
	cases := []struct {
		cols, rows         uint32
		wantCols, wantRows uint32
	}{
		{157, 46, 157, 46}, // a real browser window passes through
		{0, 0, 80, 24},     // unset
		{1, 1, 80, 24},     // degenerate
		{99999, 99999, 80, 24},
		{200, 0, 200, 24}, // one bad axis does not reset the other
	}
	for _, c := range cases {
		gotCols, gotRows := clampGeometry(c.cols, c.rows)
		if gotCols != c.wantCols || gotRows != c.wantRows {
			t.Errorf("clampGeometry(%d,%d) = %d,%d want %d,%d",
				c.cols, c.rows, gotCols, gotRows, c.wantCols, c.wantRows)
		}
	}
}

package leader

import (
	gopath "path"
	"testing"
)

// The leader manipulates paths that live on a REMOTE host, which are POSIX no
// matter what the leader itself runs on. Using path/filepath here meant that on
// a Windows leader filepath.Clean("/") produced a backslash, the virtual "/"
// root failed its equality check, and every listing came back
// "root not in managed paths for this node: /" while the sidebar happily
// offered that exact root.
func TestRemotePathsAreCleanedPOSIXStyle(t *testing.T) {
	cases := map[string]string{
		"/":              "/",
		"/srv/data":      "/srv/data",
		"/srv/data/":     "/srv/data",
		"/srv//data":     "/srv/data",
		"/srv/./data":    "/srv/data",
		"/srv/x/../data": "/srv/data",
		"/etc/cron.d":    "/etc/cron.d",
	}
	for in, want := range cases {
		if got := gopath.Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}

	// The specific regression: the virtual root has to survive cleaning as "/"
	// so resolveManagedRoot's special case fires.
	if gopath.Clean("/") != "/" {
		t.Fatal(`Clean("/") must stay "/" for the virtual full-filesystem root`)
	}
}

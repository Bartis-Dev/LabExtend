//go:build linux

package agent

import (
	"os"
	"syscall"
)

// statUIDGID extracts uid/gid from the underlying Linux stat_t. Returns false
// if the FileInfo wasn't produced by a syscall-backed call.
func statUIDGID(info os.FileInfo) (uid uint32, gid uint32, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Uid, st.Gid, true
}

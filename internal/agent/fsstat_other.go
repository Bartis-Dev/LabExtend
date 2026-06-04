//go:build !linux

package agent

import "os"

// statUIDGID stub for non-Linux. Production agents always run on Linux; this
// returns (0,0,false) so the FileEntry just omits uid/gid + owner_name.
func statUIDGID(info os.FileInfo) (uint32, uint32, bool) {
	_ = info
	return 0, 0, false
}

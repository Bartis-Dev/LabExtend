package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/Bartis-Dev/LabExtend/internal/config"
)

// Handler implements the host-side of every gRPC command. The leader sends a
// Command envelope, the dispatcher in grpc_client.go calls the matching
// method here, and the result is shipped back as CommandResult.
//
// Every method must enforce the managed-root constraint via resolvePath().
type Handler struct {
	cfg     *config.Config
	host    *hostCollector
	monitor *monitor
}

// NewHandler returns a Handler bound to the running config.
func NewHandler(cfg *config.Config) *Handler {
	return &Handler{
		cfg:     cfg,
		host:    newHostCollector(),
		monitor: newMonitor(cfg),
	}
}

// FileEntry is the agent-internal mirror of pb.FileEntry. Kept here to avoid
// proto dependency in this skeleton; replaced by pb.FileEntry in phase 6.
type FileEntry struct {
	Name          string
	IsDir         bool
	Size          uint64
	MtimeMs       int64
	Mode          uint32
	UID           uint32
	GID           uint32
	OwnerName     string
	GroupName     string
	SymlinkTarget string
}

// ─── filesystem ─────────────────────────────────────────────────────────────

// ListPath returns the directory entries under root/sub.
// TODO(phase 6): implement with os.ReadDir + syscall.Stat to get UID/GID,
// resolve owner via cached /etc/passwd lookups.
func (h *Handler) ListPath(_ context.Context, root, sub string, showHidden bool) ([]FileEntry, error) {
	_, err := resolvePath(root, sub)
	if err != nil {
		return nil, err
	}
	_ = showHidden
	return nil, errors.New("ListPath: TODO(phase 6)")
}

// Stat returns metadata for a single entry.
// TODO(phase 6).
func (h *Handler) Stat(_ context.Context, path string) (*FileEntry, error) {
	_ = path
	return nil, errors.New("Stat: TODO(phase 6)")
}

// ReadFile reads up to maxBytes from path, returning truncated=true if the
// file exceeded the cap.
// TODO(phase 6).
func (h *Handler) ReadFile(_ context.Context, path string, maxBytes uint32) (data []byte, truncated bool, err error) {
	_, _ = path, maxBytes
	return nil, false, errors.New("ReadFile: TODO(phase 6)")
}

// WriteFile writes data to path with the given mode; applies default owner
// from the managed path config when applyDefaultOwner is true.
// TODO(phase 6).
func (h *Handler) WriteFile(_ context.Context, path string, data []byte, mode uint32, applyDefaultOwner bool) (uint64, error) {
	_, _, _, _ = path, data, mode, applyDefaultOwner
	return 0, errors.New("WriteFile: TODO(phase 6)")
}

// Mkdir creates a directory (optionally with parents).
// TODO(phase 6).
func (h *Handler) Mkdir(_ context.Context, path string, mode uint32, applyDefaultOwner, parents bool) error {
	_, _, _, _ = path, mode, applyDefaultOwner, parents
	return errors.New("Mkdir: TODO(phase 6)")
}

// Rename moves from→to within the same managed root.
// TODO(phase 6): caller must verify both stay under the same root.
func (h *Handler) Rename(_ context.Context, from, to string) error {
	_, _ = from, to
	return errors.New("Rename: TODO(phase 6)")
}

// Delete removes a file or (recursively) a directory.
// TODO(phase 6).
func (h *Handler) Delete(_ context.Context, path string, recursive bool) error {
	_, _ = path, recursive
	return errors.New("Delete: TODO(phase 6)")
}

// Chown sets ownership; uses filepath.WalkDir + os.Lchown for recursive.
// Returns the number of entries actually changed.
// TODO(phase 6).
func (h *Handler) Chown(_ context.Context, path string, uid, gid uint32, recursive bool) (uint64, error) {
	_, _, _, _ = path, uid, gid, recursive
	return 0, errors.New("Chown: TODO(phase 6)")
}

// LookupUser resolves a username to {uid, gid, home}.
// TODO(phase 6): use os/user.Lookup; cache results for 60s.
func (h *Handler) LookupUser(_ context.Context, name string) (uid, gid uint32, home string, err error) {
	_ = name
	return 0, 0, "", errors.New("LookupUser: TODO(phase 6)")
}

// ─── cron ───────────────────────────────────────────────────────────────────

// ApplyCron renders all enabled entries to /etc/cron.d/bpm atomically.
// TODO(phase 7): delegated to internal/cronctl.
func (h *Handler) ApplyCron(_ context.Context, entries any) (uint32, error) {
	_ = entries
	return 0, errors.New("ApplyCron: TODO(phase 7)")
}

// ListCron parses /etc/cron.d/bpm back into entries. May not exactly match
// DB if external edits happened.
// TODO(phase 7).
func (h *Handler) ListCron(_ context.Context) (any, error) {
	return nil, errors.New("ListCron: TODO(phase 7)")
}

// ─── backup ─────────────────────────────────────────────────────────────────

// RunBackup is the meat of phase 9: walk sources → tar+gzip → io.Pipe →
// S3 multipart upload, with progress events sent back on the channel.
// TODO(phase 9): full implementation in internal/backup/runner.go,
// delegated from this handler.
func (h *Handler) RunBackup(_ context.Context, req any) (any, error) {
	_ = req
	return nil, errors.New("RunBackup: TODO(phase 9)")
}

// CancelBackup propagates ctx cancellation to the in-flight RunBackup.
// TODO(phase 9).
func (h *Handler) CancelBackup(_ context.Context, runID string) error {
	_ = runID
	return errors.New("CancelBackup: TODO(phase 9)")
}

// ─── path resolution (used by every fs op) ──────────────────────────────────

// resolvePath joins root and sub, cleans, and verifies the result stays under
// root. Returns the absolute path to use on the host, or an error if the
// caller is trying to escape.
//
// TODO(phase 6): also EvalSymlinks and verify the resolved path stays
// inside root (defense against symlink-escape).
func resolvePath(root, sub string) (string, error) {
	if root == "" {
		return "", errors.New("empty root")
	}
	root = filepath.Clean(root)
	final := filepath.Clean(filepath.Join(root, sub))

	// On the host this runs from inside a container that has '/' bind-mounted
	// at /host:rslave; the caller is responsible for prefixing /host if
	// needed. For now, just enforce the prefix check.
	if !strings.HasPrefix(final, root+string(filepath.Separator)) && final != root {
		return "", errors.New("path escapes managed root")
	}
	return final, nil
}

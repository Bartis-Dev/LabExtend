package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/config"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// Handler implements the host-side of every gRPC command. The leader sends a
// Command envelope, the dispatcher in grpc_client.go calls the matching
// method here, and the result is shipped back as CommandResult.
//
// ListPath enforces the managed-root constraint via resolvePath().
type Handler struct {
	cfg     *config.Config
	host    *hostCollector
	monitor *monitor
	cronctl *cronManager

	// User-lookup cache (60s TTL). user.LookupId is hit during every directory
	// listing — caching is the difference between snappy and unusable on
	// hosts with NSS-backed /etc/passwd.
	userMu    sync.Mutex
	userCache map[uint32]userCacheEntry
}

type userCacheEntry struct {
	name      string
	expiresAt time.Time
}

func NewHandler(cfg *config.Config) *Handler {
	mon := newMonitor(cfg)
	return &Handler{
		cfg:       cfg,
		host:      newHostCollector(),
		monitor:   mon,
		cronctl:   newCronManager(hostPrefix(cfg)+"/etc/cron.d/bpm", cronShell(hostPrefix(cfg))),
		userCache: make(map[uint32]userCacheEntry),
	}
}

// hostPrefix returns the path inside the container that maps to the host's
// "/". Inside docker-stack we mount `/:/host:rslave`, so a request for
// "/srv/data" becomes "/host/srv/data" on disk. Override via BPM_HOST_PREFIX
// (set to empty string when running natively on the host).
func hostPrefix(cfg *config.Config) string {
	if v := os.Getenv("BPM_HOST_PREFIX"); v != "" {
		return v
	}
	if _, err := os.Stat("/host"); err == nil {
		return "/host"
	}
	return ""
}

// cronShell picks the SHELL line for the cron file. bash stays the default
// wherever the host has it, so existing jobs that use bash syntax keep
// working; hosts without it (Alpine, slim images) get sh instead of a SHELL
// that fails every job with "no such file or directory".
func cronShell(prefix string) string {
	if _, err := os.Stat(prefix + "/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

// ─── filesystem ─────────────────────────────────────────────────────────────

func (h *Handler) ListPath(ctx context.Context, req *pb.ListPathReq) (*pb.ListPathResp, error) {
	final, err := h.resolvePath(req.Root, req.Sub)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(final)
	if err != nil {
		return nil, err
	}
	out := make([]*pb.FileEntry, 0, len(entries))
	for _, de := range entries {
		if !req.ShowHidden && strings.HasPrefix(de.Name(), ".") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, h.toFileEntry(filepath.Join(final, de.Name()), de.Name(), info))
	}
	return &pb.ListPathResp{Entries: out}, nil
}

// ─── cron ───────────────────────────────────────────────────────────────────

func (h *Handler) ApplyCron(_ context.Context, req *pb.ApplyCronReq) (*pb.ApplyCronResp, error) {
	n, err := h.cronctl.Apply(req.Entries)
	if err != nil {
		return nil, err
	}
	return &pb.ApplyCronResp{Installed: n}, nil
}

func (h *Handler) ListCron(_ context.Context) (*pb.ListCronResp, error) {
	entries, err := h.cronctl.List()
	if err != nil {
		return nil, err
	}
	return &pb.ListCronResp{Entries: entries}, nil
}

// ─── path resolution ────────────────────────────────────────────────────────

// resolvePath joins root and sub, cleans, ensures the result stays under root,
// then prefixes with the host bind-mount path if configured. Caller passes
// HOST paths (e.g. "/srv/data"); the returned absolute path is what we open
// on disk (e.g. "/host/srv/data").
func (h *Handler) resolvePath(root, sub string) (string, error) {
	if root == "" {
		return "", errors.New("empty root")
	}
	root = filepath.Clean(root)
	final := filepath.Clean(filepath.Join(root, sub))

	// Special-case the virtual full-filesystem root "/". Any absolute path
	// is by definition under "/", but the generic prefix-check below fails
	// for it (string(root + Separator) becomes "//", and "/boot" doesn't
	// start with "//"). filepath.Rel + a ".." scan is the standard way to
	// express "is final inside root?" without that pitfall.
	if root == string(filepath.Separator) {
		if !filepath.IsAbs(final) {
			return "", fmt.Errorf("path %s is not absolute", final)
		}
		return hostPrefix(h.cfg) + final, nil
	}

	if final != root && !strings.HasPrefix(final, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes managed root: %s vs %s", final, root)
	}
	return hostPrefix(h.cfg) + final, nil
}

// toFileEntry builds a pb.FileEntry from os.FileInfo. The uid/gid extraction
// is OS-specific (statUIDGID lives in fsstat_linux.go / fsstat_other.go).
func (h *Handler) toFileEntry(absPath, name string, info os.FileInfo) *pb.FileEntry {
	e := &pb.FileEntry{
		Name:    name,
		IsDir:   info.IsDir(),
		Size:    uint64(info.Size()),
		MtimeMs: info.ModTime().UnixMilli(),
		Mode:    uint32(info.Mode().Perm()),
	}
	if uid, gid, ok := statUIDGID(info); ok {
		e.Uid = uid
		e.Gid = gid
		e.OwnerName = h.cachedUserName(uid)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(absPath); err == nil {
			e.SymlinkTarget = target
		}
	}
	return e
}

// cachedUserName caches uid → username with 60s TTL.
func (h *Handler) cachedUserName(uid uint32) string {
	h.userMu.Lock()
	defer h.userMu.Unlock()
	if e, ok := h.userCache[uid]; ok && time.Now().Before(e.expiresAt) {
		return e.name
	}
	name := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(name); err == nil {
		name = u.Username
	}
	h.userCache[uid] = userCacheEntry{name: name, expiresAt: time.Now().Add(60 * time.Second)}
	return name
}

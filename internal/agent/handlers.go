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
// Every fs method enforces the managed-root constraint via resolvePath().
type Handler struct {
	cfg     *config.Config
	host    *hostCollector
	monitor *monitor
	cronctl *cronManager

	// User-lookup cache (60s TTL). LookupUser is hit during every directory
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
	return &Handler{
		cfg:       cfg,
		host:      newHostCollector(),
		monitor:   newMonitor(cfg),
		cronctl:   newCronManager(hostPrefix(cfg) + "/etc/cron.d/bpm"),
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

func (h *Handler) Stat(_ context.Context, req *pb.StatReq) (*pb.StatResp, error) {
	final, err := h.resolveAbs(req.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(final)
	if err != nil {
		return nil, err
	}
	return &pb.StatResp{Entry: h.toFileEntry(final, info.Name(), info)}, nil
}

func (h *Handler) ReadFile(_ context.Context, req *pb.ReadFileReq) (*pb.ReadFileResp, error) {
	final, err := h.resolveAbs(req.Path)
	if err != nil {
		return nil, err
	}
	maxBytes := req.MaxBytes
	if maxBytes == 0 {
		maxBytes = uint32(h.cfg.FSMaxInlineBytes)
	}
	f, err := os.Open(final)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, maxBytes+1)
	n, _ := f.Read(buf)
	truncated := uint32(n) > maxBytes
	if truncated {
		n = int(maxBytes)
	}
	return &pb.ReadFileResp{Data: buf[:n], Truncated: truncated}, nil
}

func (h *Handler) WriteFile(_ context.Context, req *pb.WriteFileReq) (*pb.WriteFileResp, error) {
	final, err := h.resolveAbs(req.Path)
	if err != nil {
		return nil, err
	}
	mode := os.FileMode(req.Mode)
	if mode == 0 {
		mode = 0o644
	}
	tmp := final + ".bpm-tmp"
	if err := os.WriteFile(tmp, req.Data, mode); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return &pb.WriteFileResp{BytesWritten: uint64(len(req.Data))}, nil
}

func (h *Handler) Mkdir(_ context.Context, req *pb.MkdirReq) (*pb.MkdirResp, error) {
	final, err := h.resolveAbs(req.Path)
	if err != nil {
		return nil, err
	}
	mode := os.FileMode(req.Mode)
	if mode == 0 {
		mode = 0o755
	}
	if req.Parents {
		if err := os.MkdirAll(final, mode); err != nil {
			return nil, err
		}
	} else {
		if err := os.Mkdir(final, mode); err != nil {
			return nil, err
		}
	}
	return &pb.MkdirResp{}, nil
}

func (h *Handler) Rename(_ context.Context, req *pb.RenameReq) (*pb.RenameResp, error) {
	from, err := h.resolveAbs(req.From)
	if err != nil {
		return nil, err
	}
	to, err := h.resolveAbs(req.To)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(from, to); err != nil {
		return nil, err
	}
	return &pb.RenameResp{}, nil
}

func (h *Handler) Delete(_ context.Context, req *pb.DeleteReq) (*pb.DeleteResp, error) {
	final, err := h.resolveAbs(req.Path)
	if err != nil {
		return nil, err
	}
	if req.Recursive {
		if err := os.RemoveAll(final); err != nil {
			return nil, err
		}
	} else {
		if err := os.Remove(final); err != nil {
			return nil, err
		}
	}
	return &pb.DeleteResp{}, nil
}

func (h *Handler) Chown(_ context.Context, req *pb.ChownReq) (*pb.ChownResp, error) {
	final, err := h.resolveAbs(req.Path)
	if err != nil {
		return nil, err
	}
	var count uint64
	if !req.Recursive {
		if err := os.Lchown(final, int(req.Uid), int(req.Gid)); err != nil {
			return nil, err
		}
		count = 1
	} else {
		err := filepath.WalkDir(final, func(p string, _ os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if e := os.Lchown(p, int(req.Uid), int(req.Gid)); e == nil {
				count++
			}
			return nil
		})
		if err != nil {
			return &pb.ChownResp{ChangedCount: count}, err
		}
	}
	return &pb.ChownResp{ChangedCount: count}, nil
}

func (h *Handler) LookupUser(_ context.Context, req *pb.LookupUserReq) (*pb.LookupUserResp, error) {
	u, err := user.Lookup(req.Name)
	if err != nil {
		return nil, err
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(u.Gid, 10, 32)
	return &pb.LookupUserResp{
		Name: u.Username,
		Uid:  uint32(uid),
		Gid:  uint32(gid),
		Home: u.HomeDir,
	}, nil
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

// resolveAbs is the same as resolvePath but for a single absolute path. The
// leader must have already verified the path is under a managed root — this
// function only adds the host-prefix.
func (h *Handler) resolveAbs(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) {
		return "", errors.New("absolute path required")
	}
	return hostPrefix(h.cfg) + filepath.Clean(p), nil
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

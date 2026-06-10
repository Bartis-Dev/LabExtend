package agent

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/s3"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// backupSession tracks one in-flight RunBackup so CancelBackup can target it.
type backupSession struct {
	cancel context.CancelFunc
}

var (
	backupMu   sync.Mutex
	backupRuns = map[string]*backupSession{}
)

// RunBackup is the meat of phase 12: walk sources → tar+gzip → io.Pipe →
// S3 multipart upload, with progress events sent back on the channel.
//
// The agent doesn't have direct access to the gRPC stream — progress events
// flow via the monitor's eventEmitter callback, registered when the stream
// connects. If no emitter is wired (e.g. during a test), progress is logged
// at debug level and the run still completes.
func (h *Handler) RunBackup(ctx context.Context, req *pb.RunBackupReq) (*pb.RunBackupResp, error) {
	if req.RunId == "" {
		return nil, errors.New("missing run_id")
	}
	if len(req.Sources) == 0 {
		return nil, errors.New("no sources")
	}

	// Per-run context so CancelBackup can cancel just this one.
	runCtx, cancel := context.WithCancel(ctx)
	backupMu.Lock()
	backupRuns[req.RunId] = &backupSession{cancel: cancel}
	backupMu.Unlock()
	defer func() {
		backupMu.Lock()
		delete(backupRuns, req.RunId)
		backupMu.Unlock()
		cancel()
	}()

	// Engine dispatch. "" defaults to tar for backwards compatibility with
	// older plans (the DB-migration also defaults engine='tar' for new rows).
	switch req.Engine {
	case "", "tar":
		// fall through to legacy tar pipeline below
	case "pgdump":
		emit := h.monitor.eventEmitter()
		return h.runPgDump(runCtx, req, emit)
	default:
		return nil, fmt.Errorf("unknown backup engine %q", req.Engine)
	}

	started := time.Now()

	// Prefix source paths with host bind-mount.
	prefix := hostPrefix(h.cfg)
	sources := make([]string, len(req.Sources))
	for i, p := range req.Sources {
		sources[i] = prefix + p
	}

	// Build pipeline:
	//   tar.Writer → gzip.Writer → io.Pipe → s3manager.Upload
	pr, pw := io.Pipe()

	// Counters mutated by tar-walker goroutine, read by S3 goroutine + emitter.
	var bytesProcessed atomic.Uint64
	var filesDone atomic.Uint64
	hasher := sha256.New()

	// Progress emitter (best-effort, no blocking).
	emit := h.monitor.eventEmitter()
	stopProgress := startProgressEmitter(runCtx, emit, req.RunId, &bytesProcessed, &filesDone)
	defer stopProgress()

	// Writer side: walk sources, write to tar, count.
	var walkErr error
	go func() {
		defer pw.Close()

		var w io.Writer = pw

		// Always tee through the sha hasher so we get a checksum of the
		// uploaded payload "for free".
		w = io.MultiWriter(pw, hasher)

		gzw := newGzipWriter(w, req.Compression, int(req.Level))
		tw := tar.NewWriter(gzw)

		for _, root := range sources {
			if err := runCtx.Err(); err != nil {
				walkErr = err
				break
			}
			if err := tarOneRoot(runCtx, tw, root, &bytesProcessed, &filesDone, emit, req.RunId); err != nil {
				walkErr = fmt.Errorf("tar %s: %w", root, err)
				break
			}
		}
		_ = tw.Close()
		_ = gzw.Close()
	}()

	// Reader side: stream to S3 in chunks (multipart upload).
	slog.Info("backup: opening uploader",
		"run_id", req.RunId, "endpoint", req.S3Endpoint, "region", req.S3Region,
		"bucket", req.S3Bucket, "key", req.S3Key, "path_style", req.S3PathStyle,
		"access_key", req.S3AccessKey)
	uploader, err := s3.NewUploader(s3.UploaderConfig{
		Endpoint:  req.S3Endpoint,
		Region:    req.S3Region,
		Bucket:    req.S3Bucket,
		AccessKey: req.S3AccessKey,
		SecretKey: req.S3SecretKey,
		PathStyle: req.S3PathStyle,
	})
	if err != nil {
		_ = pr.Close()
		slog.Warn("backup: uploader init failed",
			"run_id", req.RunId, "endpoint", req.S3Endpoint, "err", err)
		return nil, fmt.Errorf("s3 init: %w", err)
	}

	uploaded, uploadErr := uploader.Upload(runCtx, req.S3Key, pr)

	if walkErr != nil {
		slog.Warn("backup: walk failed", "run_id", req.RunId, "err", walkErr)
		return nil, walkErr
	}
	if uploadErr != nil {
		// Log the FULL AWS error string here — the leader truncates it for
		// the Discord embed, this line in the agent log is the source of
		// truth when diagnosing 403 / signature issues.
		slog.Warn("backup: S3 upload failed",
			"run_id", req.RunId, "endpoint", req.S3Endpoint, "region", req.S3Region,
			"bucket", req.S3Bucket, "key", req.S3Key, "path_style", req.S3PathStyle,
			"err", uploadErr.Error())
		return nil, uploadErr
	}

	return &pb.RunBackupResp{
		S3Key:         req.S3Key,
		BytesUploaded: uploaded,
		FileCount:     uint32(filesDone.Load()),
		Sha256:        hex.EncodeToString(hasher.Sum(nil)),
		DurationMs:    time.Since(started).Milliseconds(),
	}, nil
}

func (h *Handler) CancelBackup(_ context.Context, req *pb.CancelBackupReq) (*pb.CancelBackupResp, error) {
	backupMu.Lock()
	defer backupMu.Unlock()
	if s, ok := backupRuns[req.RunId]; ok {
		s.cancel()
		delete(backupRuns, req.RunId)
	}
	return &pb.CancelBackupResp{}, nil
}

// tarOneRoot walks `root` (file or dir), writing every entry into tw as a
// path relative to root's parent. Counters are bumped per file.
func tarOneRoot(
	ctx context.Context,
	tw *tar.Writer,
	root string,
	bytesProcessed *atomic.Uint64,
	filesDone *atomic.Uint64,
	emit eventEmitter,
	runID string,
) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	parent := filepath.Dir(root)

	return filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		// Compute the in-archive name as a path relative to root's parent so
		// untarring reproduces the original tree under the same basename.
		relName, err := filepath.Rel(parent, p)
		if err != nil {
			relName = info.Name()
		}
		relName = filepath.ToSlash(relName)

		var linkTarget string
		if info.Mode()&os.ModeSymlink != 0 {
			t, lerr := os.Readlink(p)
			if lerr != nil {
				return nil // skip broken symlinks
			}
			linkTarget = t
		}

		hdr, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return err
		}
		hdr.Name = relName
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			n, err := io.Copy(tw, f)
			_ = f.Close()
			if err != nil {
				return err
			}
			bytesProcessed.Add(uint64(n))
		}

		filesDone.Add(1)

		// Periodic per-file "current path" event so the UI shows progress.
		// Cheap: only fires on directory boundary (every ~100 files).
		if filesDone.Load()%200 == 0 && emit != nil {
			emit(&pb.AgentMessage{Kind: &pb.AgentMessage_Event{Event: &pb.Event{
				Kind: &pb.Event_BackupProgress{BackupProgress: &pb.BackupProgress{
					RunId:          runID,
					BytesProcessed: bytesProcessed.Load(),
					FilesDone:      filesDone.Load(),
					CurrentPath:    p,
				}},
			}}})
		}

		_ = rootInfo // referenced for clarity
		return nil
	})
}

// newGzipWriter picks a compressor. We support gzip and "none" today; zstd
// is a future option (would pull in klauspost/compress).
func newGzipWriter(w io.Writer, kind string, level int) io.WriteCloser {
	if kind == "none" {
		return &nopWriteCloser{w: w}
	}
	if level <= 0 || level > 9 {
		level = gzip.DefaultCompression
	}
	gz, err := gzip.NewWriterLevel(w, level)
	if err != nil {
		gz = gzip.NewWriter(w)
	}
	return gz
}

type nopWriteCloser struct{ w io.Writer }

func (n *nopWriteCloser) Write(p []byte) (int, error) { return n.w.Write(p) }
func (n *nopWriteCloser) Close() error                { return nil }

// startProgressEmitter sends BackupProgress events every 2s (in addition to
// the on-200-files emits in tarOneRoot). Returns a stop function.
func startProgressEmitter(ctx context.Context, emit eventEmitter, runID string, b *atomic.Uint64, f *atomic.Uint64) func() {
	if emit == nil {
		return func() {}
	}
	stopCh := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-t.C:
				emit(&pb.AgentMessage{Kind: &pb.AgentMessage_Event{Event: &pb.Event{
					Kind: &pb.Event_BackupProgress{BackupProgress: &pb.BackupProgress{
						RunId:          runID,
						BytesProcessed: b.Load(),
						FilesDone:      f.Load(),
					}},
				}}})
			}
		}
	}()
	return func() {
		close(stopCh)
		slog.Debug("backup progress emitter stopped", "run", runID)
	}
}

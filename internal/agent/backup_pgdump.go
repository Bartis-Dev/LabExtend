package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/s3"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// runPgDump implements the "pgdump" engine. Each entry in req.Sources is a
// DSN line in libpq key=value form, e.g.
//
//	"host=db user=supabase_admin dbname=postgres port=5432 password_secret=supabase-postgres-password"
//
// For each source the agent will:
//  1. Resolve the password from /run/secrets/<password_secret> (or PGPASSWORD env)
//  2. Run pg_dump -Fc to a local temp file (custom format compresses inline)
//  3. If verify_restore is true, spin up a throwaway postgres container,
//     pg_restore --schema-only the dump into it, fail the run on any error
//  4. Stream the dump to S3 at the rendered key (one .dump per source if many)
//
// Why custom format (-Fc): we want a single binary file we can verify with
// pg_restore -l and selectively restore from. Plain SQL would also work but
// custom format compresses ~3x better and is the industry default.
func (h *Handler) runPgDump(
	ctx context.Context,
	req *pb.RunBackupReq,
	emit eventEmitter,
) (*pb.RunBackupResp, error) {
	started := time.Now()

	// One DSN per source. Most plans will only have one DSN — multiple is
	// supported in case the user wants several databases captured in one run,
	// but each becomes its own .dump uploaded under a numbered suffix.
	if len(req.Sources) == 0 {
		return nil, fmt.Errorf("pgdump: at least one DSN in sources required")
	}

	tmpDir, err := os.MkdirTemp("", "labextend-pgdump-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var totalBytes uint64
	hasher := sha256.New()

	uploader, err := s3.NewUploader(s3.UploaderConfig{
		Endpoint:  req.S3Endpoint,
		Region:    req.S3Region,
		Bucket:    req.S3Bucket,
		AccessKey: req.S3AccessKey,
		SecretKey: req.S3SecretKey,
		PathStyle: req.S3PathStyle,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 init: %w", err)
	}

	for i, dsn := range req.Sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		dsnParsed, err := parsePGDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", i, err)
		}

		dumpPath := filepath.Join(tmpDir, fmt.Sprintf("dump-%d.bin", i))
		slog.Info("pgdump: starting dump",
			"run_id", req.RunId, "host", dsnParsed.Host, "db", dsnParsed.DBName,
			"path", dumpPath)
		if err := pgDump(ctx, dsnParsed, dumpPath); err != nil {
			return nil, fmt.Errorf("pg_dump %s/%s: %w", dsnParsed.Host, dsnParsed.DBName, err)
		}

		info, _ := os.Stat(dumpPath)
		size := uint64(0)
		if info != nil {
			size = uint64(info.Size())
		}
		slog.Info("pgdump: dump written",
			"run_id", req.RunId, "bytes", size, "db", dsnParsed.DBName)

		// Emit a progress event so the UI sees something happening.
		if emit != nil {
			emit(&pb.AgentMessage{Kind: &pb.AgentMessage_Event{Event: &pb.Event{
				Kind: &pb.Event_BackupProgress{BackupProgress: &pb.BackupProgress{
					RunId:          req.RunId,
					BytesProcessed: size,
					FilesDone:      uint64(i + 1),
					CurrentPath:    fmt.Sprintf("pg_dump %s/%s done", dsnParsed.Host, dsnParsed.DBName),
				}},
			}}})
		}

		if req.VerifyRestore {
			slog.Info("pgdump: verifying via sidecar restore", "run_id", req.RunId)
			if err := verifyRestore(ctx, dumpPath); err != nil {
				return nil, fmt.Errorf("verify restore %s: %w", dsnParsed.DBName, err)
			}
			slog.Info("pgdump: verify ok", "run_id", req.RunId, "db", dsnParsed.DBName)
		}

		// Upload. Key gets a per-source suffix when there are multiple sources.
		key := req.S3Key
		if len(req.Sources) > 1 {
			key = appendKeySuffix(key, fmt.Sprintf(".%s", dsnParsed.DBName))
		}
		f, err := os.Open(dumpPath)
		if err != nil {
			return nil, fmt.Errorf("open dump for upload: %w", err)
		}
		// Tee to hasher so the final response carries a SHA over all dumps.
		teed := io.TeeReader(f, hasher)
		if _, err := uploader.Upload(ctx, key, teed); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("upload %s: %w", key, err)
		}
		_ = f.Close()
		totalBytes += size
		slog.Info("pgdump: uploaded", "run_id", req.RunId, "key", key, "bytes", size)
	}

	return &pb.RunBackupResp{
		S3Key:         req.S3Key,
		BytesUploaded: totalBytes,
		FileCount:     uint32(len(req.Sources)),
		Sha256:        hex.EncodeToString(hasher.Sum(nil)),
		DurationMs:    time.Since(started).Milliseconds(),
	}, nil
}

// pgDSN is the parsed connection info for one source.
type pgDSN struct {
	Host           string
	Port           string
	User           string
	DBName         string
	Password       string // resolved from password_secret OR password=
	PasswordSecret string
}

// parsePGDSN reads libpq-style key=value pairs. Recognized keys:
// host, port, user, dbname, password, password_secret.
// On Linux agents the password is resolved from /run/secrets/<password_secret>
// if password_secret is set, otherwise from password=, otherwise empty
// (works for trust-auth, mostly useful in containers via socket).
func parsePGDSN(s string) (pgDSN, error) {
	d := pgDSN{Port: "5432"}
	for _, tok := range strings.Fields(s) {
		eq := strings.IndexByte(tok, '=')
		if eq < 0 {
			return d, fmt.Errorf("bad DSN token (need key=value): %q", tok)
		}
		k, v := tok[:eq], tok[eq+1:]
		switch k {
		case "host":
			d.Host = v
		case "port":
			d.Port = v
		case "user":
			d.User = v
		case "dbname":
			d.DBName = v
		case "password":
			d.Password = v
		case "password_secret":
			d.PasswordSecret = v
		}
	}
	if d.Host == "" {
		return d, fmt.Errorf("DSN missing host=")
	}
	if d.User == "" {
		return d, fmt.Errorf("DSN missing user=")
	}
	if d.DBName == "" {
		return d, fmt.Errorf("DSN missing dbname=")
	}
	if d.PasswordSecret != "" {
		raw, err := os.ReadFile("/run/secrets/" + d.PasswordSecret)
		if err != nil {
			return d, fmt.Errorf("read password_secret %q: %w", d.PasswordSecret, err)
		}
		d.Password = strings.TrimRight(string(raw), "\r\n")
	}
	return d, nil
}

// pgDump executes pg_dump in custom format. The agent container's image
// MUST have pg_dump available (postgresql-client package on Alpine or
// debian:slim+postgresql-client) — we'll add it to the Dockerfile.
func pgDump(ctx context.Context, d pgDSN, outPath string) error {
	cmd := exec.CommandContext(ctx, "pg_dump",
		"-h", d.Host,
		"-p", d.Port,
		"-U", d.User,
		"-d", d.DBName,
		"-Fc",  // custom format, compressed
		"-Z6",  // compression level (default is 6)
		"-f", outPath,
		"--no-owner", "--no-acl",
		"--quote-all-identifiers",
	)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+d.Password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// verifyRestore spins up a throwaway postgres container, restores the dump
// into it schema-only (fast, doesn't need real data verification), then
// removes it. Uses docker.sock on the host — the agent already has it
// mounted for log/container collection.
func verifyRestore(ctx context.Context, dumpPath string) error {
	cname := fmt.Sprintf("labextend-pgverify-%d", time.Now().UnixNano())

	// Start the sidecar bound to a random port (we connect via -h 127.0.0.1
	// from inside the agent container? No — we exec INTO the sidecar via
	// docker exec, which avoids networking entirely).
	//
	// docker run requires the agent to have docker CLI installed AND access
	// to the socket. The agent image already mounts /var/run/docker.sock for
	// the existing container/log monitor.

	startCmd := exec.CommandContext(ctx, "docker", "run", "-d",
		"--name", cname,
		"--rm",
		"-e", "POSTGRES_PASSWORD=verifypw",
		"-e", "POSTGRES_INITDB_ARGS=--no-sync",
		"postgres:15-alpine",
	)
	if out, err := startCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start sidecar: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("docker", "rm", "-f", cname).Run()

	// Wait up to 25s for postgres to accept connections.
	deadline := time.Now().Add(25 * time.Second)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("sidecar did not become ready within 25s")
		}
		check := exec.Command("docker", "exec", cname,
			"pg_isready", "-U", "postgres", "-h", "/var/run/postgresql")
		if check.Run() == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Copy dump into the sidecar.
	if out, err := exec.CommandContext(ctx, "docker", "cp", dumpPath,
		cname+":/tmp/dump.bin").CombinedOutput(); err != nil {
		return fmt.Errorf("docker cp into sidecar: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// Restore — schema-only because we only need to verify the dump's
	// structural integrity, not that every row imports cleanly (that would
	// double the backup time and produce no extra signal: a corrupt dump
	// also fails schema-only).
	restoreCmd := exec.CommandContext(ctx, "docker", "exec",
		"-e", "PGPASSWORD=verifypw",
		cname,
		"pg_restore",
		"-U", "postgres",
		"-h", "/var/run/postgresql",
		"-d", "postgres",
		"--no-owner", "--no-acl",
		"--exit-on-error",
		"--schema-only",
		"/tmp/dump.bin",
	)
	out, err := restoreCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_restore failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// appendKeySuffix splits an S3 key on the last "." and inserts the suffix
// before it, e.g. backups/foo.dump + ".mydb" → backups/foo.mydb.dump.
func appendKeySuffix(key, suffix string) string {
	dot := strings.LastIndex(key, ".")
	if dot < 0 {
		return key + suffix
	}
	return key[:dot] + suffix + key[dot:]
}

// counter helper kept here because backup.go already uses one and importing
// it cross-file is fine, but doc-link this for readers.
var _ atomic.Uint64

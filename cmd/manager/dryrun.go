package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/Bartis-Dev/LabExtend/internal/backup"
)

// retentionDryRun prints what backup retention would delete, against the real
// DB, without the HTTP server and without deleting anything. It runs before
// config.Load on purpose: in leader mode that would generate secrets on disk.
func retentionDryRun(args []string) int {
	fs := flag.NewFlagSet("retention-dry-run", flag.ExitOnError)
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	dbPath := fs.String("db", filepath.Join(dataDir, "labextend.db"), "path to the leader's SQLite DB")
	listBucket := fs.Bool("list-bucket", false, "also list each plan's bucket prefix and report orphans (needs BPM_SECRETS_KEY)")
	_ = fs.Parse(args)

	if _, err := os.Stat(*dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "retention-dry-run: %v\n", err)
		return 1
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(*dbPath)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "retention-dry-run: open db: %v\n", err)
		return 1
	}
	defer db.Close()

	if err := backup.RetentionReport(context.Background(), os.Stdout, db, os.Getenv("BPM_SECRETS_KEY"), *listBucket); err != nil {
		fmt.Fprintf(os.Stderr, "retention-dry-run: %v\n", err)
		return 1
	}
	return 0
}

package leader

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	// Aliased because several handlers have a local variable called "path".
	// These are paths on a REMOTE host and therefore always POSIX, so
	// path/filepath is the wrong package here: on a Windows leader
	// filepath.Clean("/") returns a backslash, which made the virtual "/" root
	// fail its equality check and rejected every listing with "root not in
	// managed paths for this node: /".
	gopath "path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// FilesDeps bundles what the file handlers need.
type FilesDeps struct {
	DB       *sql.DB
	Registry *AgentRegistry
	Audit    *AuditLogger
}

// NodePath is the JSON shape for /api/nodes/:id/paths.
type NodePath struct {
	ID        int64  `json:"id"`
	NodeID    string `json:"node_id"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	CreatedAt int64  `json:"created_at"`
}

// ─── node_paths CRUD ────────────────────────────────────────────────────────

func (d *FilesDeps) ListPaths(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT id, node_id, label, path, created_at
		FROM node_paths WHERE node_id = ? ORDER BY label
	`, nodeID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []NodePath{}
	for rows.Next() {
		var p NodePath
		if err := rows.Scan(&p.ID, &p.NodeID, &p.Label, &p.Path, &p.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"paths": out})
}

type pathReq struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

func (d *FilesDeps) CreatePath(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	var req pathReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Label == "" || req.Path == "" || !strings.HasPrefix(req.Path, "/") {
		writeErr(w, http.StatusBadRequest, errors.New("label + absolute path required"))
		return
	}
	cleaned := gopath.Clean(req.Path)
	_, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO node_paths (node_id, label, path, created_at)
		VALUES (?, ?, ?, ?)
	`, nodeID, req.Label, cleaned, time.Now().Unix())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "node_path.create", "node", nodeID, map[string]any{"path": cleaned})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *FilesDeps) DeletePath(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	pid := chi.URLParam(r, "pid")
	res, err := d.DB.ExecContext(r.Context(),
		`DELETE FROM node_paths WHERE node_id = ? AND id = ?`, nodeID, pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d.Audit.Log(r.Context(), r, "node_path.delete", "node", nodeID, map[string]any{"path_id": pid})
	n, _ := res.RowsAffected()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// ─── file operations (proxy to agent) ───────────────────────────────────────

// virtualFullRoot is the implicit "/" managed root every node has, with no
// DB entry.
func virtualFullRoot(nodeID string) NodePath {
	return NodePath{
		NodeID: nodeID,
		Label:  "Full filesystem (/)",
		Path:   "/",
	}
}

// resolveManagedRoot looks up the node_paths row for this (nodeID, root) and
// returns the canonical absolute path. Prevents callers from poking outside
// the labels they were allowed to manage.
func (d *FilesDeps) resolveManagedRoot(ctx serverCtx, nodeID, root string) (NodePath, error) {
	cleaned := gopath.Clean(root)
	if cleaned == "/" {
		return virtualFullRoot(nodeID), nil
	}
	var p NodePath
	err := d.DB.QueryRowContext(ctx,
		`SELECT id, node_id, label, path, created_at
		 FROM node_paths WHERE node_id = ? AND path = ?`, nodeID, cleaned,
	).Scan(&p.ID, &p.NodeID, &p.Label, &p.Path, &p.CreatedAt)
	if err != nil {
		return p, fmt.Errorf("root not in managed paths for this node: %s", root)
	}
	return p, nil
}

func (d *FilesDeps) ListFiles(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	root := r.URL.Query().Get("root")
	sub := r.URL.Query().Get("sub")
	showHidden := r.URL.Query().Get("show_hidden") == "true"
	if root == "" {
		writeErr(w, http.StatusBadRequest, errors.New("root required"))
		return
	}
	if _, err := d.resolveManagedRoot(r.Context(), nodeID, root); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	res, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_ListPath{ListPath: &pb.ListPathReq{
			Root: root, Sub: sub, ShowHidden: showHidden,
		}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res.GetListPath())
}

// serverCtx is an alias so handler-helper signatures stay short.
type serverCtx = ctxAlias

// helper to track that we use uuid.Nil to detect uninitialized state.
var _ = uuid.Nil

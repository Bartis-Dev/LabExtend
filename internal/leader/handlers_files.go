package leader

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
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
	ID               int64  `json:"id"`
	NodeID           string `json:"node_id"`
	Label            string `json:"label"`
	Path             string `json:"path"`
	DefaultUID       uint32 `json:"default_uid"`
	DefaultGID       uint32 `json:"default_gid"`
	DefaultUserLabel string `json:"default_user_label,omitempty"`
	ReadOnly         bool   `json:"read_only"`
	CreatedAt        int64  `json:"created_at"`
}

// ─── node_paths CRUD ────────────────────────────────────────────────────────

func (d *FilesDeps) ListPaths(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	rows, err := d.DB.QueryContext(r.Context(), `
		SELECT id, node_id, label, path, default_uid, default_gid,
		       COALESCE(default_user_label,''), read_only, created_at
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
		var ro int
		if err := rows.Scan(&p.ID, &p.NodeID, &p.Label, &p.Path, &p.DefaultUID, &p.DefaultGID,
			&p.DefaultUserLabel, &ro, &p.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		p.ReadOnly = ro == 1
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"paths": out})
}

type pathReq struct {
	Label            string `json:"label"`
	Path             string `json:"path"`
	DefaultUID       uint32 `json:"default_uid"`
	DefaultGID       uint32 `json:"default_gid"`
	DefaultUserLabel string `json:"default_user_label"`
	ReadOnly         bool   `json:"read_only"`
}

func (d *FilesDeps) CreatePath(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	var req pathReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Label == "" || req.Path == "" || !filepath.IsAbs(req.Path) {
		writeErr(w, http.StatusBadRequest, errors.New("label + absolute path required"))
		return
	}
	cleaned := filepath.Clean(req.Path)
	_, err := d.DB.ExecContext(r.Context(), `
		INSERT INTO node_paths
			(node_id, label, path, default_uid, default_gid, default_user_label, read_only, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, nodeID, req.Label, cleaned, req.DefaultUID, req.DefaultGID, req.DefaultUserLabel,
		boolI(req.ReadOnly), time.Now().Unix())
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

// resolveManagedRoot looks up the node_paths row for this (nodeID, root) and
// returns the canonical absolute path. Prevents callers from poking outside
// the labels they were allowed to manage.
// virtualFullRoot is the implicit "/" managed root every node has. Writes
// under it default to uid 0:0, no read-only, no DB entry. User-defined
// Path Profiles take precedence when their path is more specific.
func virtualFullRoot(nodeID string) NodePath {
	return NodePath{
		NodeID:     nodeID,
		Label:      "Full filesystem (/)",
		Path:       "/",
		DefaultUID: 0,
		DefaultGID: 0,
	}
}

func (d *FilesDeps) resolveManagedRoot(ctx serverCtx, nodeID, root string) (NodePath, error) {
	cleaned := filepath.Clean(root)
	if cleaned == "/" {
		return virtualFullRoot(nodeID), nil
	}
	var p NodePath
	var ro int
	err := d.DB.QueryRowContext(ctx,
		`SELECT id, node_id, label, path, default_uid, default_gid,
		        COALESCE(default_user_label,''), read_only, created_at
		 FROM node_paths WHERE node_id = ? AND path = ?`, nodeID, cleaned,
	).Scan(&p.ID, &p.NodeID, &p.Label, &p.Path, &p.DefaultUID, &p.DefaultGID,
		&p.DefaultUserLabel, &ro, &p.CreatedAt)
	if err != nil {
		return p, fmt.Errorf("root not in managed paths for this node: %s", root)
	}
	p.ReadOnly = ro == 1
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

func (d *FilesDeps) StatFile(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path required"))
		return
	}
	if err := d.assertUnderManagedRoot(r.Context(), nodeID, path); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	res, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_Stat{Stat: &pb.StatReq{Path: path}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res.GetStat())
}

func (d *FilesDeps) ReadFile(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path required"))
		return
	}
	maxBytes := uint32(0)
	if v := r.URL.Query().Get("max_bytes"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			maxBytes = uint32(n)
		}
	}
	if err := d.assertUnderManagedRoot(r.Context(), nodeID, path); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	res, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_ReadFile{ReadFile: &pb.ReadFileReq{Path: path, MaxBytes: maxBytes}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res.GetReadFile())
}

type writeFileReq struct {
	Path              string `json:"path"`
	Data              []byte `json:"data"` // base64-encoded in JSON
	Mode              uint32 `json:"mode"`
	ApplyDefaultOwner bool   `json:"apply_default_owner"`
}

func (d *FilesDeps) WriteFile(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	var req writeFileReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path required"))
		return
	}
	root, err := d.findManagedRoot(r.Context(), nodeID, req.Path)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	if root.ReadOnly {
		writeErr(w, http.StatusForbidden, errors.New("managed root is read-only"))
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	res, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_WriteFile{WriteFile: &pb.WriteFileReq{
			Path: req.Path, Data: req.Data, Mode: req.Mode,
			ApplyDefaultOwner: req.ApplyDefaultOwner,
		}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	// Apply default owner via a chown call if requested.
	if req.ApplyDefaultOwner {
		_, _ = conn.RequestWithDefault(r.Context(), &pb.Command{
			Op: &pb.Command_Chown{Chown: &pb.ChownReq{
				Path: req.Path, Uid: root.DefaultUID, Gid: root.DefaultGID, Recursive: false,
			}},
		})
	}
	d.Audit.Log(r.Context(), r, "file.write", "file", nodeID+":"+req.Path, map[string]any{"bytes": len(req.Data)})
	writeJSON(w, http.StatusOK, res.GetWriteFile())
}

type mkdirReq struct {
	Path              string `json:"path"`
	Mode              uint32 `json:"mode"`
	ApplyDefaultOwner bool   `json:"apply_default_owner"`
	Parents           bool   `json:"parents"`
}

func (d *FilesDeps) Mkdir(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	var req mkdirReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	root, err := d.findManagedRoot(r.Context(), nodeID, req.Path)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	_, err = conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_Mkdir{Mkdir: &pb.MkdirReq{
			Path: req.Path, Mode: req.Mode, Parents: req.Parents,
			ApplyDefaultOwner: req.ApplyDefaultOwner,
		}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if req.ApplyDefaultOwner {
		_, _ = conn.RequestWithDefault(r.Context(), &pb.Command{
			Op: &pb.Command_Chown{Chown: &pb.ChownReq{
				Path: req.Path, Uid: root.DefaultUID, Gid: root.DefaultGID, Recursive: false,
			}},
		})
	}
	d.Audit.Log(r.Context(), r, "file.mkdir", "dir", nodeID+":"+req.Path, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type renameReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (d *FilesDeps) Rename(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	var req renameReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := d.findManagedRoot(r.Context(), nodeID, req.From); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	if _, err := d.findManagedRoot(r.Context(), nodeID, req.To); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	if _, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_Rename{Rename: &pb.RenameReq{From: req.From, To: req.To}},
	}); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	d.Audit.Log(r.Context(), r, "file.rename", "file", nodeID+":"+req.From, map[string]any{"to": req.To})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *FilesDeps) Delete(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	path := r.URL.Query().Get("path")
	recursive := r.URL.Query().Get("recursive") == "true"
	if path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path required"))
		return
	}
	root, err := d.findManagedRoot(r.Context(), nodeID, path)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	if root.ReadOnly {
		writeErr(w, http.StatusForbidden, errors.New("read-only root"))
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	if _, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_Delete{Delete: &pb.DeleteReq{Path: path, Recursive: recursive}},
	}); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	d.Audit.Log(r.Context(), r, "file.delete", "file", nodeID+":"+path, map[string]any{"recursive": recursive})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type chownReq struct {
	Path      string `json:"path"`
	UID       uint32 `json:"uid"`
	GID       uint32 `json:"gid"`
	Recursive bool   `json:"recursive"`
}

func (d *FilesDeps) Chown(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	var req chownReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := d.findManagedRoot(r.Context(), nodeID, req.Path); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	res, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_Chown{Chown: &pb.ChownReq{
			Path: req.Path, Uid: req.UID, Gid: req.GID, Recursive: req.Recursive,
		}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	d.Audit.Log(r.Context(), r, "file.chown", "file", nodeID+":"+req.Path,
		map[string]any{"uid": req.UID, "gid": req.GID, "recursive": req.Recursive,
			"changed": res.GetChown().GetChangedCount()})
	writeJSON(w, http.StatusOK, res.GetChown())
}

func (d *FilesDeps) LookupUser(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	name := r.URL.Query().Get("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name required"))
		return
	}
	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	res, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_LookupUser{LookupUser: &pb.LookupUserReq{Name: name}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res.GetLookupUser())
}

// SuggestOwner stats the file at `path` and its parent directory, and
// reports whether the file's owner differs from the parent's. Used by the
// frontend's "Fix permissions" affordance: when you create a file as root
// inside a directory owned by 999:999 (a container user), the file ends
// up as root:root and the UI offers to chown it back to 999:999 — what
// the surrounding files already are.
//
// Returns:
//
//	{
//	  needs_fix: bool,
//	  current:   { uid, gid, owner_name, group_name },
//	  suggested: { uid, gid, owner_name, group_name, source: "parent" },
//	  parent_path: "/var/lib/...",
//	  is_dir: bool,                // if true, recursive chown is meaningful
//	}
func (d *FilesDeps) SuggestOwner(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path required"))
		return
	}
	cleaned := filepath.Clean(body.Path)
	if err := d.assertUnderManagedRoot(r.Context(), nodeID, cleaned); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	parent := filepath.Dir(cleaned)
	if parent == cleaned {
		writeJSON(w, http.StatusOK, map[string]any{
			"needs_fix": false,
			"reason":    "path has no parent (filesystem root)",
		})
		return
	}

	conn, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}

	curRes, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_Stat{Stat: &pb.StatReq{Path: cleaned}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("stat %s: %w", cleaned, err))
		return
	}
	curEntry := curRes.GetStat().GetEntry()
	if curEntry == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("stat returned no entry for %s", cleaned))
		return
	}

	parRes, err := conn.RequestWithDefault(r.Context(), &pb.Command{
		Op: &pb.Command_Stat{Stat: &pb.StatReq{Path: parent}},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("stat parent %s: %w", parent, err))
		return
	}
	parEntry := parRes.GetStat().GetEntry()
	if parEntry == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("stat returned no entry for parent %s", parent))
		return
	}

	needsFix := curEntry.Uid != parEntry.Uid || curEntry.Gid != parEntry.Gid
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_fix": needsFix,
		"current": map[string]any{
			"uid":        curEntry.Uid,
			"gid":        curEntry.Gid,
			"owner_name": curEntry.OwnerName,
			"group_name": curEntry.GroupName,
		},
		"suggested": map[string]any{
			"uid":        parEntry.Uid,
			"gid":        parEntry.Gid,
			"owner_name": parEntry.OwnerName,
			"group_name": parEntry.GroupName,
			"source":     "parent",
		},
		"parent_path": parent,
		"is_dir":      curEntry.IsDir,
	})
}

// ─── helpers ────────────────────────────────────────────────────────────────

// assertUnderManagedRoot returns nil if `path` lies inside ANY of the node's
// managed roots. Operations on files use this; operations on roots use
// resolveManagedRoot directly.
func (d *FilesDeps) assertUnderManagedRoot(ctx serverCtx, nodeID, path string) error {
	_, err := d.findManagedRoot(ctx, nodeID, path)
	return err
}

// findManagedRoot returns the most-specific managed-path entry that
// contains `path`. Falls back to the virtual full-filesystem root if no
// user-defined profile matches but the path is absolute.
func (d *FilesDeps) findManagedRoot(ctx serverCtx, nodeID, path string) (NodePath, error) {
	cleaned := filepath.Clean(path)
	rows, err := d.DB.QueryContext(ctx, `
		SELECT id, node_id, label, path, default_uid, default_gid,
		       COALESCE(default_user_label,''), read_only, created_at
		FROM node_paths WHERE node_id = ?`, nodeID)
	if err != nil {
		return NodePath{}, err
	}
	defer rows.Close()
	var best NodePath
	bestLen := -1
	for rows.Next() {
		var p NodePath
		var ro int
		if err := rows.Scan(&p.ID, &p.NodeID, &p.Label, &p.Path, &p.DefaultUID, &p.DefaultGID,
			&p.DefaultUserLabel, &ro, &p.CreatedAt); err != nil {
			continue
		}
		p.ReadOnly = ro == 1
		if cleaned == p.Path || strings.HasPrefix(cleaned, p.Path+"/") {
			if len(p.Path) > bestLen {
				best = p
				bestLen = len(p.Path)
			}
		}
	}
	if bestLen >= 0 {
		return best, nil
	}
	// No explicit profile matched. Fall back to the virtual full root —
	// every absolute path is reachable, defaults are root:root.
	if strings.HasPrefix(cleaned, "/") {
		return virtualFullRoot(nodeID), nil
	}
	return NodePath{}, fmt.Errorf("path %q is not absolute", cleaned)
}

// serverCtx is an alias so handler-helper signatures stay short.
type serverCtx = ctxAlias

// helper to track that we use uuid.Nil to detect uninitialized state.
var _ = uuid.Nil

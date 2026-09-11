package leader

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// Browser terminal.
//
// The browser holds a WebSocket to the leader; the leader turns it into
// PtyData commands on the agent's existing gRPC channel and pipes PtyOutput
// events back. The shell itself is a privileged nsenter container the agent
// starts on the node (see internal/agent/pty.go) — this endpoint hands out a
// root shell on the host, which is why it lives behind requireAdmin.
//
// Cross-site protection is the WebSocket origin check: coder/websocket rejects
// a handshake whose Origin is not this host, which is what stops a random page
// from opening a shell with the user's cookie. The CSRF token middleware
// cannot help here — a browser will not attach custom headers to a WebSocket
// handshake.

type TerminalDeps struct {
	Registry *AgentRegistry
	Pty      *ptyHub
	Audit    *AuditLogger
}

// terminalMaxSeconds is the session lifetime the leader asks for. The agent
// enforces its own ceiling on top of this.
const terminalMaxSeconds = 3600

// clientMsg is the JSON control frame the browser sends. Keystrokes travel as
// binary frames instead, so nothing has to be escaped or base64'd.
type clientMsg struct {
	Type string `json:"type"`           // "resize"
	Cols uint32 `json:"cols,omitempty"` // for "resize"
	Rows uint32 `json:"rows,omitempty"`
}

// serverMsg is the JSON control frame the leader sends. Terminal output
// travels as binary frames.
type serverMsg struct {
	Type        string `json:"type"` // "ready" | "closed" | "error"
	SessionID   string `json:"session_id,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	Image       string `json:"image,omitempty"`
	MaxSeconds  int    `json:"max_seconds,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Serve upgrades to a WebSocket and runs one terminal session on one node.
func (d *TerminalDeps) Serve(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	agent, err := d.Registry.RequireAgent(nodeID)
	if err != nil {
		// Answer before the upgrade so the browser gets a readable status
		// instead of a socket that opens and immediately dies.
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}

	cols := uint32(queryInt(r, "cols", 80))
	rows := uint32(queryInt(r, "rows", 24))

	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		slog.Debug("terminal: websocket accept failed", "node", nodeID, "err", err)
		return
	}
	defer ws.CloseNow()
	// A paste is one message; a megabyte of it is already absurd.
	ws.SetReadLimit(1 << 20)

	// Not r.Context(): after the hijack its cancellation no longer tracks the
	// client, and the router's request timeout would kill a healthy session.
	// The WebSocket read failing is what tells us the browser is gone.
	// WithoutCancel keeps the session's values — the audit logger reads the
	// acting user from there, so a plain Background() would file the teardown
	// as if nobody had done it.
	auditCtx := context.WithoutCancel(r.Context())
	ctx, cancel := context.WithCancel(auditCtx)
	defer cancel()

	// Subscribe before opening, or the shell's first prompt races us and is
	// delivered to nobody. That is why the leader assigns the session id.
	sessionID := uuid.NewString()
	sub, unsubscribe := d.Pty.subscribe(sessionID)
	defer unsubscribe()

	openCtx, openCancel := context.WithTimeout(ctx, 30*time.Second)
	res, err := agent.Request(openCtx, &pb.Command{Op: &pb.Command_PtyOpen{PtyOpen: &pb.PtyOpenReq{
		Cols: cols, Rows: rows, MaxSeconds: terminalMaxSeconds, SessionId: sessionID,
	}}})
	openCancel()
	if err != nil {
		writeControl(ctx, ws, serverMsg{Type: "error", Error: err.Error()})
		_ = ws.Close(websocket.StatusInternalError, "open failed")
		return
	}
	opened := res.GetPtyOpen()

	d.Audit.Log(auditCtx, r, "terminal.open", "node", nodeID, map[string]any{
		"session_id": sessionID, "container_id": opened.GetContainerId(),
	})
	slog.Info("terminal: attached", "node", nodeID, "session", sessionID,
		"container", opened.GetContainerId())

	// Whatever ends the session, the agent has to hear about it — otherwise a
	// privileged root container survives until its 1h cap.
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if _, err := agent.Request(closeCtx, &pb.Command{Op: &pb.Command_PtyClose{
			PtyClose: &pb.PtyCloseReq{SessionId: sessionID},
		}}); err != nil {
			slog.Warn("terminal: closing session on agent failed",
				"node", nodeID, "session", sessionID, "err", err)
		}
		d.Audit.Log(auditCtx, r, "terminal.close", "node", nodeID,
			map[string]any{"session_id": sessionID})
	}()

	writeControl(ctx, ws, serverMsg{
		Type: "ready", SessionID: sessionID, ContainerID: opened.GetContainerId(),
		Image: opened.GetImage(), MaxSeconds: terminalMaxSeconds,
	})

	go d.readLoop(ctx, cancel, ws, agent, sessionID)

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-sub.dead:
			writeControl(ctx, ws, serverMsg{Type: "closed", Reason: "terminal output overflow"})
			_ = ws.Close(websocket.StatusPolicyViolation, "output overflow")
			return

		case out := <-sub.out:
			if data := out.GetData(); len(data) > 0 {
				wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
				err := ws.Write(wctx, websocket.MessageBinary, data)
				wcancel()
				if err != nil {
					return
				}
			}
			if out.GetClosed() {
				writeControl(ctx, ws, serverMsg{Type: "closed", Reason: out.GetReason()})
				_ = ws.Close(websocket.StatusNormalClosure, "shell ended")
				return
			}

		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := ws.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

// readLoop forwards browser input to the agent until the socket dies.
func (d *TerminalDeps) readLoop(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, agent *AgentConn, sessionID string) {
	defer cancel()
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return
		}

		var cmd *pb.Command
		switch typ {
		case websocket.MessageBinary:
			cmd = &pb.Command{Op: &pb.Command_PtyData{PtyData: &pb.PtyDataReq{
				SessionId: sessionID, Data: data,
			}}}
		case websocket.MessageText:
			var m clientMsg
			if err := json.Unmarshal(data, &m); err != nil || m.Type != "resize" {
				continue // unknown control frame — ignore rather than kill the shell
			}
			cmd = &pb.Command{Op: &pb.Command_PtyResize{PtyResize: &pb.PtyResizeReq{
				SessionId: sessionID, Cols: m.Cols, Rows: m.Rows,
			}}}
		default:
			continue
		}

		reqCtx, reqCancel := context.WithTimeout(ctx, 15*time.Second)
		_, err = agent.Request(reqCtx, cmd)
		reqCancel()
		if err != nil {
			// The agent rejecting input means the session is gone on its side.
			if !errors.Is(err, context.Canceled) {
				slog.Debug("terminal: input rejected", "session", sessionID, "err", err)
			}
			return
		}
	}
}

func writeControl(ctx context.Context, ws *websocket.Conn, m serverMsg) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = ws.Write(wctx, websocket.MessageText, b)
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

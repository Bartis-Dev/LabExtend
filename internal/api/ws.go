package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	hc "github.com/Bartis-Dev/LabExtend/internal/healthcheck"
	"github.com/coder/websocket"
)

// handleHCStatus returns the most recent status snapshot.
func (s *Server) handleHCStatus(w http.ResponseWriter, _ *http.Request) {
	if s.Hub == nil {
		writeJSON(w, http.StatusOK, hc.StatusMap{})
		return
	}
	writeJSON(w, http.StatusOK, s.Hub.Snapshot())
}

// envelope is the message format pushed over the WebSocket. Reserving a
// type field now means future message types (system metrics, toasts) can
// land without a breaking change.
type wsEnvelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// handleWS upgrades to a WebSocket connection, sends an immediate snapshot,
// and then forwards every Hub publish to the client until either side
// disconnects. Cookie auth is checked at the HTTP layer before the upgrade.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Same-origin only — match the originCheck philosophy.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{r.Host},
	})
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx := r.Context()

	// Initial snapshot.
	if s.Hub != nil {
		writeEnvelope(ctx, c, "hc_update", s.Hub.Snapshot())
	}

	if s.Hub == nil {
		_ = c.Close(websocket.StatusNormalClosure, "")
		return
	}

	sub, cancel := s.Hub.Subscribe()
	defer cancel()

	// Drain client reads in a side goroutine so we notice disconnects.
	go func() {
		_, _, _ = c.Read(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub:
			if !ok {
				return
			}
			if err := writeEnvelope(ctx, c, "hc_update", msg); err != nil {
				return
			}
		}
	}
}

func writeEnvelope(ctx context.Context, c *websocket.Conn, t string, data any) error {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, err := json.Marshal(wsEnvelope{Type: t, Data: data})
	if err != nil {
		return err
	}
	return c.Write(wctx, websocket.MessageText, b)
}

// WSAuthMiddleware wraps the WS upgrade handler with cookie verification.
// It cannot use the standard requireAuth because websocket.Accept needs
// to own ResponseWriter to perform the upgrade.
func (s *Server) wsAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, err := auth.Verify(s.JWTSecret, cookie.Value); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

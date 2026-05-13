// Package api wires the HTTP handlers for LabExtend together.
package api

import (
	"database/sql"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	"github.com/Bartis-Dev/LabExtend/internal/ddns"
	"github.com/Bartis-Dev/LabExtend/internal/docs"
	hc "github.com/Bartis-Dev/LabExtend/internal/healthcheck"
	"github.com/Bartis-Dev/LabExtend/internal/modules"
	"github.com/Bartis-Dev/LabExtend/internal/notes"
	"github.com/Bartis-Dev/LabExtend/internal/settings"
	"github.com/Bartis-Dev/LabExtend/internal/stats"
	"github.com/Bartis-Dev/LabExtend/internal/tlsstore"
	"github.com/Bartis-Dev/LabExtend/internal/vault"
	"github.com/Bartis-Dev/LabExtend/internal/wol"
)

// Server holds every dependency the handlers need. One instance is created
// per process.
type Server struct {
	DB         *sql.DB
	Cfg        config.Config
	Settings   *settings.Store
	Modules    *modules.Store
	Vault      *vault.Store
	DDNS       *ddns.Store
	DDNSWorker *ddns.Worker
	WoL        *wol.Store
	Docs       *docs.Store
	Notes        *notes.Store
	Stats        *stats.Store
	TLS          *tlsstore.Store
	HTTPSStarted bool
	JWTSecret    []byte
	LoginLimit *auth.Limiter
	SetupLimit *auth.Limiter
	Hub        *hc.Hub
	Worker     *hc.Worker
}

// New constructs a Server with sensible rate-limiter defaults:
//   - login: 5 attempts per 5 minutes per IP
//   - setup: 5 attempts per minute globally (setup is a one-shot path)
func New(db *sql.DB, cfg config.Config, st *settings.Store, mods *modules.Store, vlt *vault.Store, dd *ddns.Store, wl *wol.Store, dx *docs.Store, nt *notes.Store, sts *stats.Store, tlsS *tlsstore.Store, jwtSecret []byte) *Server {
	return &Server{
		DB:         db,
		Cfg:        cfg,
		Settings:   st,
		Modules:    mods,
		Vault:      vlt,
		DDNS:       dd,
		WoL:        wl,
		Docs:       dx,
		Notes:      nt,
		Stats:      sts,
		TLS:        tlsS,
		JWTSecret:  jwtSecret,
		LoginLimit: auth.NewLimiter(5, 5*time.Minute),
		SetupLimit: auth.NewLimiter(5, time.Minute),
	}
}

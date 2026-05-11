// Package api wires the HTTP handlers for LabExtend together.
package api

import (
	"database/sql"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
	"github.com/Bartis-Dev/LabExtend/internal/config"
	hc "github.com/Bartis-Dev/LabExtend/internal/healthcheck"
	"github.com/Bartis-Dev/LabExtend/internal/settings"
)

// Server holds every dependency the handlers need. One instance is created
// per process.
type Server struct {
	DB         *sql.DB
	Cfg        config.Config
	Settings   *settings.Store
	JWTSecret  []byte
	LoginLimit *auth.Limiter
	SetupLimit *auth.Limiter
	Hub        *hc.Hub
	Worker     *hc.Worker
}

// New constructs a Server with sensible rate-limiter defaults:
//   - login: 5 attempts per 5 minutes per IP
//   - setup: 5 attempts per minute globally (setup is a one-shot path)
func New(db *sql.DB, cfg config.Config, st *settings.Store, jwtSecret []byte) *Server {
	return &Server{
		DB:         db,
		Cfg:        cfg,
		Settings:   st,
		JWTSecret:  jwtSecret,
		LoginLimit: auth.NewLimiter(5, 5*time.Minute),
		SetupLimit: auth.NewLimiter(5, time.Minute),
	}
}

// Package config loads runtime configuration from environment variables.
// The same Config struct serves both leader and agent roles; the chosen role
// is derived from LEADER and LEADER_ADDR.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Role indicates whether the process should run as leader or agent.
type Role string

const (
	RoleLeader  Role = "leader"
	RoleAgent   Role = "agent"
	RoleInvalid Role = ""
)

// Config holds all runtime settings parsed from the environment.
// TODO(phase 2): expand validation, add auto-generation of secrets on first
// boot when running as leader.
type Config struct {
	// Role-deciding
	LeaderMode bool   // LEADER=true
	LeaderAddr string // agent only — e.g. "labextend-leader:9090"

	// Network
	HTTPAddr    string // leader only
	GRPCAddr    string // leader only
	ExternalURL string // leader only

	// Storage
	DataDir string

	// Session
	SessionCookieName   string
	SessionTTLHours     int
	SessionSecureCookie bool

	// Shared secret between leader and agents (gRPC metadata auth).
	AgentToken string

	// Optional mTLS
	GRPCTLSCert     string
	GRPCTLSKey      string
	GRPCTLSClientCA string

	// At-rest encryption keys (leader only)
	SecretsKey string // 32-byte hex for AES-GCM of S3 creds, webhook URLs
	TOTPKey    string // 32-byte hex for AES-GCM of TOTP secrets
	TOTPIssuer string

	// Feature flags
	AllowExec bool

	// Limits
	FSMaxInlineBytes int64
	UploadMaxBytes   int64

	// Agent
	AgentHostID       string
	AgentLabels       map[string]string
	HeartbeatInterval time.Duration

	// Misc
	LogLevel     string
	CORSOrigins  []string
	DisableAudit bool
}

// Load reads env vars, returning a populated Config or an error.
// For leaders with missing secrets, EnsureLeaderSecrets is called to
// auto-generate and persist them to ${DATA_DIR}/.env.generated, then the
// load is repeated.
func Load() (*Config, error) {
	leaderMode := boolEnv("LEADER", false)
	dataDir := stringEnv("DATA_DIR", "/data")

	// Auto-generate leader secrets BEFORE building the Config so the
	// fresh env values are picked up below.
	if leaderMode {
		if err := EnsureLeaderSecrets(dataDir); err != nil {
			return nil, fmt.Errorf("ensure leader secrets: %w", err)
		}
	}

	c := &Config{
		LeaderMode:          leaderMode,
		LeaderAddr:          os.Getenv("LEADER_ADDR"),
		HTTPAddr:            stringEnv("HTTP_ADDR", ":8080"),
		GRPCAddr:            stringEnv("GRPC_ADDR", ":9090"),
		ExternalURL:         os.Getenv("EXTERNAL_URL"),
		DataDir:             dataDir,
		SessionCookieName:   stringEnv("SESSION_COOKIE_NAME", "bpm_session"),
		SessionTTLHours:     intEnv("SESSION_TTL_HOURS", 168),
		SessionSecureCookie: boolEnv("SESSION_SECURE_COOKIE", true),
		AgentToken:          os.Getenv("BPM_AGENT_TOKEN"),
		GRPCTLSCert:         os.Getenv("BPM_GRPC_TLS_CERT"),
		GRPCTLSKey:          os.Getenv("BPM_GRPC_TLS_KEY"),
		GRPCTLSClientCA:     os.Getenv("BPM_GRPC_TLS_CLIENT_CA"),
		SecretsKey:          os.Getenv("BPM_SECRETS_KEY"),
		TOTPKey:             os.Getenv("BPM_TOTP_KEY"),
		TOTPIssuer:          stringEnv("BPM_TOTP_ISSUER", "labextend"),
		AllowExec:           boolEnv("BPM_ALLOW_EXEC", false),
		FSMaxInlineBytes:    int64Env("BPM_FS_MAX_INLINE_BYTES", 5*1024*1024),
		UploadMaxBytes:      int64Env("BPM_UPLOAD_MAX_BYTES", 2*1024*1024*1024),
		AgentHostID:         os.Getenv("BPM_AGENT_HOST_ID"),
		AgentLabels:         parseLabels(os.Getenv("BPM_AGENT_LABELS")),
		HeartbeatInterval:   durationEnv("BPM_HEARTBEAT_INTERVAL", 5*time.Second),
		LogLevel:            stringEnv("BPM_LOG_LEVEL", "info"),
		CORSOrigins:         splitCSV(os.Getenv("CORS_ORIGINS")),
		DisableAudit:        boolEnv("BPM_DISABLE_AUDIT", false),
	}

	if c.AgentHostID == "" {
		hn, _ := os.Hostname()
		c.AgentHostID = hn
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Role returns the resolved role based on env hints.
func (c *Config) Role() Role {
	if c.LeaderMode {
		return RoleLeader
	}
	if c.LeaderAddr != "" {
		return RoleAgent
	}
	return RoleInvalid
}

func (c *Config) validate() error {
	switch c.Role() {
	case RoleLeader:
		// EnsureLeaderSecrets() in Load() auto-generates these on first boot.
		// If still empty, something went wrong with writing .env.generated.
		if c.AgentToken == "" {
			return errors.New("BPM_AGENT_TOKEN missing (auto-gen failed — check DATA_DIR perms)")
		}
		if c.SecretsKey == "" {
			return errors.New("BPM_SECRETS_KEY missing (auto-gen failed — check DATA_DIR perms)")
		}
		if c.TOTPKey == "" {
			return errors.New("BPM_TOTP_KEY missing (auto-gen failed — check DATA_DIR perms)")
		}
	case RoleAgent:
		if c.AgentToken == "" {
			return errors.New("BPM_AGENT_TOKEN is required for agent (copy from leader's .env.generated)")
		}
	case RoleInvalid:
		return errors.New("set LEADER=true (to run leader) or LEADER_ADDR=host:port (to run agent)")
	}
	return nil
}

// ─── tiny env helpers (kept local so config is self-contained) ──────────────

func stringEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func boolEnv(k string, def bool) bool {
	v := strings.ToLower(os.Getenv(k))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func intEnv(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func int64Env(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func durationEnv(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseLabels(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// String redacts secrets for safe logging.
func (c *Config) String() string {
	return fmt.Sprintf(
		"role=%s leader_addr=%s http=%s grpc=%s data=%s allow_exec=%t labels=%v",
		c.Role(), c.LeaderAddr, c.HTTPAddr, c.GRPCAddr, c.DataDir, c.AllowExec, c.AgentLabels,
	)
}

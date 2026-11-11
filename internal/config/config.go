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
	SessionIdleHours    int // 0 = no idle timeout
	SessionSecureCookie bool

	// Shared secret between leader and agents (gRPC metadata auth).
	AgentToken string

	// Optional mTLS
	GRPCTLSCert     string
	GRPCTLSKey      string
	GRPCTLSClientCA string

	// At-rest encryption keys (leader only)
	SecretsKey string // 32-byte hex for AES-GCM of S3 creds, webhook URLs

	// OIDC login (e.g. Pocket ID), the only way to sign in. Usable only when
	// issuer, client ID and client secret are all set; see OIDCEnabled.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string // BPM_OIDC_CLIENT_SECRET or BPM_OIDC_CLIENT_SECRET_FILE
	OIDCName         string
	// Membership in one of these provider groups is what grants access; the
	// admin group additionally grants admin.
	OIDCUserGroup  string
	OIDCAdminGroup string

	// Feature flags
	AllowExec   bool
	FileBrowser bool // BPM_FILEBROWSER: the leader's file browser routes

	// Swarm service control. Default service force-updated by the "Restart
	// Portainer Agent" button (leader-side, runs against the leader's manager
	// socket). Override if the agent service has a different stack name.
	PortainerService string

	// Agent
	AgentHostID       string
	AgentLabels       map[string]string
	HeartbeatInterval time.Duration

	// Misc
	LogLevel    string
	CORSOrigins []string

	// TrustedProxies lists IPs/CIDRs whose X-Forwarded-For header may be
	// believed. Empty (the default) means no peer is trusted and the TCP peer
	// address is always used — set it to your reverse proxy when you run one,
	// or every client can pick the IP that lands in the audit log and reset its
	// own login rate limit at will.
	TrustedProxies []string

	// Web Analytics (leader only): log lines of containers whose image name
	// starts with AnalyticsImagePrefix are read as Traefik JSON access logs.
	AnalyticsImagePrefix   string
	AnalyticsRetentionDays int
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

	secureCookie := boolEnv("SESSION_SECURE_COOKIE", true)

	oidcSecret, err := fileEnv("BPM_OIDC_CLIENT_SECRET")
	if err != nil {
		return nil, err
	}

	c := &Config{
		LeaderMode:          leaderMode,
		LeaderAddr:          os.Getenv("LEADER_ADDR"),
		HTTPAddr:            stringEnv("HTTP_ADDR", ":8080"),
		GRPCAddr:            stringEnv("GRPC_ADDR", ":9090"),
		ExternalURL:         os.Getenv("EXTERNAL_URL"),
		DataDir:             dataDir,
		SessionCookieName:   stringEnv("SESSION_COOKIE_NAME", defaultSessionCookieName(secureCookie)),
		SessionTTLHours:     intEnv("SESSION_TTL_HOURS", 12),
		SessionIdleHours:    intEnv("SESSION_IDLE_HOURS", 2),
		SessionSecureCookie: secureCookie,
		AgentToken:          os.Getenv("BPM_AGENT_TOKEN"),
		GRPCTLSCert:         os.Getenv("BPM_GRPC_TLS_CERT"),
		GRPCTLSKey:          os.Getenv("BPM_GRPC_TLS_KEY"),
		GRPCTLSClientCA:     os.Getenv("BPM_GRPC_TLS_CLIENT_CA"),
		SecretsKey:          os.Getenv("BPM_SECRETS_KEY"),
		OIDCIssuer:          strings.TrimSpace(os.Getenv("BPM_OIDC_ISSUER")),
		OIDCClientID:        strings.TrimSpace(os.Getenv("BPM_OIDC_CLIENT_ID")),
		OIDCClientSecret:    oidcSecret,
		OIDCName:            stringEnv("BPM_OIDC_NAME", "Pocket ID"),
		OIDCUserGroup:       stringEnv("BPM_OIDC_USER_GROUP", "labextend"),
		OIDCAdminGroup:      stringEnv("BPM_OIDC_ADMIN_GROUP", "labextend-admins"),
		AllowExec:           boolEnv("BPM_ALLOW_EXEC", false),
		FileBrowser:         boolEnv("BPM_FILEBROWSER", false),
		PortainerService:    stringEnv("BPM_PORTAINER_SERVICE", "portainer_agent"),
		AgentHostID:         os.Getenv("BPM_AGENT_HOST_ID"),
		AgentLabels:         parseLabels(os.Getenv("BPM_AGENT_LABELS")),
		HeartbeatInterval:   durationEnv("BPM_HEARTBEAT_INTERVAL", 5*time.Second),
		LogLevel:            stringEnv("BPM_LOG_LEVEL", "info"),
		CORSOrigins:         splitCSV(os.Getenv("CORS_ORIGINS")),
		TrustedProxies:      splitCSV(os.Getenv("BPM_TRUSTED_PROXIES")),

		AnalyticsImagePrefix:   stringEnv("BPM_ANALYTICS_IMAGE_PREFIX", "traefik"),
		AnalyticsRetentionDays: intEnv("BPM_ANALYTICS_RETENTION_DAYS", 400),
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

// defaultSessionCookieName uses the __Host- prefix whenever the cookie is
// Secure: browsers then refuse it unless it is Secure, Path=/ and has no
// Domain, so a sibling subdomain cannot plant or overwrite it. Plain-HTTP
// local development cannot set such a cookie at all.
func defaultSessionCookieName(secure bool) string {
	if secure {
		return "__Host-bpm_session"
	}
	return "bpm_session"
}

// OIDCEnabled reports whether OIDC login is fully configured.
func (c *Config) OIDCEnabled() bool {
	return c.OIDCIssuer != "" && c.OIDCClientID != "" && c.OIDCClientSecret != ""
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

// GRPCMaxMsgBytes is the message limit on both ends of the agent channel.
// grpc-go defaults to 4 MiB on receive — below one log batch after a leader outage (2000 lines of up to 4 KiB) —
// and a message over it does not fail the one call, it kills the whole
// stream: the node drops offline, loses its live state and reconnects.
const GRPCMaxMsgBytes = 64 << 20

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

// fileEnv returns $k, or else the trimmed contents of the file named by
// $k_FILE (the Docker Swarm secret convention).
func fileEnv(k string) (string, error) {
	if v := os.Getenv(k); v != "" {
		return v, nil
	}
	path := os.Getenv(k + "_FILE")
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s_FILE: %w", k, err)
	}
	return strings.TrimSpace(string(b)), nil
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

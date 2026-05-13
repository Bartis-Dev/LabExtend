// Package config parses environment variables into the runtime Config.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration sourced from environment variables.
type Config struct {
	Listen              string
	TLSListen           string
	TLSCertFile         string
	TLSKeyFile          string
	TLSSelfSign         bool
	DataDir             string
	PasswordReset       bool
	SessionTimeout      time.Duration
	HealthcheckInterval time.Duration
	JWTSecret           string
	LogLevel            string
}

// Load reads LABEXTEND_* environment variables and returns a Config with
// defaults filled in for any unset or unparseable values.
func Load() Config {
	return Config{
		Listen:              envDefault("LABEXTEND_LISTEN", "0.0.0.0:10000"),
		TLSListen:           envDefault("LABEXTEND_TLS_LISTEN", "0.0.0.0:10001"),
		TLSCertFile:         os.Getenv("LABEXTEND_TLS_CERT_FILE"),
		TLSKeyFile:          os.Getenv("LABEXTEND_TLS_KEY_FILE"),
		TLSSelfSign:         envBool("LABEXTEND_TLS_SELF_SIGN", false),
		DataDir:             envDefault("LABEXTEND_DATA_DIR", "/data"),
		PasswordReset:       envBool("LABEXTEND_PASSWORD_RESET", false),
		SessionTimeout:      envDuration("LABEXTEND_SESSION_TIMEOUT", 7*24*time.Hour),
		HealthcheckInterval: envDuration("LABEXTEND_HEALTHCHECK_INTERVAL", 60*time.Second),
		JWTSecret:           os.Getenv("LABEXTEND_JWT_SECRET"),
		LogLevel:            envDefault("LABEXTEND_LOG_LEVEL", "info"),
	}
}

var daysRE = regexp.MustCompile(`^(\d+)d$`)

// ParseDuration extends time.ParseDuration with a "Nd" suffix for days,
// e.g. "7d" returns 7*24h. Returns an error for unparseable input.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if m := daysRE.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}

func envDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

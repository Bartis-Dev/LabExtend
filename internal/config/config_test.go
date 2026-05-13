package config

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"7d":   7 * 24 * time.Hour,
		"1d":   24 * time.Hour,
		"30m":  30 * time.Minute,
		"3h":   3 * time.Hour,
		"720h": 720 * time.Hour,
		"500ms": 500 * time.Millisecond,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
	if _, err := ParseDuration("bad"); err == nil {
		t.Error("expected error for 'bad'")
	}
	if _, err := ParseDuration(""); err == nil {
		t.Error("expected error for empty string")
	}
}

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{
		"LABEXTEND_LISTEN", "LABEXTEND_DATA_DIR", "LABEXTEND_PASSWORD_RESET",
		"LABEXTEND_SESSION_TIMEOUT", "LABEXTEND_HEALTHCHECK_INTERVAL",
		"LABEXTEND_JWT_SECRET", "LABEXTEND_LOG_LEVEL",
	} {
		t.Setenv(k, "")
	}
	c := Load()
	if c.Listen != "0.0.0.0:10000" {
		t.Errorf("default Listen = %q", c.Listen)
	}
	if c.TLSListen != "0.0.0.0:10001" {
		t.Errorf("default TLSListen = %q", c.TLSListen)
	}
	if c.DataDir != "/data" {
		t.Errorf("default DataDir = %q", c.DataDir)
	}
	if c.SessionTimeout != 7*24*time.Hour {
		t.Errorf("default SessionTimeout = %v", c.SessionTimeout)
	}
	if c.HealthcheckInterval != 60*time.Second {
		t.Errorf("default HealthcheckInterval = %v", c.HealthcheckInterval)
	}
	if c.PasswordReset {
		t.Error("default PasswordReset should be false")
	}
	if c.LogLevel != "info" {
		t.Errorf("default LogLevel = %q", c.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("LABEXTEND_LISTEN", "127.0.0.1:9000")
	t.Setenv("LABEXTEND_SESSION_TIMEOUT", "3h")
	t.Setenv("LABEXTEND_PASSWORD_RESET", "true")
	c := Load()
	if c.Listen != "127.0.0.1:9000" {
		t.Errorf("Listen = %q", c.Listen)
	}
	if c.SessionTimeout != 3*time.Hour {
		t.Errorf("SessionTimeout = %v", c.SessionTimeout)
	}
	if !c.PasswordReset {
		t.Error("PasswordReset should be true")
	}
}

func TestEnvBoolAccepts(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "True", "1", "yes", "YES"} {
		t.Setenv("LABEXTEND_PASSWORD_RESET", v)
		if !Load().PasswordReset {
			t.Errorf("envBool(%q) should be true", v)
		}
	}
	for _, v := range []string{"false", "0", "no", "anything"} {
		t.Setenv("LABEXTEND_PASSWORD_RESET", v)
		if Load().PasswordReset {
			t.Errorf("envBool(%q) should be false", v)
		}
	}
}

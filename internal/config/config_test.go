package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// validKey is 32 bytes, the AES-256 key length Load insists on.
var validKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/flowgrid")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("CREDENTIAL_KEY", validKey)
}

func TestLoadAppliesDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8080 {
		t.Fatalf("Port %d, want 8080", cfg.Port)
	}
	if cfg.PublicBaseURL != "http://localhost:3000" {
		t.Fatalf("PublicBaseURL %q", cfg.PublicBaseURL)
	}
	if cfg.WorkerConcurrency != 4 {
		t.Fatalf("WorkerConcurrency %d, want 4", cfg.WorkerConcurrency)
	}
	if cfg.ExecutionTimeout != 5*time.Minute {
		t.Fatalf("ExecutionTimeout %v, want 5m", cfg.ExecutionTimeout)
	}
}

func TestLoadNamesEveryMissingVariableAtOnce(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("CREDENTIAL_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected an error")
	}
	// Listing them all together saves three restart-and-retry cycles.
	for _, want := range []string{"DATABASE_URL", "JWT_SECRET", "CREDENTIAL_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadRejectsAShortCredentialKey(t *testing.T) {
	setRequired(t)
	t.Setenv("CREDENTIAL_KEY", base64.StdEncoding.EncodeToString([]byte("too short")))

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("error %v, want one naming the required length", err)
	}
}

func TestLoadRejectsANonBase64CredentialKey(t *testing.T) {
	setRequired(t)
	t.Setenv("CREDENTIAL_KEY", "not base64!!")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("error %v, want one naming the encoding", err)
	}
}

func TestLoadRejectsZeroWorkerConcurrency(t *testing.T) {
	setRequired(t)
	t.Setenv("WORKER_CONCURRENCY", "0")

	if _, err := Load(); err == nil {
		t.Fatal("a worker that claims nothing would look like a hung queue")
	}
}

func TestLoadIgnoresUnparsableNumbersAndDurations(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "http")
	t.Setenv("EXECUTION_TIMEOUT", "a while")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Falling back beats refusing to boot over a typo in an optional variable.
	if cfg.Port != 8080 || cfg.ExecutionTimeout != 5*time.Minute {
		t.Fatalf("expected defaults, got port %d and timeout %v", cfg.Port, cfg.ExecutionTimeout)
	}
}

func TestPublicBaseURLLosesItsTrailingSlash(t *testing.T) {
	setRequired(t)
	t.Setenv("PUBLIC_BASE_URL", "https://flow.acme.vn/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PublicBaseURL != "https://flow.acme.vn" {
		t.Fatalf("PublicBaseURL %q still has its slash", cfg.PublicBaseURL)
	}
	// Otherwise every rendered webhook URL would carry a double slash.
	if got := cfg.WebhookURL("/lead-in"); got != "https://flow.acme.vn/webhook/lead-in" {
		t.Fatalf("WebhookURL %q", got)
	}
}

func TestAddr(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "9000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr() != ":9000" {
		t.Fatalf("Addr %q", cfg.Addr())
	}
}

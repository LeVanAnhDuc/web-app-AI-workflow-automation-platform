// Package config reads the process environment once and fails loudly when a
// required value is missing, so a misconfigured deployment dies at boot rather
// than at the first request.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the whole configuration surface of both binaries.
type Config struct {
	DatabaseURL       string
	Port              int
	JWTSecret         []byte
	CredentialKey     []byte
	PublicBaseURL     string
	WorkerConcurrency int
	ExecutionTimeout  time.Duration
	SeedEmail         string
	SeedPassword      string
	LogLevel          string

	// AnthropicAPIKey enables the AI nodes. It is deliberately optional: a
	// deployment with no key still runs every other node, and those nodes report
	// the missing key rather than failing obscurely.
	//
	// Phase 3 moves per-workspace keys into the credential vault; until then one
	// key serves the whole single-tenant deployment.
	AnthropicAPIKey string
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	var missing []string

	cfg := Config{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		Port:              envInt("PORT", 8080),
		PublicBaseURL:     envString("PUBLIC_BASE_URL", "http://localhost:3000"),
		WorkerConcurrency: envInt("WORKER_CONCURRENCY", 4),
		ExecutionTimeout:  envDuration("EXECUTION_TIMEOUT", 5*time.Minute),
		AnthropicAPIKey:   os.Getenv("ANTHROPIC_API_KEY"),
		SeedEmail:         os.Getenv("SEED_EMAIL"),
		SeedPassword:      os.Getenv("SEED_PASSWORD"),
		LogLevel:          envString("LOG_LEVEL", "info"),
	}

	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	cfg.JWTSecret = []byte(secret)

	if raw := os.Getenv("CREDENTIAL_KEY"); raw == "" {
		missing = append(missing, "CREDENTIAL_KEY")
	} else {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
		if err != nil {
			return cfg, fmt.Errorf("CREDENTIAL_KEY is not valid base64: %w", err)
		}
		if len(key) != 32 {
			return cfg, fmt.Errorf("CREDENTIAL_KEY must decode to 32 bytes, got %d", len(key))
		}
		cfg.CredentialKey = key
	}

	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if cfg.WorkerConcurrency < 1 {
		return cfg, errors.New("WORKER_CONCURRENCY must be at least 1")
	}
	cfg.PublicBaseURL = strings.TrimRight(cfg.PublicBaseURL, "/")
	return cfg, nil
}

// AIEnabled reports whether the AI nodes have a provider to call.
func (c Config) AIEnabled() bool { return strings.TrimSpace(c.AnthropicAPIKey) != "" }

// Addr is the listen address for the API server.
func (c Config) Addr() string { return fmt.Sprintf(":%d", c.Port) }

// WebhookURL renders the public URL a webhook trigger listens on.
func (c Config) WebhookURL(path string) string {
	return c.PublicBaseURL + "/webhook/" + strings.TrimPrefix(path, "/")
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

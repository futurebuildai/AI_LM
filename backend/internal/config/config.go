package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for the AI_LM service. Values are
// sourced from environment variables with a godotenv fallback for local dev.
type Config struct {
	Port        string
	DatabaseURL string

	// Auth & Security — same convention as GableLBM-main / GableRun.
	// AuthMode "dev" disables JWT auth for local development; otherwise a token
	// verifier is required (fail-closed): SESSION_SECRET for AI_LM-issued staff
	// session tokens, and/or JWKS_URL for externally-issued tokens.
	AuthMode   string
	JWKSURL    string
	AuthIssuer string
	// SessionSecret signs/verifies AI_LM staff session JWTs (internal/auth +
	// pkg/middleware). Required when AUTH_MODE != "dev".
	SessionSecret string

	// GableLBM integration — AI_LM is a standalone service that pulls its
	// source-of-truth data (orders, products, vehicles, deliveries) from the
	// GableLBM ERP via /api/integration/* and writes approved routes back.
	GableAPIURL         string // e.g. http://localhost:8080
	GableIntegrationKey string // sent as X-Integration-Key

	// OpenRouteService (pillar 6: real OSS routing). When ORSAPIKey is set the
	// routing optimizer uses ORS's real road distance/duration matrix
	// (driving-hgv) instead of the haversine heuristic. Empty key ⇒ haversine
	// fallback (the service still runs, never hard-fails).
	ORSAPIKey  string // ORS_API_KEY
	ORSBaseURL string // ORS_BASE_URL (default https://api.openrouteservice.org)
	ORSProfile string // ORS_PROFILE (default driving-hgv — heavy lumber trucks)

	// OpenRouter LLM (pillar 6: single-key OSS inference). An OpenAI-compatible
	// chat client pointed at OpenRouter, defaulting to an open-weight model.
	// Empty key ⇒ AI features report "not configured" and degrade gracefully.
	OpenRouterAPIKey  string // OPENROUTER_API_KEY
	OpenRouterBaseURL string // OPENROUTER_BASE_URL (default https://openrouter.ai/api/v1)
	OpenRouterModel   string // OPENROUTER_MODEL (default an open-weight model id)

	// Logging
	LogLevel string // DEBUG, INFO, WARN, ERROR (default: INFO)

	// Database pool sizing (defaults mirror GableRun's PRR-tuned values).
	DBMaxConns        int32
	DBMinConns        int32
	DBMaxConnLifetime int // minutes
}

func Load() (*Config, error) {
	_ = godotenv.Load() // Load .env if present; ignore if not.

	cfg := &Config{
		Port:        getEnv("PORT", "8090"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://gable_user:gable_password@localhost:5434/ai_lm_db?sslmode=disable"),

		AuthMode:      getEnv("AUTH_MODE", ""),
		JWKSURL:       getEnv("JWKS_URL", ""),
		AuthIssuer:    getEnv("AUTH_ISSUER", ""),
		SessionSecret: getEnv("SESSION_SECRET", ""),

		GableAPIURL:         getEnv("GABLE_API_URL", "http://localhost:8080"),
		GableIntegrationKey: getEnv("GABLE_INTEGRATION_KEY", ""),

		ORSAPIKey:  getEnv("ORS_API_KEY", ""),
		ORSBaseURL: getEnv("ORS_BASE_URL", "https://api.openrouteservice.org"),
		ORSProfile: getEnv("ORS_PROFILE", "driving-hgv"),

		OpenRouterAPIKey:  getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterBaseURL: getEnv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
		OpenRouterModel:   getEnv("OPENROUTER_MODEL", "meta-llama/llama-3.3-70b-instruct"),

		LogLevel: getEnv("LOG_LEVEL", "INFO"),

		DBMaxConns:        int32(getEnvInt("DB_MAX_CONNS", 25)),
		DBMinConns:        int32(getEnvInt("DB_MIN_CONNS", 2)),
		DBMaxConnLifetime: getEnvInt("DB_MAX_CONN_LIFETIME_MIN", 60),
	}

	// In non-dev mode, DATABASE_URL must be explicitly set — the localhost
	// fallback is only safe for local development.
	if cfg.AuthMode != "dev" {
		if _, explicit := os.LookupEnv("DATABASE_URL"); !explicit {
			return nil, fmt.Errorf("FATAL: DATABASE_URL must be explicitly set when AUTH_MODE != 'dev' (refusing to fall back to localhost)")
		}
		// DigitalOcean managed-DB bindings (${db.DATABASE_URL}) connect over TLS
		// but omit an explicit sslmode query param. Rather than fail closed,
		// enforce it by appending sslmode=require when no sslmode is present.
		// If an sslmode IS present but is insecure, that's a deliberate
		// misconfiguration and we still refuse.
		if !strings.Contains(cfg.DatabaseURL, "sslmode=") {
			sep := "?"
			if strings.Contains(cfg.DatabaseURL, "?") {
				sep = "&"
			}
			cfg.DatabaseURL += sep + "sslmode=require"
		} else if !strings.Contains(cfg.DatabaseURL, "sslmode=require") &&
			!strings.Contains(cfg.DatabaseURL, "sslmode=verify-full") &&
			!strings.Contains(cfg.DatabaseURL, "sslmode=verify-ca") {
			return nil, fmt.Errorf("FATAL: DATABASE_URL has an insecure sslmode; require/verify-full/verify-ca needed when AUTH_MODE != 'dev'")
		}
		// AI_LM now mints its own staff session tokens (internal/auth), so
		// SESSION_SECRET is the required verifier in production. JWKS_URL is
		// optional and only needed to additionally accept externally-issued
		// (shared GableLBM JWKS) tokens.
		if cfg.SessionSecret == "" {
			return nil, fmt.Errorf("FATAL: SESSION_SECRET must be set when AUTH_MODE != 'dev' (signs AI_LM staff session tokens)")
		}
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		n, err := strconv.Atoi(value)
		if err != nil {
			slog.Warn("Invalid integer env var, using default", "key", key, "value", value, "default", fallback)
			return fallback
		}
		return n
	}
	return fallback
}

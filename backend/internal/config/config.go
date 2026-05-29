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
	// AuthMode "dev" disables JWT auth for local development; otherwise
	// JWKS_URL is required (fail-closed).
	AuthMode   string
	JWKSURL    string
	AuthIssuer string

	// GableLBM integration — AI_LM is a standalone service that pulls its
	// source-of-truth data (orders, products, vehicles, deliveries) from the
	// GableLBM ERP via /api/integration/* and writes approved routes back.
	GableAPIURL         string // e.g. http://localhost:8080
	GableIntegrationKey string // sent as X-Integration-Key

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

		AuthMode:   getEnv("AUTH_MODE", ""),
		JWKSURL:    getEnv("JWKS_URL", ""),
		AuthIssuer: getEnv("AUTH_ISSUER", ""),

		GableAPIURL:         getEnv("GABLE_API_URL", "http://localhost:8080"),
		GableIntegrationKey: getEnv("GABLE_INTEGRATION_KEY", ""),

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
		if !strings.Contains(cfg.DatabaseURL, "sslmode=require") &&
			!strings.Contains(cfg.DatabaseURL, "sslmode=verify-full") &&
			!strings.Contains(cfg.DatabaseURL, "sslmode=verify-ca") {
			return nil, fmt.Errorf("FATAL: DATABASE_URL must include sslmode=require or sslmode=verify-full when AUTH_MODE != 'dev'")
		}
		if cfg.JWKSURL == "" {
			return nil, fmt.Errorf("FATAL: JWKS_URL must be set when AUTH_MODE != 'dev'")
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

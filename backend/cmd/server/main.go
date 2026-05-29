package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/futurebuildai/ai-lm/internal/catalog"
	"github.com/futurebuildai/ai-lm/internal/compliance"
	"github.com/futurebuildai/ai-lm/internal/config"
	"github.com/futurebuildai/ai-lm/internal/fleet"
	"github.com/futurebuildai/ai-lm/internal/gable"
	"github.com/futurebuildai/ai-lm/internal/load"
	"github.com/futurebuildai/ai-lm/internal/routing"
	"github.com/futurebuildai/ai-lm/pkg/database"
	"github.com/futurebuildai/ai-lm/pkg/metrics"
	"github.com/futurebuildai/ai-lm/pkg/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	startTime := time.Now()

	// 1. Structured logging (JSON) with configurable level.
	logLevel := new(slog.LevelVar)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// 2. Config.
	cfg, err := config.Load()
	if err != nil {
		logger.Error("Configuration error", "error", err)
		os.Exit(1)
	}
	switch strings.ToUpper(cfg.LogLevel) {
	case "DEBUG":
		logLevel.Set(slog.LevelDebug)
	case "WARN":
		logLevel.Set(slog.LevelWarn)
	case "ERROR":
		logLevel.Set(slog.LevelError)
	default:
		logLevel.Set(slog.LevelInfo)
	}

	if !strings.EqualFold(cfg.AuthMode, "dev") && os.Getenv("CORS_ORIGINS") == "" {
		logger.Error("CORS_ORIGINS not set and AUTH_MODE != dev; set CORS_ORIGINS for production or AUTH_MODE=dev for development")
		os.Exit(1)
	}

	logger.Info("Starting AI_LM server...", "port", cfg.Port, "auth_mode", cfg.AuthMode, "log_level", cfg.LogLevel)

	// 3. Database.
	db, err := database.Connect(cfg.DatabaseURL, database.PoolConfig{
		MaxConns:          cfg.DBMaxConns,
		MinConns:          cfg.DBMinConns,
		MaxConnLifetime:   time.Duration(cfg.DBMaxConnLifetime) * time.Minute,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: 1 * time.Minute,
	})
	if err != nil {
		logger.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	logger.Info("Connected to database")

	// 3b. Prometheus metrics.
	metrics.Register()
	metricsCtx, metricsCancel := context.WithCancel(context.Background())
	defer metricsCancel()
	metrics.StartDBPoolCollector(metricsCtx, db.Pool, 15*time.Second)
	logger.Info("Prometheus metrics initialized")

	// 4. Auth middleware — fail-closed: JWKS_URL required unless AUTH_MODE=dev.
	var authMw *middleware.AuthMiddleware
	if cfg.JWKSURL != "" {
		logger.Info("Initializing Auth Middleware", "jwks_url", cfg.JWKSURL)
		am, err := middleware.NewAuthMiddleware(context.Background(), middleware.AuthConfig{
			JWKSURL:     cfg.JWKSURL,
			Issuer:      cfg.AuthIssuer,
			PublicPaths: []string{"/health", "/healthz/live", "/healthz/ready", "/metrics"},
		}, logger)
		if err != nil {
			logger.Error("Failed to initialize Auth Middleware", "error", err)
			os.Exit(1)
		}
		authMw = am
	} else if strings.EqualFold(cfg.AuthMode, "dev") {
		logger.Warn("AUTH_MODE=dev: authentication disabled (development only)")
	} else {
		logger.Error("JWKS_URL not set and AUTH_MODE != dev; set JWKS_URL for production or AUTH_MODE=dev for development")
		os.Exit(1)
	}

	// 5. GableLBM integration client.
	gableClient := gable.NewClient(cfg.GableAPIURL, cfg.GableIntegrationKey)
	logger.Info("GableLBM integration client initialized", "base_url", cfg.GableAPIURL)

	// 6. Router & modules.
	mux := http.NewServeMux()

	writeGuard := middleware.RequireRole("admin", "owner", "dispatcher", "yard")

	// Fleet (vehicle profiles).
	fleetSvc := fleet.NewService(fleet.NewRepository(db))
	fleet.NewHandler(fleetSvc).RegisterRoutes(mux, writeGuard)

	// Catalog (product dimensions).
	catalogSvc := catalog.NewService(catalog.NewRepository(db))
	catalog.NewHandler(catalogSvc).RegisterRoutes(mux, writeGuard)

	// Load optimization (pillar 1).
	loadSvc := load.NewService(load.NewRepository(db), fleetSvc, load.NewShelfSolver())
	load.NewHandler(loadSvc).RegisterRoutes(mux, writeGuard)

	// Routing (pillar 2).
	routingSvc := routing.NewService(routing.NewRepository(db), gableClient, gableClient)
	routing.NewHandler(routingSvc).RegisterRoutes(mux, writeGuard)

	// Compliance (pillar 2).
	complianceSvc := compliance.NewService(compliance.NewRepository(db))
	compliance.NewHandler(complianceSvc).RegisterRoutes(mux, writeGuard)

	// Health — liveness.
	mux.HandleFunc("GET /healthz/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Health — readiness (checks DB).
	mux.HandleFunc("GET /healthz/ready", func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		httpStatus := http.StatusOK
		dbStatus := "connected"
		if err := db.Pool.Ping(r.Context()); err != nil {
			status = "degraded"
			httpStatus = http.StatusServiceUnavailable
			dbStatus = "disconnected"
			logger.Error("Readiness check failed", "error", err)
		}
		poolStat := db.Pool.Stat()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": status,
			"uptime": time.Since(startTime).String(),
			"checks": map[string]interface{}{
				"database": map[string]interface{}{
					"status":      dbStatus,
					"pool_total":  poolStat.TotalConns(),
					"pool_idle":   poolStat.IdleConns(),
					"pool_in_use": poolStat.AcquiredConns(),
					"pool_max":    poolStat.MaxConns(),
				},
			},
		})
	})

	// Legacy /health endpoint.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		dbStatus := "connected"
		if err := db.Pool.Ping(r.Context()); err != nil {
			status = "error"
			dbStatus = "disconnected"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": status, "db": dbStatus})
	})

	// Prometheus scrape target.
	mux.Handle("GET /metrics", promhttp.Handler())

	// 7. Middleware chain (outermost first when reading bottom-up).
	var finalHandler http.Handler = mux
	if authMw != nil {
		finalHandler = authMw.Handler(finalHandler)
	}
	finalHandler = middleware.CORSMiddleware(finalHandler)
	finalHandler = middleware.Recovery(logger)(finalHandler)
	finalHandler = middleware.RequestID(finalHandler)
	finalHandler = metrics.HTTPMetrics(finalHandler)

	// 8. Server with graceful shutdown.
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           finalHandler,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()
	logger.Info("AI_LM server listening", "addr", srv.Addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("Shutdown signal received", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	logger.Info("Shutdown step 1/3: draining HTTP connections...")
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("HTTP server forced to shutdown", "error", err)
		os.Exit(1)
	}
	logger.Info("Shutdown step 2/3: stopping metrics collector...")
	metricsCancel()
	logger.Info("Shutdown step 3/3: closing database pool...")
	db.Close()
	logger.Info("Server exiting — clean shutdown complete")
}

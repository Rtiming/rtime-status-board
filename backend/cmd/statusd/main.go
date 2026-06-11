package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"rtime-status-board/backend/internal/app"
)

func main() {
	var configPath string
	var checkConfig bool
	flag.StringVar(&configPath, "config", env("STATUS_BOARD_CONFIG", "../config/status-board.yaml"), "status board YAML config")
	flag.BoolVar(&checkConfig, "check-config", false, "validate config and exit")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := app.LoadConfig(configPath)
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("validate config", "error", err)
		os.Exit(1)
	}
	if checkConfig {
		fmt.Printf("config ok: %s (%d nodes, %d projects, %d services)\n", configPath, len(cfg.Nodes), len(cfg.Projects), len(cfg.Services))
		return
	}

	dbPath := env("STATUS_BOARD_DB_PATH", "./data/status-board.db")
	store, err := app.OpenStore(dbPath)
	if err != nil {
		logger.Error("open store", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	metricsRetention, err := parseDurationWithDays(env("STATUS_BOARD_METRICS_RETENTION", "30d"))
	if err != nil {
		logger.Error("parse metrics retention", "error", err)
		os.Exit(1)
	}
	store.SetMetricsRetention(metricsRetention)

	gatusURL := env("STATUS_BOARD_GATUS_URL", "http://127.0.0.1:23181")
	cacheTTL, err := time.ParseDuration(env("STATUS_BOARD_CACHE_TTL", "10s"))
	if err != nil {
		logger.Error("parse cache ttl", "error", err)
		os.Exit(1)
	}

	frontendDir := env("STATUS_BOARD_FRONTEND_DIR", "")
	listenAddr := env("STATUS_BOARD_LISTEN_ADDR", ":8080")
	aggregator := app.NewAggregatorWithRuntime(cfg, store, app.NewGatusClient(gatusURL), cacheTTL, app.RuntimeSettings{
		DeploymentMode:   env("STATUS_BOARD_DEPLOYMENT_MODE", "development"),
		ConfigPath:       configPath,
		DBPath:           dbPath,
		GatusURL:         gatusURL,
		ListenAddr:       listenAddr,
		FrontendDir:      frontendDir,
		CacheTTL:         cacheTTL,
		MetricsRetention: metricsRetention,
		PublicDomain:     env("STATUS_BOARD_PUBLIC_DOMAIN", ""),
		PublicIP:         env("STATUS_BOARD_PUBLIC_IP", ""),
		TailnetStatusURL: env("STATUS_BOARD_TAILNET_URL", ""),
		BuildCommit:      env("STATUS_BOARD_BUILD_COMMIT", ""),
		BuildTime:        env("STATUS_BOARD_BUILD_TIME", ""),
	})
	server := app.NewServer(app.ServerOptions{
		Config:         cfg,
		Store:          store,
		Aggregator:     aggregator,
		FrontendDir:    frontendDir,
		HeartbeatToken: env("STATUS_BOARD_HEARTBEAT_TOKEN", ""),
		AgentToken:     env("STATUS_BOARD_AGENT_TOKEN", ""),
		Auth: app.AuthOptions{
			CookieName:   env("STATUS_BOARD_AUTH_COOKIE_NAME", ""),
			CookieSecret: env("STATUS_BOARD_AUTH_COOKIE_SECRET", ""),
			HtpasswdPath: env("STATUS_BOARD_AUTH_HTPASSWD", ""),
			SessionTTL:   parseOptionalDuration(env("STATUS_BOARD_AUTH_SESSION_TTL", "")),
		},
		Logger: logger,
	})

	httpServer := &http.Server{
		Addr:              listenAddr,
		Handler:           server.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		logger.Info("status board listening", "addr", listenAddr, "gatus", gatusURL)
		errs <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errs:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("shutdown", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseOptionalDuration(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	duration, err := parseDurationWithDays(value)
	if err != nil {
		return 0
	}
	return duration
}

func parseDurationWithDays(value string) (time.Duration, error) {
	normalized := strings.TrimSpace(strings.ToLower(value))
	if strings.HasSuffix(normalized, "d") {
		hours, err := time.ParseDuration(strings.TrimSuffix(normalized, "d") + "h")
		if err != nil {
			return 0, err
		}
		return hours * 24, nil
	}
	return time.ParseDuration(normalized)
}

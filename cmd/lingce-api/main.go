package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/freeasyman/lingce-api/internal/auth"
	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/store"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/freeasyman/lingce-api/pkg/sms"
)

const version = "1.0.0"

func main() {
	// Setup logger
	setupLogger()

	slog.Info("starting lingce-api", "version", version)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Create database connection pool
	ctx := context.Background()
	pool, err := store.NewPostgresPool(ctx, cfg.Database.URL)
	if err != nil {
		slog.Error("failed to create database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Setup HTTP router
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteSuccess(w, map[string]string{
			"status":  "ok",
			"version": version,
		})
	})

	// Register module routes
	authStore := auth.NewStore(pool)
	smsClient := sms.NewAliyunClient(
		cfg.Aliyun.AccessKeyID,
		cfg.Aliyun.AccessKeySecret,
		cfg.Aliyun.SMSSignName,
		cfg.Aliyun.SMSTemplateCode,
	)
	authService := auth.NewService(authStore, smsClient, cfg.JWT.Secret, cfg.JWT.ExpiryHours)
	authHandler := auth.NewHandler(authService)
	authHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// TODO: Register other modules
	// organization.RegisterRoutes(mux, orgSvc, mw)
	// ...

	// Apply middleware chain
	handler := middleware.RequestID(
		middleware.Logger(
			middleware.Recovery(
				middleware.CORS(mux),
			),
		),
	)

	// Create HTTP server
	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		slog.Info("server listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	slog.Info("server stopped")
}

func setupLogger() {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	slog.SetDefault(slog.New(handler))
}

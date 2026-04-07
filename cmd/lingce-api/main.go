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
	"github.com/freeasyman/lingce-api/internal/badge"
	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/customer"
	"github.com/freeasyman/lingce-api/internal/department"
	"github.com/freeasyman/lingce-api/internal/employee"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/organization"
	"github.com/freeasyman/lingce-api/internal/rbac"
	"github.com/freeasyman/lingce-api/internal/recording"
	"github.com/freeasyman/lingce-api/internal/store"
	"github.com/freeasyman/lingce-api/internal/support"
	"github.com/freeasyman/lingce-api/internal/sysconfig"
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

	// Register organization module
	orgStore := organization.NewStore(pool)
	orgService := organization.NewService(orgStore)
	orgHandler := organization.NewHandler(orgService)
	orgHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register department module
	deptStore := department.NewStore(pool)
	deptService := department.NewService(deptStore)
	deptHandler := department.NewHandler(deptService)
	deptHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register employee module
	empStore := employee.NewStore(pool)
	empService := employee.NewService(empStore)
	empHandler := employee.NewHandler(empService)
	empHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register medical recording module
	recStore := recording.NewStore(pool)
	recService := recording.NewService(recStore)
	recHandler := recording.NewHandler(recService)
	recHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register sysconfig module
	sysconfigStore := sysconfig.NewStore(pool)
	sysconfigService := sysconfig.NewService(sysconfigStore)
	sysconfigHandler := sysconfig.NewHandler(sysconfigService)
	sysconfigHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register rbac module
	rbacStore := rbac.NewStore(pool)
	rbacService := rbac.NewService(rbacStore)
	rbacHandler := rbac.NewHandler(rbacService)
	rbacHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register support module
	supportStore := support.NewStore(pool)
	supportService := support.NewService(supportStore)
	supportHandler := support.NewHandler(supportService)
	supportHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register customer module
	customerStore := customer.NewStore(pool)
	customerService := customer.NewService(customerStore)
	customerHandler := customer.NewHandler(customerService)
	customerHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register badge module
	badgeStore := badge.NewStore(pool)
	badgeService := badge.NewService(badgeStore)
	badgeHandler := badge.NewHandler(badgeService)
	badgeHandler.RegisterRoutes(mux, cfg.JWT.Secret)

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

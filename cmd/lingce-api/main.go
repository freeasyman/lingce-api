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
	"github.com/freeasyman/lingce-api/internal/content"
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
	"github.com/freeasyman/lingce-api/internal/tenant"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/freeasyman/lingce-api/pkg/oss"
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

	if err := store.ApplyCompatMigrations(ctx, pool); err != nil {
		slog.Error("failed to apply compatibility migrations", "error", err)
		os.Exit(1)
	}

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
	authHandler.RegisterRoutes(mux, cfg.JWT.Secret, pool)

	// Register tenant module (new)
	tenantStore := tenant.NewStore(pool)

	// Register organization module
	orgStore := organization.NewStore(pool, tenantStore)
	orgService := organization.NewService(orgStore, tenantStore)
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
	recService := recording.NewService(recStore, cfg.External.RecordingWorkerURL, cfg.External.RecordingWorkerToken)
	recHandler := recording.NewHandler(recService)
	recHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register sysconfig module
	sysconfigStore := sysconfig.NewStore(pool, tenantStore)
	sysconfigService := sysconfig.NewService(sysconfigStore, tenantStore)
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
	badgeService := badge.NewServiceWithMiddleware(
		badgeStore,
		cfg.External.BadgeMiddlewareURL,
		cfg.External.BadgeMiddlewareToken,
	)
	badgeHandler := badge.NewHandler(badgeService)
	badgeHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Create LLM gateway client
	llmClient := llmgateway.NewClient(cfg.External.LLMGatewayURL, cfg.External.LLMGatewayAPIKey)

	// Create OSS client
	var ossClient *oss.Client
	if cfg.Aliyun.OSSEndpoint != "" && cfg.Aliyun.OSSBucket != "" {
		var err error
		ossClient, err = oss.NewClient(
			cfg.Aliyun.OSSEndpoint,
			cfg.Aliyun.AccessKeyID,
			cfg.Aliyun.AccessKeySecret,
			cfg.Aliyun.OSSBucket,
		)
		if err != nil {
			slog.Warn("failed to create OSS client", "error", err)
		}
	}

	// Register content module
	contentStore := content.NewStore(pool)
	contentService := content.NewService(contentStore, llmClient, ossClient)
	contentHandler := content.NewHandler(contentService)
	contentHandler.RegisterRoutes(mux, cfg.JWT.Secret)

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

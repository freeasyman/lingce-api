package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/freeasyman/lingce-api/internal/auth"
	"github.com/freeasyman/lingce-api/internal/badge"
	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/content"
	"github.com/freeasyman/lingce-api/internal/customer"
	"github.com/freeasyman/lingce-api/internal/dashboard"
	"github.com/freeasyman/lingce-api/internal/department"
	"github.com/freeasyman/lingce-api/internal/employee"
	"github.com/freeasyman/lingce-api/internal/knowledge"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/organization"
	"github.com/freeasyman/lingce-api/internal/rbac"
	"github.com/freeasyman/lingce-api/internal/recording"
	"github.com/freeasyman/lingce-api/internal/sandbox"
	"github.com/freeasyman/lingce-api/internal/store"
	"github.com/freeasyman/lingce-api/internal/support"
	"github.com/freeasyman/lingce-api/internal/sysconfig"
	"github.com/freeasyman/lingce-api/internal/tenant"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/freeasyman/lingce-api/pkg/oss"
	"github.com/freeasyman/lingce-api/pkg/sms"
)

var (
	version   = "1.0.2"
	gitSHA    = "unknown"
	buildTime = "unknown"
)

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
	middleware.SetAuthValidationPool(pool)

	if err := store.ApplyCompatMigrations(ctx, pool); err != nil {
		slog.Error("failed to apply compatibility migrations", "error", err)
		os.Exit(1)
	}

	// Setup HTTP router
	mux := http.NewServeMux()

	// Health check endpoint
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteSuccess(w, map[string]string{
			"status":  "ok",
			"version": version,
		})
	}
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /api/v1/healthz", healthHandler)

	// Version endpoint
	versionHandler := func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteSuccess(w, map[string]string{
			"service":    "lingce-api",
			"version":    version,
			"git_sha":    gitSHA,
			"build_time": buildTime,
		})
	}
	mux.HandleFunc("GET /version", versionHandler)
	mux.HandleFunc("GET /api/v1/version", versionHandler)

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

	// Register tenant/sysconfig modules
	tenantStore := tenant.NewStore(pool)
	sysconfigStore := sysconfig.NewStore(pool)
	sysconfigService := sysconfig.NewService(sysconfigStore)

	tenantService := tenant.NewService(tenantStore, sysconfigService)
	tenantHandler := tenant.NewHandler(tenantService)
	tenantHandler.RegisterRoutes(mux, cfg.JWT.Secret)

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

	// Register knowledge module
	kbStore := knowledge.NewStore(pool)
	kbService := knowledge.NewService(kbStore)
	kbHandler := knowledge.NewHandler(kbService)
	kbHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register employee module
	empStore := employee.NewStore(pool)
	empService := employee.NewService(empStore)
	empHandler := employee.NewHandler(empService)
	empHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register medical recording module
	recStore := recording.NewStore(pool)
	recService := recording.NewService(recStore, empStore, cfg.External.RecordingWorkerURL, cfg.External.RecordingWorkerToken, cfg.External.LingceWorkerURL, cfg.External.LingceWorkerToken)
	recHandler := recording.NewHandler(recService)
	recHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register sysconfig module
	sysconfigHandler := sysconfig.NewHandler(sysconfigService)
	sysconfigHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register rbac module
	rbacStore := rbac.NewStore(pool)
	rbacService := rbac.NewService(rbacStore)
	rbacHandler := rbac.NewHandler(rbacService)
	rbacHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Create LLM gateway client
	llmClient := llmgateway.NewClient(cfg.External.LLMGatewayURL, cfg.External.LLMGatewayAPIKey)

	// Register support module
	supportStore := support.NewStore(pool)
	supportService := support.NewService(supportStore, llmClient)
	supportHandler := support.NewHandler(supportService)
	supportHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register customer module
	customerStore := customer.NewStore(pool)
	customerService := customer.NewService(customerStore)
	customerHandler := customer.NewHandler(customerService)
	customerHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register badge module
	badgeStore := badge.NewStore(pool)
	ticketNotifier := badge.NewTicketEmailNotifier(
		cfg.External.TicketNotifySMTPHost,
		cfg.External.TicketNotifySMTPPort,
		cfg.External.TicketNotifySMTPUser,
		cfg.External.TicketNotifySMTPPass,
		cfg.External.TicketNotifyFrom,
		cfg.External.TicketNotifyTo,
	)
	if ticketNotifier == nil {
		slog.Warn("badge ticket email notifier disabled", "missing", badge.MissingTicketEmailNotifierFields(
			cfg.External.TicketNotifySMTPHost,
			cfg.External.TicketNotifySMTPPort,
			cfg.External.TicketNotifySMTPUser,
			cfg.External.TicketNotifySMTPPass,
			cfg.External.TicketNotifyFrom,
			cfg.External.TicketNotifyTo,
		))
	} else {
		slog.Info("badge ticket email notifier enabled",
			"smtp_host", cfg.External.TicketNotifySMTPHost,
			"smtp_port", cfg.External.TicketNotifySMTPPort,
			"from", cfg.External.TicketNotifyFrom,
			"to", cfg.External.TicketNotifyTo)
	}
	badgeService := badge.NewServiceWithMiddleware(
		badgeStore,
		empStore,
		cfg.External.BadgeMiddlewareURL,
		cfg.External.BadgeMiddlewareToken,
		cfg.External.LingceWorkerURL,
		cfg.External.LingceWorkerToken,
		ticketNotifier,
	)
	if cfg.External.LingceWorkerURL == "" {
		slog.Warn("badge callback worker target is not configured; audio callback enqueue will be disabled", "expected_env", "LINGCE_WORKER_URL")
	} else {
		slog.Info("badge callback worker target configured",
			"worker_url", cfg.External.LingceWorkerURL,
			"token_configured", strings.TrimSpace(cfg.External.LingceWorkerToken) != "")
	}
	badgeHandler := badge.NewHandler(badgeService)
	badgeHandler.SetCallbackGatewayToken(cfg.External.BadgeCallbackGatewayToken)
	badgeHandler.RegisterRoutes(mux, cfg.JWT.Secret)

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

	// Register sandbox transfer module
	sandboxService := sandbox.NewService(pool, ossClient, cfg.External.LingceWorkerURL, cfg.External.LingceWorkerToken)
	sandboxHandler := sandbox.NewHandler(sandboxService)
	sandboxHandler.RegisterRoutes(mux, cfg.JWT.Secret)

	// Register dashboard module
	dashboardStore := dashboard.NewStore(pool)
	dashboardService := dashboard.NewService(dashboardStore, recService)
	dashboardHandler := dashboard.NewHandler(dashboardService)
	dashboardHandler.RegisterRoutes(mux, cfg.JWT.Secret, pool)

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
		WriteTimeout: 5 * time.Minute,
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

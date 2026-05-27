package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	JWT      JWTConfig
	External ExternalConfig
	Aliyun   AliyunConfig
	WeCom    WeComConfig
	Log      LogConfig
}

type ServerConfig struct {
	Port string
	Host string
}

type DatabaseConfig struct {
	URL string
}

type JWTConfig struct {
	Secret      string
	ExpiryHours int
}

type ExternalConfig struct {
	LLMGatewayURL             string
	LLMGatewayAPIKey          string
	BadgeMiddlewareURL        string
	BadgeMiddlewareToken      string
	BadgeCallbackGatewayToken string
	RecordingWorkerURL        string
	RecordingWorkerToken      string
	LingceWorkerURL           string
	LingceWorkerToken         string
	TicketNotifySMTPHost      string
	TicketNotifySMTPPort      int
	TicketNotifySMTPUser      string
	TicketNotifySMTPPass      string
	TicketNotifyFrom          string
	TicketNotifyTo            string
}

type AliyunConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	SMSSignName     string
	SMSTemplateCode string
	OSSEndpoint     string
	OSSBucket       string
}

type WeComConfig struct {
	SuiteID            string
	SuiteSecret        string
	Token              string
	EncodingAESKey     string
	CallbackBaseURL    string
	APIBaseURL         string
	InstallRedirectURL string
	InstallAuthType    int
}

type LogConfig struct {
	Level string
}

// Load loads configuration from environment variables and .env file
func Load() (*Config, error) {
	// Try to load .env file (optional)
	_ = godotenv.Load("configs/.env")
	_ = godotenv.Load(".env")

	recordingWorkerToken, recordingWorkerTokenSource := resolveWorkerToken(
		"INTERNAL_WORKER_TOKEN",
		"RECORDING_WORKER_TOKEN",
		"LINGCE_WORKER_TOKEN",
	)
	lingceWorkerToken, lingceWorkerTokenSource := resolveWorkerToken(
		"INTERNAL_WORKER_TOKEN",
		"LINGCE_WORKER_TOKEN",
		"RECORDING_WORKER_TOKEN",
	)

	cfg := &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "18080"),
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
		},
		Database: DatabaseConfig{
			URL: getEnv("DATABASE_URL", ""),
		},
		JWT: JWTConfig{
			Secret:      getEnv("JWT_SECRET", ""),
			ExpiryHours: getEnvInt("JWT_EXPIRY_HOURS", 24),
		},
		External: ExternalConfig{
			LLMGatewayURL:             getEnv("LLM_GATEWAY_URL", "http://localhost:8080"),
			LLMGatewayAPIKey:          getEnv("LLM_GATEWAY_API_KEY", ""),
			BadgeMiddlewareURL:        getEnv("BADGE_MIDDLEWARE_URL", "http://localhost:18082"),
			BadgeMiddlewareToken:      getEnv("BADGE_MIDDLEWARE_TOKEN", ""),
			BadgeCallbackGatewayToken: getEnv("BADGE_CALLBACK_GATEWAY_TOKEN", ""),
			RecordingWorkerURL:        getEnv("RECORDING_WORKER_URL", "http://localhost:18090"),
			RecordingWorkerToken:      recordingWorkerToken,
			LingceWorkerURL:           getEnv("LINGCE_WORKER_URL", ""),
			LingceWorkerToken:         lingceWorkerToken,
			TicketNotifySMTPHost:      getEnv("TICKET_NOTIFY_SMTP_HOST", ""),
			TicketNotifySMTPPort:      getEnvInt("TICKET_NOTIFY_SMTP_PORT", 25),
			TicketNotifySMTPUser:      getEnv("TICKET_NOTIFY_SMTP_USER", ""),
			TicketNotifySMTPPass:      getEnv("TICKET_NOTIFY_SMTP_PASS", ""),
			TicketNotifyFrom:          getEnv("TICKET_NOTIFY_FROM", ""),
			TicketNotifyTo:            getEnv("TICKET_NOTIFY_TO", ""),
		},
		Aliyun: AliyunConfig{
			AccessKeyID:     getEnv("ALIYUN_ACCESS_KEY_ID", ""),
			AccessKeySecret: getEnv("ALIYUN_ACCESS_KEY_SECRET", ""),
			SMSSignName:     getEnv("ALIYUN_SMS_SIGN_NAME", ""),
			SMSTemplateCode: getEnv("ALIYUN_SMS_TEMPLATE_CODE", ""),
			OSSEndpoint:     getEnv("ALIYUN_OSS_ENDPOINT", ""),
			OSSBucket:       getEnv("ALIYUN_OSS_BUCKET", ""),
		},
		WeCom: WeComConfig{
			SuiteID:            getEnv("WECOM_SUITE_ID", ""),
			SuiteSecret:        getEnv("WECOM_SUITE_SECRET", ""),
			Token:              getEnv("WECOM_TOKEN", ""),
			EncodingAESKey:     getEnv("WECOM_ENCODING_AES_KEY", ""),
			CallbackBaseURL:    getEnv("WECOM_CALLBACK_BASE_URL", ""),
			APIBaseURL:         getEnv("WECOM_API_BASE_URL", "https://qyapi.weixin.qq.com"),
			InstallRedirectURL: getEnv("WECOM_INSTALL_REDIRECT_URL", ""),
			InstallAuthType:    getEnvInt("WECOM_INSTALL_AUTH_TYPE", 1),
		},
		Log: LogConfig{
			Level: getEnv("LOG_LEVEL", "info"),
		},
	}

	logWorkerTokenResolution(recordingWorkerTokenSource, lingceWorkerTokenSource)

	// Validate required fields
	if cfg.Database.URL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	return cfg, nil
}

func resolveWorkerToken(keys ...string) (string, string) {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value, key
		}
	}
	return "", ""
}

func logWorkerTokenResolution(recordingSource, lingceSource string) {
	internal := strings.TrimSpace(os.Getenv("INTERNAL_WORKER_TOKEN"))
	recording := strings.TrimSpace(os.Getenv("RECORDING_WORKER_TOKEN"))
	lingce := strings.TrimSpace(os.Getenv("LINGCE_WORKER_TOKEN"))

	if internal != "" {
		if recording != "" && recording != internal {
			slog.Warn("worker token mismatch detected: RECORDING_WORKER_TOKEN differs from INTERNAL_WORKER_TOKEN")
		}
		if lingce != "" && lingce != internal {
			slog.Warn("worker token mismatch detected: LINGCE_WORKER_TOKEN differs from INTERNAL_WORKER_TOKEN")
		}
	}
	if recording != "" && lingce != "" && recording != lingce {
		slog.Warn("worker token mismatch detected: RECORDING_WORKER_TOKEN differs from LINGCE_WORKER_TOKEN")
	}

	slog.Info("worker token sources resolved",
		"recording_worker_token_source", recordingSource,
		"lingce_worker_token_source", lingceSource,
	)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

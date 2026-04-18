package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	JWT      JWTConfig
	External ExternalConfig
	Aliyun   AliyunConfig
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
}

type AliyunConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	SMSSignName     string
	SMSTemplateCode string
	OSSEndpoint     string
	OSSBucket       string
}

type LogConfig struct {
	Level string
}

// Load loads configuration from environment variables and .env file
func Load() (*Config, error) {
	// Try to load .env file (optional)
	_ = godotenv.Load("configs/.env")
	_ = godotenv.Load(".env")

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
			RecordingWorkerToken:      getEnv("RECORDING_WORKER_TOKEN", ""),
		},
		Aliyun: AliyunConfig{
			AccessKeyID:     getEnv("ALIYUN_ACCESS_KEY_ID", ""),
			AccessKeySecret: getEnv("ALIYUN_ACCESS_KEY_SECRET", ""),
			SMSSignName:     getEnv("ALIYUN_SMS_SIGN_NAME", ""),
			SMSTemplateCode: getEnv("ALIYUN_SMS_TEMPLATE_CODE", ""),
			OSSEndpoint:     getEnv("ALIYUN_OSS_ENDPOINT", ""),
			OSSBucket:       getEnv("ALIYUN_OSS_BUCKET", ""),
		},
		Log: LogConfig{
			Level: getEnv("LOG_LEVEL", "info"),
		},
	}

	// Validate required fields
	if cfg.Database.URL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	return cfg, nil
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

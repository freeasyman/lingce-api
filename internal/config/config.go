package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	App       AppConfig       `toml:"app"`
	Server    ServerConfig    `toml:"server"`
	Database  DatabaseConfig  `toml:"database"`
	Schema    SchemaConfig    `toml:"schema"`
	JWT       JWTConfig       `toml:"jwt"`
	Recording RecordingConfig `toml:"recording"`
	External  ExternalConfig  `toml:"external"`
	DashScope DashScopeConfig `toml:"dashscope"`
	Aliyun    AliyunConfig    `toml:"aliyun"`
	WeCom     WeComConfig     `toml:"wecom"`
	Log       LogConfig       `toml:"log"`
}

type AppConfig struct {
	Name        string `toml:"name"`
	Env         string `toml:"env"`
	SecretsFile string `toml:"secrets_file"`
}

type ServerConfig struct {
	Port string `toml:"port"`
	Host string `toml:"host"`
}

type DatabaseConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	Name     string `toml:"name"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	SSLMode  string `toml:"sslmode"`
	MaxConns int32  `toml:"max_conns"`
	MinConns int32  `toml:"min_conns"`
}

type SchemaConfig struct {
	MaxMigrationVersion string `toml:"max_migration_version"`
}

type JWTConfig struct {
	Secret      string `toml:"secret"`
	ExpiryHours int    `toml:"expiry_hours"`
}

type RecordingConfig struct {
	PlayURLRequireOwnedMedia bool   `toml:"play_url_require_owned_media"`
	ResetCodeDictionaryPath  string `toml:"reset_code_dictionary_path"`
}

type ExternalConfig struct {
	LLMGatewayURL             string             `toml:"llm_gateway_url"`
	LLMGatewayAPIKey          string             `toml:"llm_gateway_api_key"`
	BadgeMiddlewareURL        string             `toml:"badge_middleware_url"`
	BadgeMiddlewareToken      string             `toml:"badge_middleware_token"`
	BadgeCallbackGatewayToken string             `toml:"badge_callback_gateway_token"`
	RecordingWorkerURL        string             `toml:"recording_worker_url"`
	RecordingWorkerToken      string             `toml:"recording_worker_token"`
	LingceWorkerURL           string             `toml:"lingce_worker_url"`
	LingceWorkerToken         string             `toml:"lingce_worker_token"`
	InternalWorkerToken       string             `toml:"internal_worker_token"`
	EmployeeWebBaseURL        string             `toml:"employee_web_base_url"`
	TicketNotify              TicketNotifyConfig `toml:"ticket_notify"`
}

type TicketNotifyConfig struct {
	SMTPHost string `toml:"smtp_host"`
	SMTPPort int    `toml:"smtp_port"`
	SMTPUser string `toml:"smtp_user"`
	SMTPPass string `toml:"smtp_pass"`
	From     string `toml:"from"`
	To       string `toml:"to"`
}

type DashScopeConfig struct {
	APIKey  string `toml:"api_key"`
	Model   string `toml:"model"`
	BaseURL string `toml:"base_url"`
}

type AliyunConfig struct {
	AccessKeyID      string `toml:"access_key_id"`
	AccessKeySecret  string `toml:"access_key_secret"`
	SMSSignName      string `toml:"sms_sign_name"`
	SMSTemplateCode  string `toml:"sms_template_code"`
	OSSEndpoint      string `toml:"oss_endpoint"`
	OSSBucket        string `toml:"oss_bucket"`
	OSSPublicBaseURL string `toml:"oss_public_base_url"`
}

type WeComConfig struct {
	APIBaseURL   string              `toml:"api_base_url"`
	DelegatedApp WeComModeConfig     `toml:"delegated_app"`
	Provider     WeComProviderConfig `toml:"provider"`
}

type WeComModeConfig struct {
	SuiteID            string              `toml:"suite_id"`
	SuiteSecret        string              `toml:"suite_secret"`
	Token              string              `toml:"token"`
	EncodingAESKey     string              `toml:"encoding_aes_key"`
	CallbackBaseURL    string              `toml:"callback_base_url"`
	InstallRedirectURL string              `toml:"install_redirect_url"`
	InstallAuthType    int                 `toml:"install_auth_type"`
	EnterpriseCallback WeComCallbackConfig `toml:"enterprise_callback"`
}

type WeComCallbackConfig struct {
	Token          string `toml:"token"`
	EncodingAESKey string `toml:"encoding_aes_key"`
}

type WeComProviderConfig struct {
	CorpID string `toml:"corp_id"`
	Secret string `toml:"secret"`
}

type LogConfig struct {
	Level string `toml:"level"`
}

type secretsConfig struct {
	Database  DatabaseSecrets  `toml:"database"`
	JWT       JWTSecrets       `toml:"jwt"`
	External  ExternalSecrets  `toml:"external"`
	DashScope DashScopeSecrets `toml:"dashscope"`
	Aliyun    AliyunSecrets    `toml:"aliyun"`
	WeCom     WeComSecrets     `toml:"wecom"`
}

type DatabaseSecrets struct {
	Password string `toml:"password"`
}

type JWTSecrets struct {
	Secret string `toml:"secret"`
}

type ExternalSecrets struct {
	LLMGatewayAPIKey          string             `toml:"llm_gateway_api_key"`
	BadgeMiddlewareToken      string             `toml:"badge_middleware_token"`
	BadgeCallbackGatewayToken string             `toml:"badge_callback_gateway_token"`
	RecordingWorkerToken      string             `toml:"recording_worker_token"`
	LingceWorkerToken         string             `toml:"lingce_worker_token"`
	InternalWorkerToken       string             `toml:"internal_worker_token"`
	EmployeeWebBaseURL        string             `toml:"employee_web_base_url"`
	TicketNotify              TicketNotifyConfig `toml:"ticket_notify"`
}

type DashScopeSecrets struct {
	APIKey  string `toml:"api_key"`
	Model   string `toml:"model"`
	BaseURL string `toml:"base_url"`
}

type AliyunSecrets struct {
	AccessKeyID     string `toml:"access_key_id"`
	AccessKeySecret string `toml:"access_key_secret"`
}

type WeComSecrets struct {
	DelegatedApp WeComModeSecrets     `toml:"delegated_app"`
	Provider     WeComProviderSecrets `toml:"provider"`
}

type WeComModeSecrets struct {
	SuiteID            string               `toml:"suite_id"`
	SuiteSecret        string               `toml:"suite_secret"`
	Token              string               `toml:"token"`
	EncodingAESKey     string               `toml:"encoding_aes_key"`
	CallbackBaseURL    string               `toml:"callback_base_url"`
	InstallRedirectURL string               `toml:"install_redirect_url"`
	InstallAuthType    int                  `toml:"install_auth_type"`
	EnterpriseCallback WeComCallbackSecrets `toml:"enterprise_callback"`
}

type WeComCallbackSecrets struct {
	Token          string `toml:"token"`
	EncodingAESKey string `toml:"encoding_aes_key"`
}

type WeComProviderSecrets struct {
	CorpID string `toml:"corp_id"`
	Secret string `toml:"secret"`
}

func Load(configPath string) (*Config, error) {
	if configPath == "" {
		return nil, fmt.Errorf("--config is required")
	}

	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	cfg := &Config{}
	if err := decodeTOML(configPath, cfg); err != nil {
		return nil, fmt.Errorf("load main config: %w", err)
	}

	applyDefaults(cfg)

	if cfg.App.SecretsFile == "" {
		return nil, fmt.Errorf("app.secrets_file is required")
	}

	secretsPath := resolvePath(filepath.Dir(configPath), cfg.App.SecretsFile)
	secrets := &secretsConfig{}
	if err := decodeTOML(secretsPath, secrets); err != nil {
		return nil, fmt.Errorf("load secrets config: %w", err)
	}

	cfg.App.SecretsFile = secretsPath
	cfg.Recording.ResetCodeDictionaryPath = resolvePath(filepath.Dir(configPath), cfg.Recording.ResetCodeDictionaryPath)
	mergeSecrets(cfg, secrets)

	recordingWorkerToken, recordingWorkerTokenSource := resolveWorkerToken(
		cfg.External.InternalWorkerToken,
		cfg.External.RecordingWorkerToken,
		cfg.External.LingceWorkerToken,
	)
	lingceWorkerToken, lingceWorkerTokenSource := resolveWorkerToken(
		cfg.External.InternalWorkerToken,
		cfg.External.LingceWorkerToken,
		cfg.External.RecordingWorkerToken,
	)
	cfg.External.RecordingWorkerToken = recordingWorkerToken
	cfg.External.LingceWorkerToken = lingceWorkerToken

	logWorkerTokenResolution(
		cfg.External.InternalWorkerToken,
		cfg.External.RecordingWorkerToken,
		cfg.External.LingceWorkerToken,
		recordingWorkerTokenSource,
		lingceWorkerTokenSource,
	)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.App.Name == "" {
		cfg.App.Name = "lingce-api"
	}
	if cfg.App.Env == "" {
		cfg.App.Env = "dev"
	}
	if cfg.Server.Port == "" {
		cfg.Server.Port = "18080"
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if cfg.Database.SSLMode == "" {
		cfg.Database.SSLMode = "disable"
	}
	if cfg.Database.MaxConns == 0 {
		cfg.Database.MaxConns = 2
	}
	if cfg.Database.MinConns == 0 {
		cfg.Database.MinConns = 1
	}
	if cfg.JWT.ExpiryHours == 0 {
		cfg.JWT.ExpiryHours = 24
	}
	if cfg.Recording.ResetCodeDictionaryPath == "" {
		cfg.Recording.ResetCodeDictionaryPath = "./configs/reset_code_dictionary.json"
	}
	if cfg.External.LLMGatewayURL == "" {
		cfg.External.LLMGatewayURL = "http://localhost:8080"
	}
	if cfg.External.BadgeMiddlewareURL == "" {
		cfg.External.BadgeMiddlewareURL = "http://localhost:18082"
	}
	if cfg.External.RecordingWorkerURL == "" {
		cfg.External.RecordingWorkerURL = "http://localhost:18090"
	}
	if cfg.WeCom.APIBaseURL == "" {
		cfg.WeCom.APIBaseURL = "https://qyapi.weixin.qq.com"
	}
	applyWeComModeDefaults(&cfg.WeCom.DelegatedApp)
	if cfg.External.EmployeeWebBaseURL == "" {
		if strings.EqualFold(cfg.App.Env, "development") || strings.EqualFold(cfg.App.Env, "dev") {
			cfg.External.EmployeeWebBaseURL = "http://localhost:3000"
		} else {
			cfg.External.EmployeeWebBaseURL = "https://employee.khgl.xyz"
		}
	}
	if cfg.External.TicketNotify.SMTPPort == 0 {
		cfg.External.TicketNotify.SMTPPort = 25
	}
	if cfg.DashScope.Model == "" {
		cfg.DashScope.Model = "qwen-max"
	}
	if cfg.DashScope.BaseURL == "" {
		cfg.DashScope.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
}

func mergeSecrets(cfg *Config, secrets *secretsConfig) {
	if secrets.Database.Password != "" {
		cfg.Database.Password = secrets.Database.Password
	}
	if secrets.JWT.Secret != "" {
		cfg.JWT.Secret = secrets.JWT.Secret
	}
	if secrets.External.LLMGatewayAPIKey != "" {
		cfg.External.LLMGatewayAPIKey = secrets.External.LLMGatewayAPIKey
	}
	if secrets.External.BadgeMiddlewareToken != "" {
		cfg.External.BadgeMiddlewareToken = secrets.External.BadgeMiddlewareToken
	}
	if secrets.External.BadgeCallbackGatewayToken != "" {
		cfg.External.BadgeCallbackGatewayToken = secrets.External.BadgeCallbackGatewayToken
	}
	if secrets.External.RecordingWorkerToken != "" {
		cfg.External.RecordingWorkerToken = secrets.External.RecordingWorkerToken
	}
	if secrets.External.LingceWorkerToken != "" {
		cfg.External.LingceWorkerToken = secrets.External.LingceWorkerToken
	}
	if secrets.External.InternalWorkerToken != "" {
		cfg.External.InternalWorkerToken = secrets.External.InternalWorkerToken
	}
	if secrets.External.TicketNotify.SMTPHost != "" {
		cfg.External.TicketNotify.SMTPHost = secrets.External.TicketNotify.SMTPHost
	}
	if secrets.External.TicketNotify.SMTPPort != 0 {
		cfg.External.TicketNotify.SMTPPort = secrets.External.TicketNotify.SMTPPort
	}
	if secrets.External.TicketNotify.SMTPUser != "" {
		cfg.External.TicketNotify.SMTPUser = secrets.External.TicketNotify.SMTPUser
	}
	if secrets.External.TicketNotify.SMTPPass != "" {
		cfg.External.TicketNotify.SMTPPass = secrets.External.TicketNotify.SMTPPass
	}
	if secrets.External.TicketNotify.From != "" {
		cfg.External.TicketNotify.From = secrets.External.TicketNotify.From
	}
	if secrets.External.TicketNotify.To != "" {
		cfg.External.TicketNotify.To = secrets.External.TicketNotify.To
	}
	if secrets.DashScope.APIKey != "" {
		cfg.DashScope.APIKey = secrets.DashScope.APIKey
	}
	if secrets.DashScope.Model != "" {
		cfg.DashScope.Model = secrets.DashScope.Model
	}
	if secrets.DashScope.BaseURL != "" {
		cfg.DashScope.BaseURL = secrets.DashScope.BaseURL
	}
	if secrets.Aliyun.AccessKeyID != "" {
		cfg.Aliyun.AccessKeyID = secrets.Aliyun.AccessKeyID
	}
	if secrets.Aliyun.AccessKeySecret != "" {
		cfg.Aliyun.AccessKeySecret = secrets.Aliyun.AccessKeySecret
	}
	mergeWeComModeSecrets(&cfg.WeCom.DelegatedApp, secrets.WeCom.DelegatedApp)
	mergeWeComProviderSecrets(&cfg.WeCom.Provider, secrets.WeCom.Provider)
}

func validate(cfg *Config) error {
	if cfg.Database.Host == "" {
		return fmt.Errorf("database.host is required")
	}
	if cfg.Database.Name == "" {
		return fmt.Errorf("database.name is required")
	}
	if cfg.Database.User == "" {
		return fmt.Errorf("database.user is required")
	}
	if cfg.Database.Password == "" {
		return fmt.Errorf("database.password is required in secrets file")
	}
	if cfg.JWT.Secret == "" {
		return fmt.Errorf("jwt.secret is required in secrets file")
	}
	if strings.TrimSpace(cfg.External.LLMGatewayURL) == "" {
		return fmt.Errorf("external.llm_gateway_url is required")
	}
	if strings.TrimSpace(cfg.External.BadgeMiddlewareURL) == "" {
		return fmt.Errorf("external.badge_middleware_url is required")
	}
	if strings.TrimSpace(cfg.External.BadgeMiddlewareToken) == "" {
		return fmt.Errorf("external.badge_middleware_token is required in secrets file")
	}
	return nil
}

func applyWeComModeDefaults(cfg *WeComModeConfig) {
	if cfg.InstallAuthType == 0 {
		cfg.InstallAuthType = 1
	}
}

func mergeWeComModeSecrets(cfg *WeComModeConfig, secrets WeComModeSecrets) {
	if secrets.SuiteID != "" {
		cfg.SuiteID = secrets.SuiteID
	}
	if secrets.SuiteSecret != "" {
		cfg.SuiteSecret = secrets.SuiteSecret
	}
	if secrets.Token != "" {
		cfg.Token = secrets.Token
	}
	if secrets.EncodingAESKey != "" {
		cfg.EncodingAESKey = secrets.EncodingAESKey
	}
	if secrets.CallbackBaseURL != "" {
		cfg.CallbackBaseURL = secrets.CallbackBaseURL
	}
	if secrets.InstallRedirectURL != "" {
		cfg.InstallRedirectURL = secrets.InstallRedirectURL
	}
	if secrets.InstallAuthType != 0 {
		cfg.InstallAuthType = secrets.InstallAuthType
	}
	if secrets.EnterpriseCallback.Token != "" {
		cfg.EnterpriseCallback.Token = secrets.EnterpriseCallback.Token
	}
	if secrets.EnterpriseCallback.EncodingAESKey != "" {
		cfg.EnterpriseCallback.EncodingAESKey = secrets.EnterpriseCallback.EncodingAESKey
	}
}

func mergeWeComProviderSecrets(cfg *WeComProviderConfig, secrets WeComProviderSecrets) {
	if secrets.CorpID != "" {
		cfg.CorpID = secrets.CorpID
	}
	if secrets.Secret != "" {
		cfg.Secret = secrets.Secret
	}
}

func resolveWorkerToken(values ...string) (string, string) {
	for idx, value := range values {
		if value != "" {
			switch idx {
			case 0:
				return value, "internal_worker_token"
			case 1:
				return value, "service_specific_primary"
			case 2:
				return value, "service_specific_fallback"
			}
		}
	}
	return "", ""
}

func logWorkerTokenResolution(internal, recording, lingce, recordingSource, lingceSource string) {
	if internal != "" {
		if recording != "" && recording != internal {
			slog.Warn("worker token mismatch detected: recording worker token differs from internal worker token")
		}
		if lingce != "" && lingce != internal {
			slog.Warn("worker token mismatch detected: lingce worker token differs from internal worker token")
		}
	}
	if recording != "" && lingce != "" && recording != lingce {
		slog.Warn("worker token mismatch detected: recording worker token differs from lingce worker token")
	}

	slog.Info("worker token sources resolved",
		"recording_worker_token_source", recordingSource,
		"lingce_worker_token_source", lingceSource,
	)
}

func resolvePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func decodeTOML(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func (c DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgresql://%s:%s@%s:%d/%s?sslmode=%s",
		c.User,
		c.Password,
		c.Host,
		c.Port,
		c.Name,
		c.SSLMode,
	)
}

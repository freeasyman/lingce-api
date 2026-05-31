package scriptutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	apiconfig "github.com/freeasyman/lingce-api/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	App      appConfig      `toml:"app"`
	Database databaseConfig `toml:"database"`
	JWT      jwtConfig      `toml:"jwt"`
	Aliyun   aliyunConfig   `toml:"aliyun"`
}

type appConfig struct {
	SecretsFile string `toml:"secrets_file"`
}

type databaseConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	Name     string `toml:"name"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	SSLMode  string `toml:"sslmode"`
}

type jwtConfig struct {
	Secret string `toml:"secret"`
}

type aliyunConfig struct {
	AccessKeyID      string `toml:"access_key_id"`
	AccessKeySecret  string `toml:"access_key_secret"`
	OSSEndpoint      string `toml:"oss_endpoint"`
	OSSBucket        string `toml:"oss_bucket"`
	OSSPublicBaseURL string `toml:"oss_public_base_url"`
}

type secretsConfig struct {
	Database databaseSecrets `toml:"database"`
	JWT      jwtSecrets      `toml:"jwt"`
	Aliyun   aliyunSecrets   `toml:"aliyun"`
}

type databaseSecrets struct {
	Password string `toml:"password"`
}

type jwtSecrets struct {
	Secret string `toml:"secret"`
}

type aliyunSecrets struct {
	AccessKeyID     string `toml:"access_key_id"`
	AccessKeySecret string `toml:"access_key_secret"`
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
	mergeSecrets(cfg, secrets)

	if cfg.Database.Host == "" || cfg.Database.Name == "" || cfg.Database.User == "" || cfg.Database.Password == "" {
		return nil, fmt.Errorf("database config is incomplete")
	}

	return cfg, nil
}

func OpenPool(ctx context.Context, configPath string) (*Config, *pgxpool.Pool, error) {
	cfg, err := Load(configPath)
	if err != nil {
		return nil, nil, err
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseDSN())
	if err != nil {
		return nil, nil, err
	}
	return cfg, pool, nil
}

func (c *Config) DatabaseDSN() string {
	return apiconfig.DatabaseConfig{
		Host:     c.Database.Host,
		Port:     c.Database.Port,
		Name:     c.Database.Name,
		User:     c.Database.User,
		Password: c.Database.Password,
		SSLMode:  c.Database.SSLMode,
	}.DSN()
}

func applyDefaults(cfg *Config) {
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if cfg.Database.SSLMode == "" {
		cfg.Database.SSLMode = "disable"
	}
}

func mergeSecrets(cfg *Config, secrets *secretsConfig) {
	if secrets.Database.Password != "" {
		cfg.Database.Password = secrets.Database.Password
	}
	if secrets.JWT.Secret != "" {
		cfg.JWT.Secret = secrets.JWT.Secret
	}
	if secrets.Aliyun.AccessKeyID != "" {
		cfg.Aliyun.AccessKeyID = secrets.Aliyun.AccessKeyID
	}
	if secrets.Aliyun.AccessKeySecret != "" {
		cfg.Aliyun.AccessKeySecret = secrets.Aliyun.AccessKeySecret
	}
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

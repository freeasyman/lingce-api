package store

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool creates a new PostgreSQL connection pool.
// slowSQLThreshold 默认 200ms；LINGCE_SLOW_SQL_MS 可覆盖（<=0 表示关闭）。
func slowSQLThreshold() time.Duration {
	value := strings.TrimSpace(os.Getenv("LINGCE_SLOW_SQL_MS"))
	if value == "" {
		return 200 * time.Millisecond
	}
	ms, err := strconv.Atoi(value)
	if err != nil || ms <= 0 {
		return 24 * time.Hour // 相当于关闭
	}
	return time.Duration(ms) * time.Millisecond
}

func NewPostgresPool(ctx context.Context, databaseURL string, maxConns, minConns int32) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	if maxConns > 0 {
		config.MaxConns = maxConns
	}
	if minConns > 0 {
		config.MinConns = minConns
	}

	// 慢查询追踪：超过阈值的 SQL 记一条 warn，阈值可用环境变量覆盖。
	config.ConnConfig.Tracer = &slowQueryTracer{threshold: slowSQLThreshold()}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	slog.Info("database connection pool established",
		"max_conns", config.MaxConns,
		"min_conns", config.MinConns)

	return pool, nil
}

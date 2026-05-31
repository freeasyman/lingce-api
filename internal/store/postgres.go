package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool creates a new PostgreSQL connection pool.
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

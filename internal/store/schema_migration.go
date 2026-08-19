package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var schemaMigrationFS embed.FS

const (
	schemaMigrationTable   = "api_schema_migrations"
	schemaMigrationLockKey = int64(2026080701)
)

var schemaMigrationFilePattern = regexp.MustCompile(`^([0-9]{8}_[0-9]{3})_([a-z0-9_]+)\.sql$`)

type schemaMigration struct {
	Version  string
	Name     string
	Path     string
	SQL      string
	Checksum string
}

// ApplySchemaMigrations applies embedded versioned schema migrations exactly once.
func ApplySchemaMigrations(ctx context.Context, pool *pgxpool.Pool, maxVersion string) error {
	migrations, err := loadEmbeddedSchemaMigrations(maxVersion)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no embedded schema migrations found")
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire schema migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, schemaMigrationLockKey); err != nil {
		return fmt.Errorf("acquire schema migration lock: %w", err)
	}
	defer func() {
		if _, unlockErr := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaMigrationLockKey); unlockErr != nil {
			slog.Warn("failed to release schema migration lock", "error", unlockErr)
		}
	}()

	if err := ensureSchemaMigrationTable(ctx, conn); err != nil {
		return err
	}

	applied, err := loadAppliedSchemaMigrations(ctx, conn)
	if err != nil {
		return err
	}
	if err := validateNoUnknownAppliedMigrations(applied, migrations); err != nil {
		return err
	}

	for _, migration := range migrations {
		appliedChecksum, ok := applied[migration.Version]
		if ok {
			if appliedChecksum != migration.Checksum {
				return fmt.Errorf("schema migration checksum mismatch for %s: db=%s code=%s", migration.Version, appliedChecksum, migration.Checksum)
			}
			continue
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin schema migration %s: %w", migration.Version, err)
		}
		if _, err := tx.Exec(ctx, migration.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply schema migration %s (%s): %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s (version, name, checksum, applied_at)
			VALUES ($1, $2, $3, NOW())
		`, schemaMigrationTable), migration.Version, migration.Name, migration.Checksum); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record schema migration %s: %w", migration.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit schema migration %s: %w", migration.Version, err)
		}
		slog.Info("schema migration applied", "version", migration.Version, "name", migration.Name)
	}

	required := migrations[len(migrations)-1]
	slog.Info("schema migrations ready", "version", required.Version)
	return nil
}

// CheckSchemaMigrations verifies that the database has all embedded migrations applied.
func CheckSchemaMigrations(ctx context.Context, pool *pgxpool.Pool, maxVersion string) error {
	migrations, err := loadEmbeddedSchemaMigrations(maxVersion)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no embedded schema migrations found")
	}

	exists, err := schemaMigrationTableExists(ctx, pool)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("schema migration ledger is missing; run lingce-api --migrate-only before starting the API")
	}

	applied, err := loadAppliedSchemaMigrations(ctx, pool)
	if err != nil {
		return err
	}
	if err := validateNoUnknownAppliedMigrations(applied, migrations); err != nil {
		return err
	}
	for _, migration := range migrations {
		appliedChecksum, ok := applied[migration.Version]
		if !ok {
			return fmt.Errorf("schema migration %s (%s) is not applied; run lingce-api --migrate-only before starting the API", migration.Version, migration.Name)
		}
		if appliedChecksum != migration.Checksum {
			return fmt.Errorf("schema migration checksum mismatch for %s: db=%s code=%s", migration.Version, appliedChecksum, migration.Checksum)
		}
	}

	required := migrations[len(migrations)-1]
	slog.Info("schema migrations verified", "version", required.Version)
	return nil
}

func loadEmbeddedSchemaMigrations(maxVersion string) ([]schemaMigration, error) {
	entries, err := fs.ReadDir(schemaMigrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded schema migrations: %w", err)
	}

	migrations := make([]schemaMigration, 0, len(entries))
	seen := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		matches := schemaMigrationFilePattern.FindStringSubmatch(filename)
		if matches == nil {
			return nil, fmt.Errorf("invalid schema migration filename %q; expected YYYYMMDD_NNN_name.sql", filename)
		}
		version := matches[1]
		if existing, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate schema migration version %s in %s and %s", version, existing, filename)
		}
		seen[version] = filename

		path := filepath.ToSlash(filepath.Join("migrations", filename))
		content, err := fs.ReadFile(schemaMigrationFS, path)
		if err != nil {
			return nil, fmt.Errorf("read schema migration %s: %w", filename, err)
		}
		sum := sha256.Sum256(content)
		migrations = append(migrations, schemaMigration{
			Version:  version,
			Name:     strings.TrimSuffix(matches[2], ".sql"),
			Path:     path,
			SQL:      string(content),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return filterSchemaMigrationsByMaxVersion(migrations, maxVersion), nil
}

func filterSchemaMigrationsByMaxVersion(migrations []schemaMigration, maxVersion string) []schemaMigration {
	maxVersion = strings.TrimSpace(maxVersion)
	if maxVersion == "" {
		return migrations
	}
	filtered := make([]schemaMigration, 0, len(migrations))
	for _, migration := range migrations {
		if migration.Version <= maxVersion {
			filtered = append(filtered, migration)
		}
	}
	return filtered
}

func ensureSchemaMigrationTable(ctx context.Context, db compatExecutor) error {
	_, err := db.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT NOW()
		)
	`, schemaMigrationTable))
	return err
}

func schemaMigrationTableExists(ctx context.Context, db compatExecutor) (bool, error) {
	var exists bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('public.`+schemaMigrationTable+`') IS NOT NULL`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check schema migration ledger: %w", err)
	}
	return exists, nil
}

func loadAppliedSchemaMigrations(ctx context.Context, db compatExecutor) (map[string]string, error) {
	rows, err := db.Query(ctx, fmt.Sprintf(`SELECT version, checksum FROM %s`, schemaMigrationTable))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("load applied schema migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]string{}
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan applied schema migration: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied schema migrations: %w", err)
	}
	return applied, nil
}

func validateNoUnknownAppliedMigrations(applied map[string]string, migrations []schemaMigration) error {
	known := map[string]struct{}{}
	for _, migration := range migrations {
		known[migration.Version] = struct{}{}
	}
	for version := range applied {
		if _, ok := known[version]; !ok {
			return fmt.Errorf("database has unknown schema migration %s; current code does not contain this migration", version)
		}
	}
	return nil
}

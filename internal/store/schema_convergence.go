package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed convergence/*.sql
var convergenceFS embed.FS

const convergenceTable = "api_schema_baselines"
const convergenceLockKey int64 = 2026100601

type convergenceScript struct{ Version, Name, SQL, Checksum string }

func loadConvergenceScript(version, file string) (convergenceScript, error) {
	version = strings.TrimSpace(version)
	file = strings.TrimSpace(file)
	if version == "" || file == "" {
		return convergenceScript{}, fmt.Errorf("baseline version and convergence file are required")
	}
	path := "convergence/" + file
	raw, err := fs.ReadFile(convergenceFS, path)
	if err != nil {
		return convergenceScript{}, fmt.Errorf("read convergence script %s: %w", path, err)
	}
	if strings.TrimSpace(string(raw)) == "" || strings.Contains(string(raw), "GENERATED_CONVERGENCE_REQUIRED") {
		return convergenceScript{}, fmt.Errorf("convergence script %s has not been generated and audited", file)
	}
	sum := sha256.Sum256(raw)
	return convergenceScript{Version: version, Name: file, SQL: string(raw), Checksum: hex.EncodeToString(sum[:])}, nil
}

func ApplyConvergence(ctx context.Context, pool *pgxpool.Pool, version, file string) error {
	script, err := loadConvergenceScript(version, file)
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire convergence connection: %w", err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, convergenceLockKey); err != nil {
		return fmt.Errorf("acquire convergence lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, convergenceLockKey) }()
	if err = ensureConvergenceTable(ctx, conn); err != nil {
		return err
	}
	var appliedChecksum string
	err = conn.QueryRow(ctx, fmt.Sprintf(`SELECT checksum FROM %s WHERE version=$1`, convergenceTable), version).Scan(&appliedChecksum)
	if err == nil {
		if appliedChecksum != script.Checksum {
			return fmt.Errorf("convergence checksum mismatch for %s: db=%s code=%s", version, appliedChecksum, script.Checksum)
		}
		slog.Info("convergence already applied", "version", version)
		return nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin convergence %s: %w", version, err)
	}
	if _, err = tx.Exec(ctx, strings.TrimSpace(script.SQL)); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("apply convergence %s: %w", version, err)
	}
	if _, err = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s(version,name,checksum,applied_at) VALUES($1,$2,$3,NOW())`, convergenceTable), version, script.Name, script.Checksum); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("record convergence %s: %w", version, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit convergence %s: %w", version, err)
	}
	slog.Info("convergence applied", "version", version, "name", script.Name)
	return nil
}

func CheckConvergence(ctx context.Context, pool *pgxpool.Pool, version, file string) error {
	script, err := loadConvergenceScript(version, file)
	if err != nil {
		return err
	}
	exists, err := schemaConvergenceTableExists(ctx, pool)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("convergence ledger is missing; run --converge-only before starting the API")
	}
	var checksum string
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT checksum FROM %s WHERE version=$1`, convergenceTable), version).Scan(&checksum); err != nil {
		return fmt.Errorf("convergence %s is not applied; run --converge-only before starting the API: %w", version, err)
	}
	if checksum != script.Checksum {
		return fmt.Errorf("convergence checksum mismatch for %s: db=%s code=%s", version, checksum, script.Checksum)
	}
	slog.Info("convergence verified", "version", version)
	return nil
}

func ensureConvergenceTable(ctx context.Context, db compatExecutor) error {
	_, err := db.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (version TEXT PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at TIMESTAMP NOT NULL DEFAULT NOW())`, convergenceTable))
	return err
}
func schemaConvergenceTableExists(ctx context.Context, db compatExecutor) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT to_regclass('public.`+convergenceTable+`') IS NOT NULL`).Scan(&exists)
	return exists, err
}

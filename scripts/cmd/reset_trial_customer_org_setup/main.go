package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
)

func main() {
	configPath := flag.String("config", "./configs/dev.toml", "path to TOML config file")
	flag.Parse()

	_, pool, err := scriptutil.OpenPool(context.Background(), *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open pool: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "begin tx: %v\n", err)
		os.Exit(1)
	}
	defer tx.Rollback(ctx)

	var orgCountBefore int64
	var adminBoundBefore int64
	var ownershipCountBefore int64
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM ops_organizations`).Scan(&orgCountBefore); err != nil {
		fmt.Fprintf(os.Stderr, "count orgs before: %v\n", err)
		os.Exit(1)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM operations_admins WHERE deleted_at IS NULL AND org_id IS NOT NULL AND org_id > 0`).Scan(&adminBoundBefore); err != nil {
		fmt.Fprintf(os.Stderr, "count bound admins before: %v\n", err)
		os.Exit(1)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM trial_customer_ownerships`).Scan(&ownershipCountBefore); err != nil {
		fmt.Fprintf(os.Stderr, "count ownerships before: %v\n", err)
		os.Exit(1)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM trial_customer_ownerships`); err != nil {
		fmt.Fprintf(os.Stderr, "delete trial customer ownerships: %v\n", err)
		os.Exit(1)
	}
	if _, err := tx.Exec(ctx, `UPDATE operations_admins SET org_id = NULL WHERE org_id IS NOT NULL`); err != nil {
		fmt.Fprintf(os.Stderr, "clear admin org bindings: %v\n", err)
		os.Exit(1)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ops_organizations`); err != nil {
		fmt.Fprintf(os.Stderr, "delete organizations: %v\n", err)
		os.Exit(1)
	}

	var orgCountAfter int64
	var adminBoundAfter int64
	var ownershipCountAfter int64
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM ops_organizations`).Scan(&orgCountAfter); err != nil {
		fmt.Fprintf(os.Stderr, "count orgs after: %v\n", err)
		os.Exit(1)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM operations_admins WHERE deleted_at IS NULL AND org_id IS NOT NULL AND org_id > 0`).Scan(&adminBoundAfter); err != nil {
		fmt.Fprintf(os.Stderr, "count bound admins after: %v\n", err)
		os.Exit(1)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM trial_customer_ownerships`).Scan(&ownershipCountAfter); err != nil {
		fmt.Fprintf(os.Stderr, "count ownerships after: %v\n", err)
		os.Exit(1)
	}

	if err := tx.Commit(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "commit tx: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("reset completed: orgs %d -> %d, bound_admins %d -> %d, ownerships %d -> %d\n",
		orgCountBefore, orgCountAfter, adminBoundBefore, adminBoundAfter, ownershipCountBefore, ownershipCountAfter)
}

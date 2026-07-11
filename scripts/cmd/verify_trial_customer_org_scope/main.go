package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
	"github.com/jackc/pgx/v5"
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

	var trialTenantID int64
	if err := tx.QueryRow(ctx, `
		SELECT tenant_id
		FROM trial_customer_ownerships
		ORDER BY tenant_id ASC
		LIMIT 1
	`).Scan(&trialTenantID); err != nil {
		fmt.Fprintf(os.Stderr, "pick sample tenant: %v\n", err)
		os.Exit(1)
	}

	var platformOrgID int64
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM ops_organizations
		WHERE type = 'platform'
		ORDER BY id ASC
		LIMIT 1
	`).Scan(&platformOrgID); err != nil {
		fmt.Fprintf(os.Stderr, "load platform org: %v\n", err)
		os.Exit(1)
	}

	agencyRows, err := tx.Query(ctx, `
		SELECT id, name
		FROM ops_organizations
		WHERE type = 'agency'
		ORDER BY id ASC
		LIMIT 2
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load agency orgs: %v\n", err)
		os.Exit(1)
	}
	defer agencyRows.Close()

	type orgRow struct {
		id   int64
		name string
	}
	var agencies []orgRow
	for agencyRows.Next() {
		var row orgRow
		if err := agencyRows.Scan(&row.id, &row.name); err != nil {
			fmt.Fprintf(os.Stderr, "scan agency org: %v\n", err)
			os.Exit(1)
		}
		agencies = append(agencies, row)
	}
	if err := agencyRows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "iterate agency orgs: %v\n", err)
		os.Exit(1)
	}
	if len(agencies) == 0 {
		fmt.Fprintln(os.Stderr, "no agency org found")
		os.Exit(1)
	}

	// 在事务里临时把一条试用客户切给 agency[0]，验证后回滚。
	if _, err := tx.Exec(ctx, `
		UPDATE trial_customer_ownerships
		SET owner_org_id = $1,
		    updated_at = NOW()
		WHERE tenant_id = $2
	`, agencies[0].id, trialTenantID); err != nil {
		fmt.Fprintf(os.Stderr, "reassign sample tenant in tx: %v\n", err)
		os.Exit(1)
	}

	var platformVisible int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM trial_customer_ownerships
	`).Scan(&platformVisible); err != nil {
		fmt.Fprintf(os.Stderr, "count platform visible: %v\n", err)
		os.Exit(1)
	}

	var agencyVisible int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM trial_customer_ownerships
		WHERE owner_org_id = $1
	`, agencies[0].id).Scan(&agencyVisible); err != nil {
		fmt.Fprintf(os.Stderr, "count agency visible: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("== transactional scope verification ==")
	fmt.Printf("sample_trial_tenant_id=%d\n", trialTenantID)
	fmt.Printf("platform_org_id=%d\n", platformOrgID)
	fmt.Printf("agency_org_id=%d agency_org_name=%s\n", agencies[0].id, agencies[0].name)
	fmt.Printf("platform_visible_count=%d\n", platformVisible)
	fmt.Printf("agency_visible_count=%d\n", agencyVisible)
	fmt.Printf("agency_can_access_sample=%t\n", agencyVisible > 0)

	if len(agencies) > 1 {
		var foreignVisible int64
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM trial_customer_ownerships
			WHERE owner_org_id = $1
			  AND tenant_id = $2
		`, agencies[1].id, trialTenantID).Scan(&foreignVisible); err != nil {
			fmt.Fprintf(os.Stderr, "count foreign visibility: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("foreign_agency_org_id=%d foreign_agency_org_name=%s\n", agencies[1].id, agencies[1].name)
		fmt.Printf("foreign_agency_can_access_sample=%t\n", foreignVisible > 0)
	}

	// 显式回滚，保证验证不污染现有开发数据。
	if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
		fmt.Fprintf(os.Stderr, "rollback tx: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("transaction rolled back")
}

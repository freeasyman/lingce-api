package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
	"github.com/jackc/pgx/v5"
)

type orgStat struct {
	Type  string
	Count int64
}

type coverageStat struct {
	TrialTenants      int64
	OwnershipRows     int64
	PlatformOwnedRows int64
	AgencyOwnedRows   int64
	MissingOwnership  int64
	MissingAdminOrg   int64
}

type adminScopeSample struct {
	AdminID   int64
	AdminName string
	OrgID     int64
	OrgName   string
	OrgType   string
	TrialCnt  int64
}

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

	fmt.Println("== org stats ==")
	orgRows, err := pool.Query(ctx, `
		SELECT COALESCE(type, 'agency') AS type, COUNT(*)
		FROM ops_organizations
		GROUP BY COALESCE(type, 'agency')
		ORDER BY type
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query org stats: %v\n", err)
		os.Exit(1)
	}
	for orgRows.Next() {
		var row orgStat
		if err := orgRows.Scan(&row.Type, &row.Count); err != nil {
			fmt.Fprintf(os.Stderr, "scan org stats: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s: %d\n", row.Type, row.Count)
	}
	orgRows.Close()

	fmt.Println()
	fmt.Println("== ownership coverage ==")
	var coverage coverageStat
	err = pool.QueryRow(ctx, `
		WITH trial_tenants AS (
			SELECT id
			FROM tenants
			WHERE deleted_at IS NULL
			  AND lower(COALESCE(account_mode, 'formal')) = 'trial'
		)
		SELECT
			(SELECT COUNT(*) FROM trial_tenants) AS trial_tenants,
			(SELECT COUNT(*) FROM trial_customer_ownerships) AS ownership_rows,
			(SELECT COUNT(*) FROM trial_customer_ownerships o JOIN ops_organizations org ON org.id = o.owner_org_id WHERE COALESCE(org.type, 'agency') = 'platform') AS platform_owned_rows,
			(SELECT COUNT(*) FROM trial_customer_ownerships o JOIN ops_organizations org ON org.id = o.owner_org_id WHERE COALESCE(org.type, 'agency') = 'agency') AS agency_owned_rows,
			(SELECT COUNT(*) FROM trial_tenants tt LEFT JOIN trial_customer_ownerships o ON o.tenant_id = tt.id WHERE o.tenant_id IS NULL) AS missing_ownership,
			(SELECT COUNT(*) FROM operations_admins a WHERE a.deleted_at IS NULL AND (a.org_id IS NULL OR a.org_id <= 0)) AS missing_admin_org
	`).Scan(&coverage.TrialTenants, &coverage.OwnershipRows, &coverage.PlatformOwnedRows, &coverage.AgencyOwnedRows, &coverage.MissingOwnership, &coverage.MissingAdminOrg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query coverage: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("trial_tenants=%d ownership_rows=%d platform_owned=%d agency_owned=%d missing_ownership=%d missing_admin_org=%d\n",
		coverage.TrialTenants, coverage.OwnershipRows, coverage.PlatformOwnedRows, coverage.AgencyOwnedRows, coverage.MissingOwnership, coverage.MissingAdminOrg)

	fmt.Println()
	fmt.Println("== platform visibility sample ==")
	var totalTrialCount int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM tenants
		WHERE deleted_at IS NULL
		  AND lower(COALESCE(account_mode, 'formal')) = 'trial'
	`).Scan(&totalTrialCount); err != nil {
		fmt.Fprintf(os.Stderr, "count total trial tenants: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("platform_visible_trial_count=%d\n", totalTrialCount)

	fmt.Println()
	fmt.Println("== agency visibility samples ==")
	agencyRows, err := pool.Query(ctx, `
		SELECT a.id,
		       COALESCE(NULLIF(a.name, ''), NULLIF(a.username, ''), NULLIF(a.phone, ''), '管理员') AS admin_name,
		       o.id AS org_id,
		       COALESCE(o.name, '') AS org_name,
		       COALESCE(o.type, 'agency') AS org_type,
		       COUNT(tco.tenant_id) AS trial_cnt
		FROM operations_admins a
		JOIN ops_organizations o ON o.id = a.org_id
		LEFT JOIN trial_customer_ownerships tco ON tco.owner_org_id = o.id
		WHERE a.deleted_at IS NULL
		  AND COALESCE(o.type, 'agency') = 'agency'
		GROUP BY a.id, admin_name, o.id, o.name, o.type
		ORDER BY trial_cnt DESC, a.id ASC
		LIMIT 5
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query agency samples: %v\n", err)
		os.Exit(1)
	}
	samples := make([]adminScopeSample, 0)
	for agencyRows.Next() {
		var row adminScopeSample
		if err := agencyRows.Scan(&row.AdminID, &row.AdminName, &row.OrgID, &row.OrgName, &row.OrgType, &row.TrialCnt); err != nil {
			fmt.Fprintf(os.Stderr, "scan agency sample: %v\n", err)
			os.Exit(1)
		}
		samples = append(samples, row)
		fmt.Printf("admin_id=%d admin=%s org_id=%d org=%s trial_cnt=%d\n", row.AdminID, row.AdminName, row.OrgID, row.OrgName, row.TrialCnt)
	}
	agencyRows.Close()

	if len(samples) >= 2 {
		fmt.Println()
		fmt.Println("== cross-org boundary check ==")
		var foreignTenantID int64
		err := pool.QueryRow(ctx, `
			SELECT tenant_id
			FROM trial_customer_ownerships
			WHERE owner_org_id = $1
			ORDER BY tenant_id ASC
			LIMIT 1
		`, samples[1].OrgID).Scan(&foreignTenantID)
		if err == nil {
			allowed := samples[0].OrgID == samples[1].OrgID
			fmt.Printf("sample_admin_org=%d foreign_org=%d foreign_tenant_id=%d access_should_be_allowed=%t\n",
				samples[0].OrgID, samples[1].OrgID, foreignTenantID, allowed)
		} else if err != pgx.ErrNoRows {
			fmt.Fprintf(os.Stderr, "query cross-org sample: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println()
	fmt.Println("== trial ownership sample ==")
	trialRows, err := pool.Query(ctx, `
		SELECT t.id,
		       COALESCE(t.name, '') AS tenant_name,
		       COALESCE(org.name, '') AS owner_org_name,
		       COALESCE(a.sales_owner_name_snapshot, '') AS owner_name
		FROM tenants t
		LEFT JOIN trial_customer_ownerships o ON o.tenant_id = t.id
		LEFT JOIN ops_organizations org ON org.id = o.owner_org_id
		LEFT JOIN trial_customer_assignments a ON a.tenant_id = t.id
		WHERE t.deleted_at IS NULL
		  AND lower(COALESCE(t.account_mode, 'formal')) = 'trial'
		ORDER BY t.id DESC
		LIMIT 10
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query trial sample: %v\n", err)
		os.Exit(1)
	}
	for trialRows.Next() {
		var tenantID int64
		var tenantName, ownerOrgName, ownerName string
		if err := trialRows.Scan(&tenantID, &tenantName, &ownerOrgName, &ownerName); err != nil {
			fmt.Fprintf(os.Stderr, "scan trial sample: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("tenant_id=%d tenant=%s owner_org=%s owner=%s\n", tenantID, tenantName, ownerOrgName, ownerName)
	}
	trialRows.Close()
}

package main

import (
	"context"
	"flag"
	"log"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
	"github.com/freeasyman/lingce-api/internal/sysconfig"
	"github.com/freeasyman/lingce-api/internal/tenant"
)

func main() {
	ctx := context.Background()
	configPath := flag.String("config", "./configs/dev.toml", "path to TOML config file")
	flag.Parse()

	_, pool, err := scriptutil.OpenPool(ctx, *configPath)
	if err != nil {
		log.Fatalf("open db pool: %v", err)
	}
	defer pool.Close()

	tenantStore := tenant.NewStore(pool)
	sysconfigStore := sysconfig.NewStore(pool)
	tenantSvc := tenant.NewService(tenantStore, sysconfig.NewService(sysconfigStore))

	rows, err := pool.Query(ctx, `
		SELECT id
		FROM tenants
		WHERE COALESCE(NULLIF(trim(account_mode), ''), 'formal') = 'trial'
		ORDER BY id ASC
	`)
	if err != nil {
		log.Fatalf("query trial tenants: %v", err)
	}
	defer rows.Close()

	var tenantIDs []int64
	for rows.Next() {
		var tenantID int64
		if err := rows.Scan(&tenantID); err != nil {
			log.Fatalf("scan trial tenant: %v", err)
		}
		tenantIDs = append(tenantIDs, tenantID)
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("iterate trial tenants: %v", err)
	}

	for _, tenantID := range tenantIDs {
		resp, err := tenantSvc.InitTrialTenant(ctx, tenantID, tenant.TrialInitRequest{
			TemplateCode: "intent_trial_v1",
			RequestID:    "backfill_trial_demo_setup",
		})
		if err != nil {
			log.Fatalf("init trial tenant %d: %v", tenantID, err)
		}
		log.Printf("backfilled tenant=%d status=%s employees=%d demos=%d", tenantID, resp.Status, len(resp.CreatedEmployees), len(resp.DemoRecordings))
	}
}

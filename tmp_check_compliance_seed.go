package main

import (
	"context"
	"fmt"
	"log"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/store"
)

func main() {
	cfg, err := config.Load("./configs/dev.toml")
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	pool, err := store.NewPostgresPool(ctx, cfg.Database.DSN(), 1, 1)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	var promptCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM recording_analysis_prompts WHERE code = 'compliance_check_v1'`).Scan(&promptCount); err != nil {
		log.Fatal(err)
	}
	var modelCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM llm_model_configs WHERE function_type = 'compliance_check' AND tenant_id = 0 AND deleted_at IS NULL`).Scan(&modelCount); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("prompt_count=%d\nmodel_count=%d\n", promptCount, modelCount)
}

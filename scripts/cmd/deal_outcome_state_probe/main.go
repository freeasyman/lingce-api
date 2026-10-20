// 结果事件 → 接待读模型 探针：验证「以结果事件为准」的状态与净额。
// 只操作验收租户 9001 的 8001 接待，结束后删除本次写入。
//
//	DATABASE_URL='postgresql://...' go run ./scripts/cmd/deal_outcome_state_probe
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/internal/dealexpert"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer pool.Close()

	service := dealexpert.NewService(dealexpert.NewStore(pool))
	const tenantID = int64(9001)
	const encounterID = int64(8001)
	const recordingID = int64(7001)

	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM deal_outcome_events WHERE tenant_id = $1 AND encounter_id = $2`, tenantID, encounterID)
	}()

	before := find(service, ctx, tenantID, recordingID)
	fmt.Printf("before: status=%s net=%s latest=%s\n", before.EffectiveDealStatus, amountText(before.EffectiveNetAmount), dash(before.LatestOutcomeType))

	if _, err := service.CreateOutcome(ctx, tenantID, dealexpert.CreateOutcomeRequest{
		EncounterID: encounterID,
		RecordingID: recordingID,
		Type:        "付款",
		Amount:      ptrFloat(23800),
		Source:      "order",
		Note:        "探针写入，用于验证读模型",
	}); err != nil {
		fmt.Fprintln(os.Stderr, "create outcome:", err)
		os.Exit(1)
	}

	after := find(service, ctx, tenantID, recordingID)
	fmt.Printf("after:  status=%s net=%s latest=%s\n", after.EffectiveDealStatus, amountText(after.EffectiveNetAmount), dash(after.LatestOutcomeType))

	confirmation := find(service, ctx, tenantID, 7002)
	fmt.Printf("control(7002): status=%s net=%s latest=%s\n", confirmation.EffectiveDealStatus, amountText(confirmation.EffectiveNetAmount), dash(confirmation.LatestOutcomeType))

	if before.EffectiveDealStatus == after.EffectiveDealStatus {
		fmt.Fprintln(os.Stderr, "expected status to change after payment outcome")
		os.Exit(1)
	}
	if after.EffectiveDealStatus != "成交了" {
		fmt.Fprintf(os.Stderr, "expected 成交了, got %s\n", after.EffectiveDealStatus)
		os.Exit(1)
	}
	if after.EffectiveNetAmount == nil || *after.EffectiveNetAmount != 23800 {
		fmt.Fprintf(os.Stderr, "expected net 23800, got %s\n", amountText(after.EffectiveNetAmount))
		os.Exit(1)
	}
	fmt.Println("OK")
}

func find(service *dealexpert.Service, ctx context.Context, tenantID, recordingID int64) *dealexpert.EncounterListItem {
	items, _, err := service.ListEncounters(ctx, tenantID, dealexpert.EncounterListParams{Page: 1, PageSize: 50}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list:", err)
		os.Exit(1)
	}
	for _, item := range items {
		if item.ID == recordingID {
			return item
		}
	}
	fmt.Fprintf(os.Stderr, "recording %d not found\n", recordingID)
	os.Exit(1)
	return nil
}

func amountText(value *float64) string {
	if value == nil {
		return "nil"
	}
	return fmt.Sprintf("%.2f", *value)
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func ptrFloat(value float64) *float64 { return &value }

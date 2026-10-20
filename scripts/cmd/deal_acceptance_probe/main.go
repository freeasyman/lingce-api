// 赢客专家验收数据只读探针：直接走生产 Store 查询，验证
// encounter_id / confirmed_deal_status / converted_amount / refund_amount
// 四个字段的真实落库值与扫描结果。只读，不写库。
//
//	DATABASE_URL='postgresql://...' GOCACHE=/tmp/lingce-go-cache \
//	  go run ./scripts/cmd/deal_acceptance_probe
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/internal/recording"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer pool.Close()

	store := recording.NewStore(pool)
	includeShort := false
	items, total, err := store.ListRecordings(ctx, recording.RecordingListRequest{
		TenantID:     9001,
		IncludeShort: &includeShort,
		Page:         1,
		PageSize:     20,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "list:", err)
		os.Exit(1)
	}

	fmt.Printf("total=%d returned=%d\n", total, len(items))
	fmt.Printf("%-6s %-10s %-8s %-12s %-10s %-8s\n", "id", "encounter", "status", "converted", "refund", "customer")
	for _, item := range items {
		encounter := "-"
		if item.EncounterID != nil {
			encounter = fmt.Sprintf("%d", *item.EncounterID)
		}
		status := "-"
		if item.ConfirmedDealStatus != nil {
			status = *item.ConfirmedDealStatus
		}
		converted, refund := "-", "-"
		if item.ConvertedAmount != nil {
			converted = fmt.Sprintf("%.2f", *item.ConvertedAmount)
		}
		if item.RefundAmount != nil {
			refund = fmt.Sprintf("%.2f", *item.RefundAmount)
		}
		customer := ""
		if item.CustomerName != nil {
			customer = *item.CustomerName
		}
		fmt.Printf("%-6d %-10s %-8s %-12s %-10s %-8s\n", item.ID, encounter, status, converted, refund, customer)
	}

	detail, err := store.GetRecordingByID(ctx, 7001)
	if err != nil {
		fmt.Fprintln(os.Stderr, "detail:", err)
		os.Exit(1)
	}
	fmt.Printf("detail 7001: encounter=%v status=%v converted=%v refund=%v\n",
		derefInt(detail.EncounterID), derefStr(detail.ConfirmedDealStatus), derefFloat(detail.ConvertedAmount), derefFloat(detail.RefundAmount))
}

func derefInt(v *int64) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *v)
}

func derefStr(v *string) string {
	if v == nil {
		return "nil"
	}
	return *v
}

func derefFloat(v *float64) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprintf("%.2f", *v)
}

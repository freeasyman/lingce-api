// 销售跟进闭环探针：在验收租户 9001 上验证
// 创建 → 重复创建去重 → 完成（已预约）→ 结果事件 → 时间线 → 清理。
// 只操作验收数据（encounter 8001），结束时删除本次创建的行。
//
//	DATABASE_URL='postgresql://...' GOCACHE=/tmp/lingce-go-cache \
//	  go run ./scripts/cmd/deal_followup_probe
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

	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM deal_outcome_events WHERE tenant_id = $1 AND encounter_id = $2`, tenantID, encounterID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM deal_follow_ups WHERE tenant_id = $1 AND encounter_id = $2`, tenantID, encounterID)
	}()

	followUp, inserted, err := service.CreateFollowUp(ctx, tenantID, dealexpert.CreateFollowUpRequest{
		EncounterID:     encounterID,
		RecordingID:     7001,
		CustomerName:    "王女士",
		Objective:       "确认家属决策条件",
		Action:          "今天 18:00 前联系客户，确认家属主要担心价格、效果还是安全",
		OwnerEmployeeID: ptr(int64(9101)),
		OwnerName:       "王晓",
		DueAt:           ptrTime(time.Now().Add(6 * time.Hour)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "create:", err)
		os.Exit(1)
	}
	fmt.Printf("create: id=%d status=%s inserted=%v owner=%s\n", followUp.ID, followUp.Status, inserted, followUp.OwnerName)

	duplicated, insertedAgain, err := service.CreateFollowUp(ctx, tenantID, dealexpert.CreateFollowUpRequest{
		EncounterID: encounterID,
		RecordingID: 7001,
		Objective:   "确认家属决策条件",
		Action:      "重复点击不应生成第二条活动任务",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "duplicate create:", err)
		os.Exit(1)
	}
	fmt.Printf("duplicate: id=%d inserted=%v\n", duplicated.ID, insertedAgain)

	completed, events, outcome, err := service.CompleteFollowUp(ctx, tenantID, followUp.ID, dealexpert.CompleteFollowUpRequest{
		Result:     dealexpert.ResultAppointed,
		ResultNote: "客户同意周六上午到院，家属一同前来",
		NextAction: "到院前 1 天确认时间",
	}, ptr(int64(9101)), "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "complete:", err)
		os.Exit(1)
	}
	fmt.Printf("complete: status=%s result=%s closed=%v\n", completed.Status, completed.Result, completed.ClosedAt != nil)
	for _, event := range events {
		fmt.Printf("timeline: %s → %s (%s) %s\n", event.FromStatus, event.ToStatus, event.Result, event.ActorName)
	}
	if outcome != nil {
		fmt.Printf("outcome: type=%s source=%s\n", outcome.Type, outcome.Source)
	}

	overview, err := service.Overview(ctx, tenantID, nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "overview:", err)
		os.Exit(1)
	}
	fmt.Printf("overview: total=%d converted=%d following=%d notConverted=%d unknown=%d net=%.2f source=%s missed=%d followUp(completed=%d)\n",
		overview.TotalEncounters, overview.Converted, overview.Following, overview.NotConverted, overview.Unknown,
		overview.NetAmount, overview.AmountSource, overview.ProductMissed, overview.FollowUpCompleted)

	outcomes, err := service.ListOutcomesByEncounter(ctx, tenantID, encounterID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list outcomes:", err)
		os.Exit(1)
	}
	fmt.Printf("encounter outcomes: %d\n", len(outcomes))

	if _, _, _, err := service.CompleteFollowUp(ctx, tenantID, followUp.ID, dealexpert.CompleteFollowUpRequest{Result: dealexpert.ResultPaid, Amount: ptrFloat(23800)}, nil, ""); err == nil {
		fmt.Fprintln(os.Stderr, "expected not-active error on second completion")
		os.Exit(1)
	} else {
		fmt.Printf("second completion blocked: %v\n", err)
	}
	fmt.Println("OK")
}

func ptr[T any](value T) *T { return &value }
func ptrTime(value time.Time) *string {
	formatted := value.Format(time.RFC3339)
	return &formatted
}
func ptrFloat(value float64) *float64 { return &value }

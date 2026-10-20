// 接待列表筛选探针：只读，验证服务端筛选口径。
//
//	DATABASE_URL='postgresql://...' go run ./scripts/cmd/deal_list_filter_probe
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer pool.Close()

	service := dealexpert.NewService(dealexpert.NewStore(pool))
	const tenantID = int64(9001)

	linked := true
	unlinked := false
	cases := []struct {
		name   string
		params dealexpert.EncounterListParams
		want   int
	}{
		{"全部", dealexpert.EncounterListParams{}, 8},
		{"成交了", dealexpert.EncounterListParams{Status: "成交了"}, 2},
		{"还在跟进", dealexpert.EncounterListParams{Status: "还在跟进"}, 4},
		{"决策=已付款", dealexpert.EncounterListParams{Decision: "paid"}, 2},
		{"决策=高意向", dealexpert.EncounterListParams{Decision: "high_intent"}, 4},
		{"已关联客户", dealexpert.EncounterListParams{CustomerLinked: &linked}, 7},
		{"未关联客户", dealexpert.EncounterListParams{CustomerLinked: &unlinked}, 1},
		{"无跟进", dealexpert.EncounterListParams{FollowUpState: "none"}, 8},
		{"关键词=王女士", dealexpert.EncounterListParams{Keyword: "王女士"}, 1},
	}

	failures := 0
	for _, testCase := range cases {
		testCase.params.Page = 1
		testCase.params.PageSize = 50
		items, total, err := service.ListEncounters(ctx, tenantID, testCase.params, nil)
		if err != nil {
			fmt.Printf("%-14s 查询失败: %v\n", testCase.name, err)
			failures++
			continue
		}
		ok := total == testCase.want && len(items) == testCase.want
		status := "PASS"
		if !ok {
			status = "FAIL"
			failures++
		}
		fmt.Printf("%-14s total=%-3d 返回=%-3d 期望=%-3d %s\n", testCase.name, total, len(items), testCase.want, status)
	}

	if failures > 0 {
		fmt.Printf("筛选验收失败：%d 项\n", failures)
		os.Exit(1)
	}
	fmt.Println("筛选验收通过")
}

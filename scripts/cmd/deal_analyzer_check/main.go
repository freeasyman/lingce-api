// 赢客专家分析就绪检查：只读。
// 检查模型配置、提示词、队列状态，用于上线前和提示词写入后的快速自检。
//
//	DATABASE_URL='postgresql://...' go run ./scripts/cmd/deal_analyzer_check
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

	ready := true

	fmt.Println("== 模型配置 ==")
	rows, err := pool.Query(ctx, `
		SELECT tenant_id, provider, model_code, is_default, is_active,
		       COALESCE(model_params->>'temperature', ''), COALESCE(model_params->>'response_format', '')
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND function_type = $1
		ORDER BY tenant_id, is_default DESC`, dealexpert.AnalysisFunctionType)
	if err != nil {
		fmt.Fprintln(os.Stderr, "query model config:", err)
		os.Exit(1)
	}
	count := 0
	for rows.Next() {
		var tenant int64
		var provider, model, temperature, format string
		var isDefault, isActive bool
		if err := rows.Scan(&tenant, &provider, &model, &isDefault, &isActive, &temperature, &format); err != nil {
			fmt.Fprintln(os.Stderr, "scan model config:", err)
			os.Exit(1)
		}
		count++
		fmt.Printf("tenant=%d provider=%s model=%s default=%v active=%v temperature=%s format=%s\n",
			tenant, provider, model, isDefault, isActive, temperature, format)
	}
	rows.Close()
	if count == 0 {
		fmt.Println("缺少模型配置")
		ready = false
	}

	fmt.Println("\n== 提示词 ==")
	var version string
	var active bool
	var promptLen int
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(version, 'v1'), COALESCE(is_active, false),
		       length(COALESCE(system_prompt, '')) + length(COALESCE(user_prompt_template, ''))
		FROM recording_analysis_prompts WHERE code = $1 LIMIT 1`,
		dealexpert.AnalysisPromptCode).Scan(&version, &active, &promptLen)
	if err != nil {
		fmt.Printf("缺少提示词 %s（写入后即可开始分析）\n", dealexpert.AnalysisPromptCode)
		ready = false
	} else {
		fmt.Printf("code=%s version=%s active=%v length=%d\n", dealexpert.AnalysisPromptCode, version, active, promptLen)
		if !active || promptLen == 0 {
			ready = false
		}
	}

	fmt.Println("\n== 队列 ==")
	statusRows, err := pool.Query(ctx, `
		SELECT status, COUNT(*) FROM deal_expert_analyses GROUP BY status ORDER BY status`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "query queue:", err)
		os.Exit(1)
	}
	for statusRows.Next() {
		var status string
		var total int
		_ = statusRows.Scan(&status, &total)
		fmt.Printf("%s=%d ", status, total)
	}
	statusRows.Close()
	fmt.Println()

	if !ready {
		fmt.Println("\n结论：尚未就绪（等待提示词或模型配置）")
		os.Exit(1)
	}
	fmt.Println("\n结论：已就绪")
}

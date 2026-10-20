// 赢客专家回归语料导出：从开发库抽取真实咨询师录音，写到本地 gitignore 目录。
// 不提交含患者信息的原文；只用于本地/内网回归。
//
//	DATABASE_URL='postgresql://...' go run ./scripts/cmd/deal_regression_dataset -limit 8
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/lib/pq"
)

type datasetItem struct {
	RecordingID  int64            `json:"recording_id"`
	TenantID     int64            `json:"tenant_id"`
	Duration     int              `json:"duration_seconds"`
	RecordedAt   string           `json:"recorded_at"`
	DealStatus   string           `json:"confirmed_deal_status,omitempty"`
	AmriText     string           `json:"transcription_text"`
	Segments     []map[string]any `json:"cleaned_segments"`
	SegmentCount int              `json:"segment_count"`
}

func main() {
	limit := flag.Int("limit", 8, "抽取条数")
	minChars := flag.Int("min-chars", 1500, "转写最少字数")
	tenant := flag.Int64("tenant", 0, "限定租户，0 表示不限")
	outDir := flag.String("out", "testdata/deal-analysis", "输出目录")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	query := `
		SELECT id, tenant_id, COALESCE(duration, 0), COALESCE(to_char(recorded_at, 'YYYY-MM-DD HH24:MI'), ''),
		       COALESCE(NULLIF(confirmed_deal_status, ''), ''), COALESCE(transcription_text, ''),
		       COALESCE(cleaned_transcription::text, '[]')
		FROM recordings
		WHERE deleted_at IS NULL
		  AND lower(COALESCE(business_scope, '')) = 'consultant'
		  AND length(COALESCE(transcription_text, '')) >= $1
		  AND ($2 = 0 OR tenant_id = $2)
		ORDER BY recorded_at DESC NULLS LAST
		LIMIT $3`
	rows, err := db.QueryContext(ctx, query, *minChars, *tenant, *limit)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	if err := os.MkdirAll(filepath.Clean(*outDir), 0o755); err != nil {
		panic(err)
	}

	manifest := make([]map[string]any, 0, *limit)
	for rows.Next() {
		var item datasetItem
		var segmentsRaw string
		if err := rows.Scan(&item.RecordingID, &item.TenantID, &item.Duration, &item.RecordedAt,
			&item.DealStatus, &item.AmriText, &segmentsRaw); err != nil {
			panic(err)
		}
		var payload any
		if err := json.Unmarshal([]byte(segmentsRaw), &payload); err == nil {
			raw, _ := json.Marshal(payload)
			_ = json.Unmarshal(raw, &item.Segments)
			if list, ok := payload.([]any); ok {
				item.SegmentCount = len(list)
			}
		}
		path := filepath.Join(*outDir, fmt.Sprintf("rec-%d.json", item.RecordingID))
		body, _ := json.MarshalIndent(item, "", "  ")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			panic(err)
		}
		manifest = append(manifest, map[string]any{
			"recording_id": item.RecordingID,
			"tenant_id":    item.TenantID,
			"duration":     item.Duration,
			"recorded_at":  item.RecordedAt,
			"chars":        len([]rune(item.AmriText)),
			"segments":     item.SegmentCount,
			"file":         filepath.Base(path),
		})
		fmt.Printf("exported rec=%-6d tenant=%-3d chars=%-6d segments=%d\n",
			item.RecordingID, item.TenantID, len([]rune(item.AmriText)), item.SegmentCount)
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}

	manifestBody, _ := json.MarshalIndent(map[string]any{
		"generated_at": time.Now().Format(time.RFC3339),
		"min_chars":    *minChars,
		"items":        manifest,
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(*outDir, "manifest.json"), manifestBody, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("manifest: %s (%d items)\n", filepath.Join(*outDir, "manifest.json"), len(manifest))
}

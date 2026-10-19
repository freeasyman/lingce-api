package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Recomputes stored LLM call cost from the audit usage snapshot. Read-only by
// default; writes a per-request CSV report with stored vs recalculated cost.
func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "PostgreSQL DSN (or DATABASE_URL)")
	out := flag.String("out", "llm-cost-recalc.csv", "CSV report path")
	flag.Parse()
	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "missing -dsn or DATABASE_URL")
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT request_id, COALESCE(model_code,''), COALESCE(total_cost,0), usage_metadata
		FROM gateway_call_records
		WHERE usage_metadata IS NOT NULL AND usage_metadata ? 'pricing_snapshot'
		ORDER BY created_at`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	f, err := os.Create(*out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"request_id", "model_code", "mode", "stored_total", "recalculated_total", "diff"})

	var count, mismatches int
	var storedSum, recalcSum float64
	for rows.Next() {
		var requestID, model string
		var stored float64
		var raw []byte
		if err := rows.Scan(&requestID, &model, &stored, &raw); err != nil {
			panic(err)
		}
		var meta map[string]interface{}
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		mode, recalc := recalculate(meta)
		if mode == "" {
			continue
		}
		count++
		storedSum += stored
		recalcSum += recalc
		diff := recalc - stored
		if diff > 1e-9 || diff < -1e-9 {
			mismatches++
		}
		_ = w.Write([]string{requestID, model, mode, fmt.Sprintf("%.10f", stored), fmt.Sprintf("%.10f", recalc), fmt.Sprintf("%.10f", diff)})
	}
	fmt.Printf("records=%d mismatches=%d stored=%.6f recalculated=%.6f diff=%.6f report=%s\n", count, mismatches, storedSum, recalcSum, recalcSum-storedSum, *out)
}

func recalculate(meta map[string]interface{}) (string, float64) {
	snapshot, _ := meta["pricing_snapshot"].(map[string]interface{})
	if snapshot == nil {
		return "", 0
	}
	mode, _ := snapshot["mode"].(string)
	if mode == "" {
		mode, _ = meta["pricing_mode"].(string)
	}
	rate := func(key string) float64 { v, _ := snapshot[key].(float64); return v }
	num := func(key string) float64 { v, _ := meta[key].(float64); return v }

	switch mode {
	case "text":
		input := num("input_tokens")
		output := num("output_tokens")
		cached := num("cached_input_tokens")
		creation := num("cache_creation_input_tokens")
		uncached := input - cached - creation
		if uncached < 0 {
			uncached = 0
		}
		cachedRate := rate("cached_input_per_1k")
		cacheType, _ := meta["cache_type"].(string)
		if cacheType == "ephemeral" && rate("explicit_cached_input_per_1k") > 0 {
			cachedRate = rate("explicit_cached_input_per_1k")
		}
		creationRate := rate("cache_creation_per_1k")
		cost := uncached/1000*rate("input_per_1k") + cached/1000*cachedRate + creation/1000*creationRate + output/1000*rate("output_per_1k")
		return "text", cost
	case "batch":
		return "batch", num("input_tokens")/1000*rate("batch_input_per_1k") + num("output_tokens")/1000*rate("batch_output_per_1k")
	case "embedding":
		return "embedding", num("input_tokens") / 1000 * rate("input_per_1k")
	case "image_generation":
		return "image_generation", num("input_image_count")*rate("input_per_image") + num("output_image_count")*rate("generation_per_image")
	case "audio", "realtime_audio":
		return mode, num("audio_duration_seconds") * rate("per_second")
	default:
		return "", 0
	}
}

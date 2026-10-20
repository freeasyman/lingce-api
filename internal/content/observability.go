package content

import (
	"context"
	"fmt"
	"time"
)

type GenerationMetric struct {
	FunctionType string  `json:"function_type"`
	Provider     string  `json:"provider"`
	ModelCode    string  `json:"model_code"`
	Total        int64   `json:"total"`
	Success      int64   `json:"success"`
	Failed       int64   `json:"failed"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCost    float64 `json:"total_cost"`
}

type GenerationObservability struct {
	Days          int                `json:"days"`
	Metrics       []GenerationMetric `json:"metrics"`
	RecentFailure []map[string]any   `json:"recent_failures"`
}

func (s *Service) GetGenerationObservability(ctx context.Context, tenantID int64, days int) (*GenerationObservability, error) {
	if days <= 0 || days > 30 {
		days = 7
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT function_type, provider, model_code,
		       COUNT(*), COUNT(*) FILTER (WHERE success), COUNT(*) FILTER (WHERE NOT success),
		       COALESCE(AVG(latency_ms),0), COALESCE(SUM(total_tokens),0), COALESCE(SUM(total_cost),0)
		FROM gateway_call_records
		WHERE tenant_id=$1
		  AND created_at >= NOW() - ($2 * INTERVAL '1 day')
		  AND (function_type LIKE 'content_%' OR function_type='ai_assistant')
		GROUP BY function_type, provider, model_code
		ORDER BY COUNT(*) DESC
	`, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("query generation metrics: %w", err)
	}
	defer rows.Close()
	metrics := make([]GenerationMetric, 0, 12)
	for rows.Next() {
		var m GenerationMetric
		if err := rows.Scan(&m.FunctionType, &m.Provider, &m.ModelCode, &m.Total, &m.Success, &m.Failed, &m.AvgLatencyMS, &m.TotalTokens, &m.TotalCost); err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	failureRows, err := s.store.pool.Query(ctx, `
		SELECT created_at, function_type, provider, model_code, COALESCE(error_code,''), COALESCE(error_message,''), latency_ms
		FROM gateway_call_records
		WHERE tenant_id=$1 AND NOT success
		  AND created_at >= NOW() - ($2 * INTERVAL '1 day')
		  AND (function_type LIKE 'content_%' OR function_type='ai_assistant')
		ORDER BY created_at DESC LIMIT 20
	`, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("query generation failures: %w", err)
	}
	defer failureRows.Close()
	failures := make([]map[string]any, 0, 20)
	for failureRows.Next() {
		var createdAt time.Time
		var functionType, provider, modelCode, errorCode, errorMessage string
		var latencyMS int64
		if err := failureRows.Scan(&createdAt, &functionType, &provider, &modelCode, &errorCode, &errorMessage, &latencyMS); err != nil {
			return nil, err
		}
		failures = append(failures, map[string]any{
			"created_at": createdAt, "function_type": functionType, "provider": provider,
			"model_code": modelCode, "error_code": errorCode, "error": errorMessage, "latency_ms": latencyMS,
		})
	}
	return &GenerationObservability{Days: days, Metrics: metrics, RecentFailure: failures}, nil
}

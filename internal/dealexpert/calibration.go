package dealexpert

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ForecastCalibration 预判金额 vs 实际金额校准。
type ForecastCalibration struct {
	Rows       []CalibrationRow `json:"rows"`
	Stats      CalibrationStats `json:"stats"`
	WindowDays int              `json:"window_days"`
}

type CalibrationRow struct {
	RecordingID  int64   `json:"recording_id"`
	CustomerName string  `json:"customer_name"`
	ProductName  string  `json:"product_name"`
	Estimated    float64 `json:"estimated"`
	Actual       float64 `json:"actual"`
}

type CalibrationStats struct {
	ActualCount    int     `json:"actual_count"`
	ForecastCount  int     `json:"forecast_count"`
	Coverage       float64 `json:"coverage"`
	MeanAbsError   float64 `json:"mean_abs_error"`
	MedianAbsError float64 `json:"median_abs_error"`
}

func (s *Service) GetForecastCalibration(ctx context.Context, tenantID int64) (*ForecastCalibration, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT a.recording_id, COALESCE(c.name, ''), COALESCE(p.name, ''),
		       COALESCE(p.price::float8, 0) AS estimated,
		       COALESCE(os.net, 0) AS actual
		FROM (
			SELECT DISTINCT ON (recording_id) recording_id, tenant_id, analysis, id
			FROM consultation_analyses
			WHERE tenant_id = $1 AND status = 'analyzed' AND analysis IS NOT NULL
			  AND analyzed_at >= NOW() - INTERVAL '90 days'
			ORDER BY recording_id, id DESC
		) a
		JOIN recordings r ON r.id = a.recording_id AND r.tenant_id = a.tenant_id
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(CASE WHEN type IN ('付款','定金') THEN amount ELSE 0 END)
				- SUM(CASE WHEN type = '退款' THEN amount ELSE 0 END), 0) AS net
			FROM deal_outcome_events e
			WHERE e.tenant_id = a.tenant_id AND e.encounter_id = r.encounter_id AND e.voided_at IS NULL
		) os ON true
		LEFT JOIN LATERAL (
			SELECT id, name, price FROM products
			WHERE id = COALESCE((a.analysis->'intent_project'->>'product_id')::bigint, 0)
			  AND deleted_at IS NULL
			LIMIT 1
		) p ON true
		ORDER BY a.id DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := &ForecastCalibration{WindowDays: 90, Rows: []CalibrationRow{}}
	errors := []float64{}
	for rows.Next() {
		var row CalibrationRow
		if err := rows.Scan(&row.RecordingID, &row.CustomerName, &row.ProductName, &row.Estimated, &row.Actual); err != nil {
			return nil, err
		}
		out.Rows = append(out.Rows, row)
		if row.Actual > 0 {
			out.Stats.ActualCount++
			if row.Estimated > 0 {
				out.Stats.ForecastCount++
				errors = append(errors, math.Abs(row.Estimated-row.Actual))
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out.Stats.ActualCount > 0 {
		out.Stats.Coverage = float64(out.Stats.ForecastCount) / float64(out.Stats.ActualCount)
	}
	if len(errors) > 0 {
		sort.Float64s(errors)
		sum := 0.0
		for _, value := range errors {
			sum += value
		}
		out.Stats.MeanAbsError = sum / float64(len(errors))
		out.Stats.MedianAbsError = errors[len(errors)/2]
	}
	if len(out.Rows) > 30 {
		out.Rows = out.Rows[:30]
	}
	_ = json.Valid([]byte("{}"))
	return out, nil
}

func (h *Handler) GetForecastCalibration(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	result, err := h.service.GetForecastCalibration(r.Context(), scope.TenantID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

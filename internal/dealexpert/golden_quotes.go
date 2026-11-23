package dealexpert

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type goldenQuoteItem struct {
	RecordingID    int64               `json:"recording_id"`
	EmployeeID     int64               `json:"employee_id"`
	EmployeeName   string              `json:"employee_name"`
	RecordedAt     string              `json:"recorded_at,omitempty"`
	Topic          string              `json:"topic"`
	Summary        string              `json:"summary"`
	ContentAngle   string              `json:"content_angle"`
	PatientConcern string              `json:"patient_concern"`
	ViralPotential string              `json:"viral_potential"`
	KeyQuotes      []map[string]string `json:"key_quotes"`
}

// ListGoldenQuotes 返回赢客专家新分析产出的「金句片段」。
// 数据源是新分析 content_seeds(type=golden_quote)，不再是旧的 benchmark_clips。
func (h *Handler) ListGoldenQuotes(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT
			r.id,
			COALESCE(r.employee_id, 0),
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), ''),
			COALESCE(r.recorded_at, r.created_at),
			COALESCE(s->>'topic', ''),
			COALESCE(s->>'summary', ''),
			COALESCE(s->>'content_angle', ''),
			COALESCE(s->>'patient_concern', ''),
			COALESCE(s->>'viral_potential', ''),
			COALESCE(s->'key_quotes', '[]'::jsonb)
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id AND e.tenant_id = r.tenant_id
		JOIN LATERAL jsonb_array_elements(
			COALESCE(
				r.analysis_result::jsonb->'content_seeds',
				r.analysis_result::jsonb->'raw'->'consultant_conversion_analysis_v1'->'content_seeds',
				'[]'::jsonb
			)
		) AS s ON TRUE
		WHERE r.tenant_id = $1
		  AND r.deleted_at IS NULL
		  AND COALESCE(lower(r.business_scope), '') = 'consultant'
		  AND s->>'type' IN ('golden_quote', '金句片段', '金句表达', 'quote')
		  AND ($2::bigint[] IS NULL OR r.employee_id = ANY($2))
		ORDER BY COALESCE(r.recorded_at, r.created_at) DESC NULLS LAST, r.id DESC
	`, scope.TenantID, scope.EmployeeIDs)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_GOLDEN_QUOTES_FAILED", err.Error(), nil)
		return
	}
	defer rows.Close()

	items := make([]goldenQuoteItem, 0)
	for rows.Next() {
		var item goldenQuoteItem
		var recordedAt time.Time
		var keyQuotesRaw []byte
		if err := rows.Scan(
			&item.RecordingID, &item.EmployeeID, &item.EmployeeName, &recordedAt,
			&item.Topic, &item.Summary, &item.ContentAngle, &item.PatientConcern,
			&item.ViralPotential, &keyQuotesRaw,
		); err != nil {
			continue
		}
		item.RecordedAt = recordedAt.Format(time.RFC3339)
		item.KeyQuotes = []map[string]string{}
		if len(keyQuotesRaw) > 0 {
			_ = json.Unmarshal(keyQuotesRaw, &item.KeyQuotes)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_GOLDEN_QUOTES_FAILED", err.Error(), nil)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": len(items)})
}

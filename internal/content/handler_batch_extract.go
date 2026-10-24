package content

import (
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ExtractInsightsBatch 批量补齐：对本租户「有转写但还没灵感」的录音跑灵感提取（不生成选题）。
// 一次最多处理 limit 条（默认 3，上限 20），返回逐条结果。
func (h *Handler) ExtractInsightsBatch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	limit := 3
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 20 {
		limit = 20
	}

	pending, err := h.service.store.listPendingTranscripts(r.Context(), tenantID, limit)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	type item struct {
		RecordingID int64  `json:"recording_id"`
		Insights    int    `json:"insights"`
		Error       string `json:"error,omitempty"`
	}
	results := make([]item, 0, len(pending))
	for _, p := range pending {
		n, err := h.service.AnalyzeRecording(r.Context(), tenantID, p.RecordingID)
		if err != nil {
			results = append(results, item{RecordingID: p.RecordingID, Error: err.Error()})
			continue
		}
		results = append(results, item{RecordingID: p.RecordingID, Insights: n})
	}
	httputil.WriteSuccess(w, map[string]any{
		"processed": len(results),
		"pending":   len(results),
		"items":     results,
	})
}

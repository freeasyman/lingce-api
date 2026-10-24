package content

import (
	"net/http"
	"strconv"
	"time"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ExtractInsightsBatch 手动批量补齐：对本租户「有转写但还没洞察」的录音跑分析+生成选题，并记台账。
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
	done, failed, insights, topics := h.service.runAnalysisBatch(r.Context(), tenantID, "manual", pending)
	httputil.WriteSuccess(w, map[string]any{
		"processed": done,
		"failed":    failed,
		"insights":  insights,
		"topics":    topics,
	})
}

// AnalysisStatus 补齐进度：待补齐条数 + 最近批次台账。
func (h *Handler) AnalysisStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	pending, err := h.service.store.countPendingTranscripts(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	runs, err := h.service.store.listAnalysisRuns(r.Context(), tenantID, 10)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	// 今天累计
	var todayInsights, todayRuns int
	for _, run := range runs {
		if run.StartedAt.Format("2006-01-02") == time.Now().Format("2006-01-02") {
			todayRuns++
			todayInsights += run.Insights
		}
	}
	httputil.WriteSuccess(w, map[string]any{
		"pending":        pending,
		"runs":           runs,
		"today_runs":     todayRuns,
		"today_insights": todayInsights,
	})
}

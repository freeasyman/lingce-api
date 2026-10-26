package content

import (
	"encoding/json"
	"net/http"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// AnalyzeAIVisibility 分析"患者问题"在多个 AI 引擎的可见性（多引擎 + 缺口诊断）。
func (h *Handler) AnalyzeAIVisibility(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Question string `json:"question"`
		Region   string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	result, err := h.service.AnalyzeAIVisibility(r.Context(), tenantID, req.Question, req.Region)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

// ListAIVisibilityRuns 历史诊断列表。
func (h *Handler) ListAIVisibilityRuns(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	limit := 20
	items, err := h.service.ListAIVisibilityRuns(r.Context(), tenantID, limit)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

// GenerateFromAIVisibility 基于缺口诊断生成内容。
func (h *Handler) GenerateFromAIVisibility(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		RunID  int64  `json:"run_id"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RunID <= 0 {
		httputil.WriteBadRequest(w, "run_id is required")
		return
	}
	result, err := h.service.GenerateFromAIVisibility(r.Context(), tenantID, req.RunID, req.Target)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

package content

import (
	"encoding/json"
	"net/http"
	"strconv"

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
		Question  string `json:"question"`
		Region    string `json:"region"`
		InsightID int64  `json:"insight_id"`
		TopicID   int64  `json:"topic_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	runID, err := h.service.StartAIVisibility(r.Context(), tenantID, req.Question, req.Region, req.InsightID, req.TopicID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"run_id": runID, "started": true})
}

// GetAIVisibilityRun 查单条诊断（轮询用）。
func (h *Handler) GetAIVisibilityRun(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid run id")
		return
	}
	run, err := h.service.store.getAIVisibilityRun(r.Context(), tenantID, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if run == nil {
		httputil.WriteNotFound(w, "诊断记录不存在")
		return
	}
	httputil.WriteSuccess(w, run)
}

// ListAIVisibilityRuns 历史诊断列表。
func (h *Handler) ListAIVisibilityRuns(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	limit, offset := 20, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	deleted := r.URL.Query().Get("deleted") == "1"
	items, total, err := h.service.ListAIVisibilityRuns(r.Context(), tenantID, limit, offset, deleted)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
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

// ListCandidateQuestions 候选问题（服务端搜索 + 分页）。
func (h *Handler) ListCandidateQuestions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	limit, offset := 30, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	kw := r.URL.Query().Get("q")
	items, total, err := h.service.ListCandidateQuestions(r.Context(), tenantID, kw, limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

// DeleteAIVisibilityRuns 软删除诊断历史（支持批量）。
func (h *Handler) DeleteAIVisibilityRuns(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		httputil.WriteBadRequest(w, "请至少选择一条记录")
		return
	}
	n, err := h.service.DeleteAIVisibilityRuns(r.Context(), tenantID, body.IDs)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"deleted": n})
}

// RestoreAIVisibilityRuns 从回收站恢复。
func (h *Handler) RestoreAIVisibilityRuns(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		httputil.WriteBadRequest(w, "请至少选择一条记录")
		return
	}
	n, err := h.service.RestoreAIVisibilityRuns(r.Context(), tenantID, body.IDs)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"restored": n})
}

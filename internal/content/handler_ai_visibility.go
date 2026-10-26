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

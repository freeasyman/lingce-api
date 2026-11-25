package content

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// StartPublicFreeCheckup 公共"免费体检"创建任务：匿名可调，独立产品（tenant 0）。
func (h *Handler) StartPublicFreeCheckup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgName    string `json:"org_name"`
		Region     string `json:"region"`
		Question   string `json:"question"`
		DoctorName string `json:"doctor_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	runID, err := h.service.StartAIVisibilityPublic(r.Context(), req.OrgName, req.DoctorName, req.Question, req.Region)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"run_id": runID, "started": true})
}

// GetPublicFreeCheckup 公共"免费体检"轮询：匿名可调，返回进度与引擎结果。
func (h *Handler) GetPublicFreeCheckup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid run id")
		return
	}
	run, err := h.service.store.getAIVisibilityRun(r.Context(), PublicCheckupTenantID, id)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if run == nil {
		httputil.WriteNotFound(w, "诊断记录不存在")
		return
	}
	httputil.WriteSuccess(w, run)
}

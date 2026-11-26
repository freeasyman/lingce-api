package content

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	if !checkupLimiter.allow(clientKey(r), 5, time.Hour) {
		freeCheckupTooMany(w, "请求过于频繁，请稍后再试")
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

// freeCheckupTooMany 简易 429 响应。
func freeCheckupTooMany(w http.ResponseWriter, msg string) {
	httputil.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", msg, nil)
}

// SubmitFreeCheckupLead 公共线索提交：手机号 + 场景，落 free_checkup_leads。
func (h *Handler) SubmitFreeCheckupLead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid run id")
		return
	}
	if !checkupLimiter.allow(clientKey(r), 10, time.Hour) {
		freeCheckupTooMany(w, "提交过于频繁，请稍后再试")
		return
	}
	var req struct {
		Phone string `json:"phone"`
		Scene string `json:"scene"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	phone := strings.TrimSpace(req.Phone)
	if len(phone) != 11 || !strings.HasPrefix(phone, "1") {
		httputil.WriteBadRequest(w, "手机号格式不正确")
		return
	}
	run, err := h.service.store.getAIVisibilityRun(r.Context(), PublicCheckupTenantID, id)
	if err != nil || run == nil {
		httputil.WriteNotFound(w, "体检记录不存在")
		return
	}
	leadID, err := h.service.store.InsertFreeCheckupLead(r.Context(), &FreeCheckupLead{
		RunID:      id,
		OrgName:    run.OrgName,
		Region:     run.Region,
		Question:   run.Question,
		Phone:      phone,
		Scene:      strings.TrimSpace(req.Scene),
		Status:     "new",
	})
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"lead_id": leadID})
}

// ListFreeCheckupLeadsForOperation 运营控制台：线索列表（可按状态筛选）。
func (h *Handler) ListFreeCheckupLeadsForOperation(w http.ResponseWriter, r *http.Request) {
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
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	items, total, err := h.service.store.ListFreeCheckupLeads(r.Context(), status, limit, offset)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

// GetFreeCheckupLeadForOperation 运营控制台：线索详情（含 run 的引擎与缺口）。
func (h *Handler) GetFreeCheckupLeadForOperation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid lead id")
		return
	}
	lead, err := h.service.store.GetFreeCheckupLead(r.Context(), id)
	if err != nil || lead == nil {
		httputil.WriteNotFound(w, "线索不存在")
		return
	}
	run, _ := h.service.store.getAIVisibilityRun(r.Context(), PublicCheckupTenantID, lead.RunID)
	httputil.WriteSuccess(w, map[string]any{"lead": lead, "run": run})
}

// UpdateFreeCheckupLeadStatusForOperation 运营控制台：更新处理状态/备注。
func (h *Handler) UpdateFreeCheckupLeadStatusForOperation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid lead id")
		return
	}
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	status := strings.TrimSpace(req.Status)
	switch status {
	case "new", "contacted", "converted", "invalid":
	default:
		httputil.WriteBadRequest(w, "status 只能是 new/contacted/converted/invalid")
		return
	}
	if err := h.service.store.UpdateFreeCheckupLeadStatus(r.Context(), id, status, req.Note); err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}

// FreeCheckupStatsForOperation 运营控制台：统计。
func (h *Handler) FreeCheckupStatsForOperation(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.store.FreeCheckupLeadStats(r.Context())
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, stats)
}

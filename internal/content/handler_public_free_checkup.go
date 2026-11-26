package content

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/geo"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// StartPublicFreeCheckup 公共"免费体检"创建任务：匿名可调，独立产品（tenant 0）。
func (h *Handler) StartPublicFreeCheckup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgName    string `json:"org_name"`
		Region     string `json:"region"`
		Question   string `json:"question"`
		DoctorName string `json:"doctor_name"`
		Scene      string `json:"scene"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if !checkupLimiter.allow(clientKey(r), 5, time.Hour) {
		freeCheckupTooMany(w, "请求过于频繁，请稍后再试")
		return
	}
	sid := h.ensureSID(w, r)
	deviceID := strings.TrimSpace(r.Header.Get("X-Device-Id"))
	ip := requestIP(r)
	bySID, byDevice, byIP, err := h.service.store.FreeCheckupQuotaUsage(r.Context(), sid, deviceID, ip)
	if err != nil {
		writeContentError(w, err)
		return
	}
	used := bySID
	if byDevice > used {
		used = byDevice
	}
	if used >= freeCheckupDailyLimit {
		httputil.WriteError(w, http.StatusTooManyRequests, "QUOTA_EXCEEDED", "今日免费体检次数已用完", nil)
		return
	}
	if byIP >= freeCheckupIPDailyCap {
		httputil.WriteError(w, http.StatusTooManyRequests, "IP_LIMITED", "当前网络体检次数过多，请稍后再试", nil)
		return
	}

	// 24 小时内同机构+同问题：直接复用上次结果（不调用模型、不扣次数）
	if cachedID, hit, cerr := h.service.store.FindFreeCheckupCachedRun(r.Context(),
		strings.TrimSpace(req.OrgName), strings.TrimSpace(req.Region), strings.TrimSpace(req.Question)); cerr == nil && hit {
		_ = h.service.store.InsertFreeCheckupUsage(r.Context(), cachedID, sid,
			strings.TrimSpace(r.Header.Get("X-Device-Id")), ip, geo.City(ip),
			r.UserAgent(), strings.TrimSpace(req.Scene), strings.TrimSpace(req.OrgName),
			strings.TrimSpace(req.Region), strings.TrimSpace(req.Question), true)
		httputil.WriteSuccess(w, map[string]any{"run_id": cachedID, "started": false, "cached": true})
		return
	}

	runID, err := h.service.StartAIVisibilityPublic(r.Context(), req.OrgName, req.DoctorName, req.Question, req.Region)
	if err != nil {
		writeContentError(w, err)
		return
	}
	// 使用记录：无手机号也记，用于成本与使用量统计
	_ = h.service.store.InsertFreeCheckupUsage(r.Context(), runID, sid,
		strings.TrimSpace(r.Header.Get("X-Device-Id")), ip, geo.City(ip),
		r.UserAgent(), strings.TrimSpace(req.Scene), strings.TrimSpace(req.OrgName),
		strings.TrimSpace(req.Region), strings.TrimSpace(req.Question), false)
	httputil.WriteSuccess(w, map[string]any{"run_id": runID, "started": true})
}

// GetPublicFreeCheckupQuota 公共"免费体检"额度：签发/读取签名 Cookie，返回今日剩余次数。
func (h *Handler) GetPublicFreeCheckupQuota(w http.ResponseWriter, r *http.Request) {
	sid := h.ensureSID(w, r)
	deviceID := strings.TrimSpace(r.Header.Get("X-Device-Id"))
	bySID, byDevice, _, err := h.service.store.FreeCheckupQuotaUsage(r.Context(), sid, deviceID, "")
	if err != nil {
		writeContentError(w, err)
		return
	}
	used := bySID
	if byDevice > used {
		used = byDevice
	}
	remaining := freeCheckupDailyLimit - used
	if remaining < 0 {
		remaining = 0
	}
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(24 * time.Hour)
	httputil.WriteSuccess(w, map[string]any{
		"limit":     freeCheckupDailyLimit,
		"used":      used,
		"remaining": remaining,
		"reset_at":  next.Format(time.RFC3339),
	})
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
		Phone        string `json:"phone"`
		Scene        string `json:"scene"`
		DoctorName   string `json:"doctor_name"`
		ContactName  string `json:"contact_name"`
		ContactTitle string `json:"contact_title"`
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
		DoctorName: strings.TrimSpace(req.DoctorName),
		ContactName:  strings.TrimSpace(req.ContactName),
		ContactTitle: strings.TrimSpace(req.ContactTitle),
		DeviceID:   strings.TrimSpace(r.Header.Get("X-Device-Id")),
		IP:         requestIP(r),
		IPCity:     geo.City(requestIP(r)),
		UserAgent:  r.UserAgent(),
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
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	items, total, err := h.service.store.ListFreeCheckupLeads(r.Context(), status, q, limit, offset)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

// ListFreeCheckupUsageForOperation 运营控制台：体检使用记录（含未留手机号）。
func (h *Handler) ListFreeCheckupUsageForOperation(w http.ResponseWriter, r *http.Request) {
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
	leadFilter := strings.TrimSpace(r.URL.Query().Get("lead"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	items, total, err := h.service.store.ListFreeCheckupUsage(r.Context(), leadFilter, q, limit, offset)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

// ConvertFreeCheckupLeadForOperation 运营控制台：创建机构成功后回写线索（tenant_id + converted）。
func (h *Handler) ConvertFreeCheckupLeadForOperation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid lead id")
		return
	}
	var req struct {
		TenantID int64 `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}
	ok, err := h.service.store.ConvertFreeCheckupLead(r.Context(), id, req.TenantID)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if !ok {
		httputil.WriteError(w, http.StatusConflict, "ALREADY_CONVERTED", "该线索已关联机构，不能重复回写", nil)
		return
	}
	lead, err := h.service.store.GetFreeCheckupLead(r.Context(), id)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"lead": lead, "tenant_id": req.TenantID})
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
	updated, err := h.service.store.UpdateFreeCheckupLeadStatus(r.Context(), id, status, req.Note)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if !updated {
		httputil.WriteNotFound(w, "线索不存在")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}

// FreeCheckupStatsForOperation 运营控制台：统计（线索状态 + 使用量）。
func (h *Handler) FreeCheckupStatsForOperation(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.store.FreeCheckupLeadStats(r.Context())
	if err != nil {
		writeContentError(w, err)
		return
	}
	if usage, uerr := h.service.store.FreeCheckupUsageStats(r.Context()); uerr == nil {
		for k, v := range usage {
			stats[k] = v
		}
	}
	httputil.WriteSuccess(w, stats)
}

// requestIP 取客户端 IP（优先 X-Forwarded-For 第一段）。
func requestIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return xff
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

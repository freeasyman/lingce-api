package content

import (
	"strings"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func tenantOf(r *http.Request) (int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		return 0, false
	}
	id, err := tenancy.RequireTenantID(claims, "")
	if err != nil {
		return 0, false
	}
	return id, true
}

func (h *Handler) ListMediaAccountsV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	accounts, err := h.service.store.listMediaAccounts(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, accounts)
}

func (h *Handler) CreateMediaAccountV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Platform      string `json:"platform"`
		Name          string `json:"name"`
		Role          string `json:"role"`
		MonthlyTarget *int   `json:"monthly_target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Platform == "" || req.Name == "" || req.Role == "" {
		httputil.WriteBadRequest(w, "platform/name/role are required")
		return
	}
	// 发布量默认：公众号 8 / 小红书 8 / 官网 2 / 其他 4
	if req.MonthlyTarget == nil || *req.MonthlyTarget <= 0 {
		d := 4
		switch req.Platform {
		case "wechat", "xiaohongshu":
			d = 8
		case "website":
			d = 2
		}
		req.MonthlyTarget = &d
	}
	id, err := h.service.store.insertMediaAccount(r.Context(), tenantID, req.Platform, req.Name, req.Role, req.MonthlyTarget)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]int64{"id": id})
}

func (h *Handler) ListInsightsV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	employeeID, _ := strconv.ParseInt(q.Get("employee_id"), 10, 64)
	departmentCode := q.Get("department_code")
	status := q.Get("status")

	items, total, employees, departments, err := h.service.store.listInsightsFiltered(r.Context(), tenantID, insightListFilter{
		Type:           q.Get("type"),
		EmployeeID:     employeeID,
		DepartmentCode: departmentCode,
		Status:         status,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{
		"items":       items,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
		"employees":   employees,
		"departments": departments,
	})
}

// SetInsightStatusV2 设置灵感处理状态（new=待处理 / dismissed=已忽略）。
func (h *Handler) SetInsightStatusV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid insight id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Status != "new" && req.Status != "dismissed" {
		httputil.WriteBadRequest(w, "status must be new or dismissed")
		return
	}
	if err := h.service.store.setInsightStatus(r.Context(), tenantID, id, req.Status); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"id": id, "status": req.Status})
}

// ListQuestionClusters 反复问聚合。
func (h *Handler) ListQuestionClusters(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	minCount := 2
	if v := r.URL.Query().Get("min"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 2 {
			minCount = n
		}
	}
	clusters, err := h.service.store.listQuestionClusters(r.Context(), tenantID, minCount)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, clusters)
}

// RebuildQuestionClusters 重建反复问簇（LLM 归一化）。
func (h *Handler) RebuildQuestionClusters(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	n, err := h.service.RebuildQuestionClusters(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]int{"clusters": n})
}

// RecommendTopics 推荐打分（反复问频次）。
func (h *Handler) RecommendTopics(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	limit := 3
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	items, err := h.service.RecommendTopics(r.Context(), tenantID, limit)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) ListTopicsV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	topics, err := h.service.store.listTopicsV2(r.Context(), tenantID, 200)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, topics)
}

func (h *Handler) ListContentItemsV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	deleted := r.URL.Query().Get("deleted") == "true"
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := h.service.store.listContentItemsV2(r.Context(), tenantID, 200, deleted, q)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) ListStyleTemplatesV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tplType := r.URL.Query().Get("type")
	templates, err := h.service.store.listStyleTemplates(r.Context(), tenantID, tplType)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, templates)
}

func (h *Handler) GeneratePlanV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Period    string `json:"period"`
		StartDate string `json:"start_date"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	plan, err := h.service.GeneratePlan(r.Context(), tenantID, req.Period, req.StartDate)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, plan)
}

func (h *Handler) ListPlanItemsV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	planPeriodID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid plan period id")
		return
	}
	items, err := h.service.store.listPlanItemsV2(r.Context(), tenantID, planPeriodID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) ScheduleTopicV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	planPeriodID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid plan period id")
		return
	}
	var req struct {
		AccountID   int64   `json:"account_id"`
		TopicID     int64   `json:"topic_id"`
		PlannedDate *string `json:"planned_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	id, err := h.service.ScheduleTopic(r.Context(), tenantID, planPeriodID, req.AccountID, req.TopicID, req.PlannedDate)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]int64{"id": id})
}

// PublishContentV2 发布内容并回填计划。
func (h *Handler) PublishContentV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	contentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid content id")
		return
	}
	if err := h.service.PublishContentV2(r.Context(), tenantID, contentID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]bool{"published": true})
}

// ReviewContentV2 标记内容已审核（半自动：一键复制/导出→医生确认→点已审核）。
func (h *Handler) ReviewContentV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	contentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid content id")
		return
	}
	if err := h.service.store.updateContentItemV2Status(r.Context(), tenantID, contentID, "reviewed"); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]bool{"reviewed": true})
}

// GetReportData 周报/月报数据聚合。
func (h *Handler) GetReportData(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	since := time.Now().AddDate(0, 0, -days)
	data, err := h.service.store.GetReportData(r.Context(), tenantID, since)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, data)
}

// ExportReport 导出周报/月报 Word + PPT。
func (h *Handler) ExportReport(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	result, err := h.service.ExportReport(r.Context(), tenantID, days)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

// GetCurrentPlan 取某个月现有计划（无则返回 null）。
func (h *Handler) GetCurrentPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	period := r.URL.Query().Get("period")
	startDate := r.URL.Query().Get("start_date")
	plan, err := h.service.GetPlan(r.Context(), tenantID, period, startDate)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, plan)
}

// ListPlanPeriods 列出计划周期（含完成度）。
func (h *Handler) ListPlanPeriods(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	periods, err := h.service.store.listPlanPeriods(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, periods)
}

// GetContentItemV2 内容详情。
func (h *Handler) GetContentItemV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid content id")
		return
	}
	item, err := h.service.store.getContentItemV2(r.Context(), tenantID, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "content not found")
		return
	}
	httputil.WriteSuccess(w, item)
}

// DeleteContentItemV2 软删除（进回收站，数据保留）。
func (h *Handler) DeleteContentItemV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid content id")
		return
	}
	if err := h.service.store.softDeleteContentItemV2(r.Context(), tenantID, id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"deleted": true})
}

// RestoreContentItemV2 从回收站恢复。
func (h *Handler) RestoreContentItemV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid content id")
		return
	}
	if err := h.service.store.restoreContentItemV2(r.Context(), tenantID, id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"restored": true})
}

// HardDeleteContentItemV2 彻底删除（仅回收站里的可删）。
func (h *Handler) HardDeleteContentItemV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid content id")
		return
	}
	if err := h.service.store.hardDeleteContentItemV2(r.Context(), tenantID, id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"purged": true})
}

// GetOrCreateContentItemV2 按选题取或建内容（已有就返回原 id，绝不覆盖正文/图片）。
func (h *Handler) GetOrCreateContentItemV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		TopicID int64  `json:"topic_id"`
		Title   string `json:"title"`
		Form    string `json:"form"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.TopicID <= 0 {
		httputil.WriteBadRequest(w, "topic_id is required")
		return
	}
	id, existed, err := h.service.store.getOrCreateContentItemByTopic(r.Context(), tenantID, req.TopicID, req.Title, req.Form)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"id": id, "existed": existed})
}

// UpdateContentItemV2 编辑内容。
func (h *Handler) UpdateContentItemV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid content id")
		return
	}
	var req struct {
		Form           *string         `json:"form"`
		Title          *string         `json:"title"`
		Body           *string         `json:"body"`
		PublishPackage json.RawMessage `json:"publish_package"`
		Images         json.RawMessage `json:"images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	existing, err := h.service.store.getContentItemV2(r.Context(), tenantID, id)
	if err != nil || existing == nil {
		httputil.WriteNotFound(w, "content not found")
		return
	}
	title, body := existing.Title, existing.Body
	if req.Title != nil {
		title = *req.Title
	}
	if req.Body != nil {
		body = *req.Body
	}
	pp := req.PublishPackage
	if len(pp) == 0 {
		pp = existing.PublishPackage
	}
	img := req.Images
	if len(img) == 0 {
		img = existing.Images
	}
	form := ""
	if req.Form != nil {
		form = normalizeFormCode(*req.Form)
	}
	if err := h.service.store.updateContentItemV2(r.Context(), tenantID, id, form, title, body, pp, img); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]bool{"updated": true})
}

func (h *Handler) registerV2Routes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/content-media-accounts", Handler: h.ListMediaAccountsV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-media-accounts", Handler: h.CreateMediaAccountV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-insights", Handler: h.ListInsightsV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-insights/{id}", Handler: h.GetInsightV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-insights/clusters", Handler: h.ListQuestionClusters, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-insights/{id}/status", Handler: h.SetInsightStatusV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-insights/clusters/actions/rebuild", Handler: h.RebuildQuestionClusters, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-topics", Handler: h.ListTopicsV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-topics/recommendations", Handler: h.RecommendTopics, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-items", Handler: h.ListContentItemsV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-items/{id}", Handler: h.GetContentItemV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/content-items/{id}", Handler: h.UpdateContentItemV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/content-items/{id}", Handler: h.DeleteContentItemV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/restore", Handler: h.RestoreContentItemV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/actions/get-or-create", Handler: h.GetOrCreateContentItemV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/content-items/{id}/permanent", Handler: h.HardDeleteContentItemV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-style-templates", Handler: h.ListStyleTemplatesV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-org-knowledge", Handler: h.GetOrgKnowledge, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-org-knowledge", Handler: h.UpsertOrgKnowledge, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-assets/actions/upload", Handler: h.UploadContentAsset, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-plans", Handler: h.ListPlanPeriods, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-plans/current", Handler: h.GetCurrentPlan, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-plans/actions/generate", Handler: h.GeneratePlanV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-plans/{id}/items", Handler: h.ListPlanItemsV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-plans/{id}/items", Handler: h.ScheduleTopicV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/publish", Handler: h.PublishContentV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/{id}/actions/review", Handler: h.ReviewContentV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-reports", Handler: h.GetReportData, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-reports/actions/export", Handler: h.ExportReport, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}

// GetOrgKnowledge 取知识库/规则。
func (h *Handler) GetOrgKnowledge(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	k, err := h.service.store.getOrgKnowledge(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, k)
}

// UpsertOrgKnowledge 保存知识库/规则。
func (h *Handler) UpsertOrgKnowledge(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var k orgKnowledge
	if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := h.service.store.upsertOrgKnowledge(r.Context(), tenantID, &k); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]bool{"saved": true})
}




// GetInsightV2 洞察详情。
func (h *Handler) GetInsightV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid insight id")
		return
	}
	ins, err := h.service.store.getInsightV2(r.Context(), tenantID, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if ins == nil {
		httputil.WriteNotFound(w, "insight not found")
		return
	}
	httputil.WriteSuccess(w, ins)
}

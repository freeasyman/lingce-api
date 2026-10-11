package complianceguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/compliancepermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	service     *Service
	permissions *compliancepermission.Service
	logMu       sync.Mutex
	logPath     string
}

const communicationCheckLogPath = "/Users/yiliiang/Documents/lingce-web/apps/compliance/data/log/communication-check.log"

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
		logPath: communicationCheckLogPath,
	}
}

func (h *Handler) SetPermissionService(service *compliancepermission.Service) {
	h.permissions = service
}

func (h *Handler) requireAbility(w http.ResponseWriter, r *http.Request, ability string) (*auth.Claims, int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return nil, 0, false
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return nil, 0, false
	}
	if h.permissions == nil {
		httputil.WriteInternalError(w, "compliance permissions are not configured")
		return nil, 0, false
	}
	if _, err := h.permissions.Authorize(r.Context(), claims, tenantID, ability); err != nil {
		httputil.WriteForbidden(w, "compliance permission required")
		return nil, 0, false
	}
	return claims, tenantID, true
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "POST", Path: "/api/v1/compliance/debug/analyze-text", Handler: h.AnalyzeText, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/compliance/findings", Handler: h.ListFindings, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/compliance/check-items", Handler: h.ListCheckItems, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/compliance/check-items/{id}", Handler: h.GetCheckItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/compliance/check-items/{id}/start", Handler: h.StartCheckItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/compliance/check-items/{id}/process", Handler: h.ProcessCheckItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/compliance/findings/{id}", Handler: h.GetFinding, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/compliance/findings/{id}/review", Handler: h.ReviewFinding, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/compliance/overview", Handler: h.Overview, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/compliance/rules", Handler: h.ListRules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/compliance/rules/{id}", Handler: h.GetRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/compliance/rules", Handler: h.CreateRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/compliance/rules/{id}", Handler: h.UpdateRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/compliance/rules/{id}/enable", Handler: h.EnableRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/compliance/rules/{id}/disable", Handler: h.DisableRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/compliance/rules/{id}", Handler: h.DeleteRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{
			Method:   "POST",
			Path:     "/api/v1/internal/compliance/communication-check",
			Handler:  h.CommunicationCheck,
			AuthMode: "internal",
		},
	}, router.RouteDeps{
		JWTSecret:     jwtSecret,
		InternalToken: strings.TrimSpace(internalToken),
	})
}

func (h *Handler) ListCheckItems(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead)
	if !ok {
		return
	}
	items, err := h.service.ListCheckItems(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}
func (h *Handler) GetCheckItem(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead)
	if !ok {
		return
	}
	item, err := h.service.GetCheckItem(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		if err == pgx.ErrNoRows {
			httputil.WriteNotFound(w, "check item not found")
		} else {
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	httputil.WriteSuccess(w, item)
}
func (h *Handler) StartCheckItem(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingReview)
	if !ok {
		return
	}
	item, err := h.service.StartCheckItem(r.Context(), tenantID, r.PathValue("id"), claims.UserID)
	if err != nil {
		if err == pgx.ErrNoRows {
			httputil.WriteNotFound(w, "check item not found")
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}
func (h *Handler) ProcessCheckItem(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingReview)
	if !ok {
		return
	}
	var req CheckItemProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	item, err := h.service.ProcessCheckItem(r.Context(), tenantID, r.PathValue("id"), claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListRules 返回当前合规卫士使用的规则定义。
//
// 规则是全局平台规则，当前阶段不复制租户规则表；医院本地补充规则
// 将沿用同一张表和版本机制，后续再增加权限边界。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAbility(w, r, compliancepermission.AbilityRuleRead); !ok {
		return
	}
	filter := ComplianceRuleListFilter{
		Scope:    r.URL.Query().Get("scope"),
		RoleCode: r.URL.Query().Get("role_code"),
	}
	if value := strings.TrimSpace(r.URL.Query().Get("enabled")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			httputil.WriteBadRequest(w, "invalid enabled")
			return
		}
		filter.Enabled = &enabled
	}
	rules, err := h.service.ListRules(r.Context(), filter)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"rules": rules})
}

// GetRule 返回单条规则及其是否已经被分析记录使用。
//
// in_use 为 true 时，修改接口会创建新版本而不是覆盖原版本。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) GetRule(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAbility(w, r, compliancepermission.AbilityRuleRead); !ok {
		return
	}
	rule, err := h.service.GetRule(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		if err == errComplianceRuleNotFound {
			httputil.WriteNotFound(w, "compliance rule not found")
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rule)
}

// CreateRule 新增一条规则，默认启用并发布。
//
// 规则版本由请求提供时保留请求值，否则使用当前时间生成；规则编码和版本
// 共同构成唯一身份。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	claims, _, ok := h.requireAbility(w, r, compliancepermission.AbilityRuleManage)
	if !ok {
		return
	}
	var req ComplianceRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if err := validateComplianceRuleRequest(req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	rule, err := h.service.CreateRule(r.Context(), claims.UserID, req)
	if err != nil {
		if isComplianceRuleConflict(err) {
			httputil.WriteError(w, http.StatusConflict, "RULE_CONFLICT", err.Error(), nil)
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rule)
}

// UpdateRule 修改未使用规则，或为已使用规则创建新版本。
//
// 旧版本永远保留，确保历史分析结果仍能按原版本追溯。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	claims, _, ok := h.requireAbility(w, r, compliancepermission.AbilityRuleManage)
	if !ok {
		return
	}
	var req ComplianceRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if err := validateComplianceRuleRequest(req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	rule, err := h.service.UpdateRule(r.Context(), claims.UserID, strings.TrimSpace(r.PathValue("id")), req)
	if err != nil {
		switch {
		case err == errComplianceRuleNotFound:
			httputil.WriteNotFound(w, "compliance rule not found")
		case isComplianceRuleConflict(err):
			httputil.WriteError(w, http.StatusConflict, "RULE_CONFLICT", err.Error(), nil)
		default:
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	httputil.WriteSuccess(w, rule)
}

func (h *Handler) EnableRule(w http.ResponseWriter, r *http.Request) {
	h.setRuleEnabled(w, r, true)
}

func (h *Handler) DisableRule(w http.ResponseWriter, r *http.Request) {
	h.setRuleEnabled(w, r, false)
}

func (h *Handler) setRuleEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	claims, _, ok := h.requireAbility(w, r, compliancepermission.AbilityRuleManage)
	if !ok {
		return
	}
	rule, err := h.service.SetRuleEnabled(r.Context(), claims.UserID, strings.TrimSpace(r.PathValue("id")), enabled)
	if err != nil {
		if err == errComplianceRuleNotFound {
			httputil.WriteNotFound(w, "compliance rule not found")
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rule)
}

// DeleteRule 只允许删除从未被分析运行或发现使用的规则。
//
// 已使用规则不能删除，只能停用或新建版本，避免破坏历史结果的可追溯性。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	claims, _, ok := h.requireAbility(w, r, compliancepermission.AbilityRuleManage)
	if !ok {
		return
	}
	err := h.service.DeleteRule(r.Context(), claims.UserID, strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		switch {
		case err == errComplianceRuleNotFound:
			httputil.WriteNotFound(w, "compliance rule not found")
		case isComplianceRuleConflict(err):
			httputil.WriteError(w, http.StatusConflict, "RULE_IN_USE", err.Error(), nil)
		default:
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	httputil.WriteSuccess(w, map[string]string{"status": "deleted"})
}

func validateComplianceRuleRequest(req ComplianceRuleRequest) error {
	if strings.TrimSpace(req.Code) == "" {
		return fmt.Errorf("code is required")
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if scope := strings.TrimSpace(req.Scope); scope != "communication" && scope != "content" {
		return fmt.Errorf("scope must be communication or content")
	}
	if value := strings.TrimSpace(req.TriggerType); value != "" &&
		value != "keywords" && value != "semantic" && value != "pattern" {
		return fmt.Errorf("invalid trigger_type")
	}
	return nil
}

// Overview 返回当前租户的全院发现概览。
//
// 这是发现数据的聚合接口，不是分析执行日志接口；因此只读取
// compliance_findings，并沿用当前登录用户的租户校验。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead)
	if !ok {
		return
	}
	var err error
	overview, err := h.service.GetOverview(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, overview)
}

// ListFindings 为待核查和已核查页面提供发现数据。
//
// 租户由 JWT 和 tenant_id 查询参数共同确定：普通员工不能跨租户，
// 管理员只能查询自己可见的租户。筛选值只作为查询条件，不改变租户边界。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) ListFindings(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead)
	if !ok {
		return
	}

	var err error
	filter, err := parseFindingListFilter(r, tenantID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	result, err := h.service.ListFindings(r.Context(), filter)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, result.Items, result.Total, result.Page, result.PageSize)
}

// GetFinding 返回发现详情，并严格按当前登录用户的租户过滤。
//
// 记录不存在和记录属于其他租户都统一返回 404，避免泄露跨租户对象是否存在。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) GetFinding(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingRead)
	if !ok {
		return
	}
	var err error
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "finding id is required")
		return
	}
	finding, err := h.service.GetFinding(r.Context(), tenantID, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			httputil.WriteNotFound(w, "compliance finding not found")
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, finding)
}

// ReviewFinding 保存医务科对发现的人工核查结论。
//
// 只有待核查发现可以提交一次；已核查记录不能被覆盖，避免核查历史
// 在没有独立版本表的情况下被无痕修改。具体权限由当前路由的登录用户类型
// 和租户校验共同保证。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
func (h *Handler) ReviewFinding(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireAbility(w, r, compliancepermission.AbilityFindingReview)
	if !ok {
		return
	}
	var err error
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "finding id is required")
		return
	}

	var req ReviewFindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if err := validateReviewFindingRequest(req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	result, err := h.service.ReviewFinding(r.Context(), tenantID, id, claims.UserID, string(claims.UserType), req, r)
	if err != nil {
		switch {
		case err == errFindingNotFound:
			httputil.WriteNotFound(w, "compliance finding not found")
		case err == errFindingAlreadyReviewed:
			httputil.WriteError(w, http.StatusConflict, "ALREADY_REVIEWED", err.Error(), nil)
		default:
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	httputil.WriteSuccess(w, result)
}

func validateReviewFindingRequest(req ReviewFindingRequest) error {
	switch strings.TrimSpace(req.ReviewConclusion) {
	case "confirmed", "not_confirmed", "unable_to_determine":
		return nil
	default:
		return fmt.Errorf("review_conclusion must be confirmed, not_confirmed, or unable_to_determine")
	}
}

func parseFindingListFilter(r *http.Request, tenantID int64) (FindingListFilter, error) {
	query := r.URL.Query()
	filter := FindingListFilter{
		TenantID:   tenantID,
		Status:     splitQueryValues(query.Get("status")),
		Keyword:    query.Get("keyword"),
		SceneCode:  query.Get("scene_code"),
		RoleCode:   query.Get("role_code"),
		Department: query.Get("department"),
		RuleCode:   query.Get("rule_code"),
		Page:       1,
		PageSize:   20,
	}
	if value := strings.TrimSpace(query.Get("employee_id")); value != "" {
		employeeID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || employeeID <= 0 {
			return filter, fmt.Errorf("invalid employee_id")
		}
		filter.EmployeeID = &employeeID
	}
	if value := strings.TrimSpace(query.Get("page")); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page <= 0 {
			return filter, fmt.Errorf("invalid page")
		}
		filter.Page = page
	}
	if value := strings.TrimSpace(query.Get("page_size")); value != "" {
		pageSize, err := strconv.Atoi(value)
		if err != nil || pageSize <= 0 {
			return filter, fmt.Errorf("invalid page_size")
		}
		filter.PageSize = pageSize
	}
	var err error
	if value := strings.TrimSpace(query.Get("created_from")); value != "" {
		filter.CreatedFrom, err = parseFindingTime(value)
		if err != nil {
			return filter, fmt.Errorf("invalid created_from")
		}
	}
	if value := strings.TrimSpace(query.Get("created_to")); value != "" {
		filter.CreatedTo, err = parseFindingTime(value)
		if err != nil {
			return filter, fmt.Errorf("invalid created_to")
		}
	}
	return filter, nil
}

func splitQueryValues(value string) []string {
	values := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func parseFindingTime(value string) (*time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return &parsed, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// CommunicationCheck 接收 Worker 提交的沟通录音素材。
//
// 这里仅做 JSON 和字段的同步校验，并调用 Service 返回接收确认。
// 正式持久化、LLM 调用和结果入库将在后续 API 阶段继续接入。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func (h *Handler) CommunicationCheck(w http.ResponseWriter, r *http.Request) {
	// 先创建过程文件，再读取请求体。这样即使 body 读取失败，也能证明请求
	// 已经到达合规接口，并保留失败阶段的过程记录。
	processLogPath, processLogErr := h.service.startCommunicationProcessLog(nil)
	var requestBody []byte
	var readErr error
	processStatus := http.StatusInternalServerError
	processResponseBody := ""
	processRuntime := (*communicationRuntime)(nil)
	processError := ""
	defer func() {
		if processLogErr != nil {
			return
		}
		if err := h.service.finishCommunicationProcessLog(
			processLogPath,
			requestBody,
			processRuntime,
			processStatus,
			processResponseBody,
			processError,
		); err != nil {
			// 该失败不能改变已经返回的 HTTP 响应，但接口日志会保留失败原因。
			h.logCommunicationCheck(r, nil, processStatus, fmt.Sprintf(`{"process_log_error":%q}`, err.Error()))
		}
	}()

	requestBody, readErr = io.ReadAll(r.Body)
	if readErr != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		processStatus = http.StatusBadRequest
		processResponseBody = `{"code":"BAD_REQUEST","message":"invalid request body"}`
		processError = readErr.Error()
		h.logCommunicationCheck(r, requestBody, http.StatusBadRequest, `{"code":"BAD_REQUEST","message":"invalid request body"}`)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(requestBody))

	loggedWriter := &communicationCheckResponseWriter{
		header: make(http.Header),
	}
	for key, values := range w.Header() {
		loggedWriter.header[key] = append([]string(nil), values...)
	}

	var req CommunicationCheckRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		httputil.WriteBadRequest(loggedWriter, "invalid request body")
		h.writeCommunicationCheckResponse(w, loggedWriter)
		processStatus = loggedWriter.statusCode()
		processResponseBody = loggedWriter.body.String()
		processError = err.Error()
		h.logCommunicationCheck(r, requestBody, loggedWriter.statusCode(), loggedWriter.body.String())
		return
	}
	if err := validateCommunicationCheckRequest(req); err != nil {
		httputil.WriteBadRequest(loggedWriter, err.Error())
		h.writeCommunicationCheckResponse(w, loggedWriter)
		processStatus = loggedWriter.statusCode()
		processResponseBody = loggedWriter.body.String()
		processError = err.Error()
		h.logCommunicationCheck(r, requestBody, loggedWriter.statusCode(), loggedWriter.body.String())
		return
	}

	resp, runtime, err := h.service.AcceptCommunicationCheck(r.Context(), req)
	processRuntime = runtime
	if err != nil {
		if unsupported, ok := err.(*UnsupportedRoleError); ok {
			httputil.WriteError(loggedWriter, http.StatusUnprocessableEntity, "UNSUPPORTED_ROLE", unsupported.Error(), nil)
		} else if invalid, ok := err.(*InvalidReferenceError); ok {
			httputil.WriteError(loggedWriter, http.StatusUnprocessableEntity, "INVALID_REFERENCE", invalid.Error(), nil)
		} else {
			httputil.WriteInternalError(loggedWriter, err.Error())
		}
		h.writeCommunicationCheckResponse(w, loggedWriter)
		processStatus = loggedWriter.statusCode()
		processResponseBody = loggedWriter.body.String()
		processError = err.Error()
		h.logCommunicationCheck(r, requestBody, loggedWriter.statusCode(), loggedWriter.body.String())
		return
	}
	httputil.WriteSuccess(loggedWriter, resp)
	h.writeCommunicationCheckResponse(w, loggedWriter)
	processStatus = loggedWriter.statusCode()
	processResponseBody = loggedWriter.body.String()
	h.logCommunicationCheck(r, requestBody, loggedWriter.statusCode(), loggedWriter.body.String())
}

type communicationCheckResponseWriter struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func (w *communicationCheckResponseWriter) Header() http.Header {
	return w.header
}

func (w *communicationCheckResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
}

func (w *communicationCheckResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}

func (w *communicationCheckResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (h *Handler) writeCommunicationCheckResponse(w http.ResponseWriter, captured *communicationCheckResponseWriter) {
	for key, values := range captured.header {
		w.Header()[key] = append([]string(nil), values...)
	}
	w.WriteHeader(captured.statusCode())
	_, _ = w.Write(captured.body.Bytes())
}

// logCommunicationCheck 追加记录沟通合规接口的请求和响应。
//
// 只记录请求体、响应状态和响应体，不记录 X-Internal-Token，避免把服务凭证
// 写入业务调试日志。日志写入失败不改变 API 已经返回给调用方的结果。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func (h *Handler) logCommunicationCheck(r *http.Request, requestBody []byte, status int, responseBody string) {
	entry := map[string]any{
		"time":            time.Now().UTC().Format(time.RFC3339Nano),
		"method":          r.Method,
		"path":            r.URL.Path,
		"remote_addr":     r.RemoteAddr,
		"request_body":    string(requestBody),
		"response_status": status,
		"response_body":   responseBody,
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return
	}

	h.logMu.Lock()
	defer h.logMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.logPath), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(h.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(payload, '\n'))
}

func validateCommunicationCheckRequest(req CommunicationCheckRequest) error {
	trigger := strings.TrimSpace(req.TriggerReason)
	if trigger != "" && trigger != "delivery" && trigger != "manual_rerun" {
		return fmt.Errorf("trigger_reason must be delivery or manual_rerun")
	}
	if (trigger == "manual_rerun") != (strings.TrimSpace(req.RerunKey) != "") {
		return fmt.Errorf("manual_rerun requires rerun_key; delivery must not include rerun_key")
	}
	switch {
	case req.TenantID <= 0:
		return fmt.Errorf("tenant_id is required")
	case req.RecordingID <= 0:
		return fmt.Errorf("recording_id is required")
	case req.EncounterID <= 0:
		return fmt.Errorf("encounter_id is required")
	case req.EmployeeID <= 0:
		return fmt.Errorf("employee_id is required")
	// Patient identity is optional for realtime communication checks. The
	// recording, encounter, employee, role and final transcript remain required;
	// when a patient is known, EMR sends both customer_id and customer_name.
	case strings.TrimSpace(req.EmployeeName) == "" && strings.TrimSpace(req.DoctorName) == "":
		return fmt.Errorf("employee_name is required")
	case strings.TrimSpace(req.BusinessScope) == "":
		return fmt.Errorf("business_scope is required")
	case strings.TrimSpace(req.RoleCode) == "":
		return fmt.Errorf("role_code is required")
	case strings.TrimSpace(req.RecordedAt) == "":
		return fmt.Errorf("recorded_at is required")
	case strings.TrimSpace(req.CleanedTranscript) == "":
		return fmt.Errorf("cleaned_transcript is required")
	default:
		return nil
	}
}

func (h *Handler) AnalyzeText(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req AnalyzeTextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	resp, err := h.service.AnalyzeText(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

package emrcheck

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service          *Service
	permissions      *emrpermission.Service
	logMu            sync.Mutex
	executionLogPath string
	resultLogPath    string
	promptLogPath    string
}

func NewHandler(service *Service, permissions *emrpermission.Service) *Handler {
	return &Handler{
		service:          service,
		permissions:      permissions,
		executionLogPath: "/Users/yiliiang/Documents/lingce-web/apps/emr/data/log/emr_program_quality_execution.log",
		resultLogPath:    "/Users/yiliiang/Documents/lingce-web/apps/emr/data/log/emr_program_quality_result.log",
		promptLogPath:    "/Users/yiliiang/Documents/lingce-web/apps/emr/data/log/emr_llm_quality_prompt.log",
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "POST", Path: "/api/v1/emr/records/{id}/checks", Handler: h.RunManual, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/checks", Handler: h.ListRuns, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/checks/{check_id}", Handler: h.GetRun, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/quality-check/program-preview", Handler: h.ProgramPreview, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/quality-check/preview", Handler: h.CompletePreview, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

// CompletePreview 是第二阶段完整质检的调试入口：程序规则与大模型规则
// 均执行，结果返回页面，并将提示词、模型请求和结构化结果写入 EMR
// data/log，便于核对模板、绑定规则和模型输出是否一致。
//
// 署名：Codex
// 时间：2026-09-14
func (h *Handler) CompletePreview(w http.ResponseWriter, r *http.Request) {
	tenantID, _, err := h.authorizeQuality(r)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req CheckPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.TenantID = tenantID
	if req.Content == nil {
		httputil.WriteBadRequest(w, "content is required")
		return
	}
	startedAt := time.Now().UTC()
	data, err := h.service.CompletePreview(r.Context(), req)
	completedAt := time.Now().UTC()
	response := map[string]any{
		"code": "EMR_COMPLETE_QUALITY_PREVIEW", "tenant_id": tenantID,
		"template_version_id": req.TemplateVersionID, "execution_started_at": startedAt.Format(time.RFC3339Nano),
		"execution_completed_at": completedAt.Format(time.RFC3339Nano),
		"execution_duration_ms":  completedAt.Sub(startedAt).Milliseconds(),
		"execution_log_file":     h.executionLogPath, "result_log_file": h.resultLogPath, "prompt_log_file": h.promptLogPath,
	}
	if err != nil {
		response["error"] = err.Error()
		_ = h.writeCompleteQualityLogs(req, response, nil, err)
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	response["template_code"] = data.Template.TemplateCode
	response["template_version"] = data.Template.TemplateVersion
	response["prompt_code"] = data.Prompt.Code
	response["prompt_version"] = data.Prompt.Version
	response["model_provider"] = data.Config.Provider
	response["model_code"] = data.Config.ModelCode
	response["overall_result"] = data.Overall
	response["model_check_summary"] = data.Summary
	response["program_result_count"] = data.ProgramCount
	response["model_result_count"] = data.ModelCount
	response["result_count"] = len(data.Results)
	response["results"] = data.Results
	if err := h.writeCompleteQualityLogs(req, response, data, nil); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": response})
}

func (h *Handler) writeCompleteQualityLogs(req CheckPreviewRequest, response map[string]any, data *completePreviewData, callErr error) error {
	h.logMu.Lock()
	defer h.logMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.promptLogPath), 0o755); err != nil {
		return err
	}
	entry := map[string]any{"logged_at": time.Now().UTC().Format(time.RFC3339Nano), "request": req, "response": response}
	if callErr != nil {
		entry["error"] = callErr.Error()
	}
	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	if err := appendLog(h.executionLogPath, raw); err != nil {
		return err
	}
	resultEntry := map[string]any{"logged_at": time.Now().UTC().Format(time.RFC3339Nano), "response": response}
	resultRaw, err := json.MarshalIndent(resultEntry, "", "  ")
	if err != nil {
		return err
	}
	if err := appendLog(h.resultLogPath, resultRaw); err != nil {
		return err
	}
	if data == nil {
		return nil
	}
	promptEntry := map[string]any{
		"logged_at": time.Now().UTC().Format(time.RFC3339Nano), "request": req,
		"system_prompt": data.Prompt.SystemPrompt, "user_prompt": data.UserPrompt,
		"gateway_request": string(data.RequestBody), "gateway_response": string(data.ResponseBody),
	}
	promptRaw, err := json.MarshalIndent(promptEntry, "", "  ")
	if err != nil {
		return err
	}
	return appendLog(h.promptLogPath, promptRaw)
}

// ProgramPreview 是第一阶段程序质检的正式独立入口。
//
// 请求直接携带结构化病历和模板版本 ID，接口读取该模板绑定的规则，
// 只执行 execution_mode=程序判断 的规则。结果不写正式质检表，不调用
// 大模型；请求和返回会追加保存到 API 仓库 data/log，便于重复触发和核对。
//
// 署名：Codex
// 时间：2026-09-14
func (h *Handler) ProgramPreview(w http.ResponseWriter, r *http.Request) {
	tenantID, _, err := h.authorizeQuality(r)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	var req CheckPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.TenantID = tenantID
	if req.Content == nil {
		httputil.WriteBadRequest(w, "content is required")
		return
	}

	startedAt := time.Now().UTC()
	results, overall, err := h.service.PreviewProgram(r.Context(), req)
	completedAt := time.Now().UTC()
	response := map[string]any{
		"code":                   "EMR_PROGRAM_QUALITY_PREVIEW",
		"tenant_id":              tenantID,
		"template_version_id":    req.TemplateVersionID,
		"overall_result":         overall,
		"results":                results,
		"result_count":           len(results),
		"execution_started_at":   startedAt.Format(time.RFC3339Nano),
		"execution_completed_at": completedAt.Format(time.RFC3339Nano),
		"execution_duration_ms":  completedAt.Sub(startedAt).Milliseconds(),
		"execution_log_file":     h.executionLogPath,
		"result_log_file":        h.resultLogPath,
	}
	if err != nil {
		response["error"] = err.Error()
		if logErr := h.writeProgramQualityLogs(req, response, err); logErr != nil {
			httputil.WriteInternalError(w, logErr.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if logErr := h.writeProgramQualityLogs(req, response, nil); logErr != nil {
		httputil.WriteInternalError(w, logErr.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": response})
}

func (h *Handler) authorizeQuality(r *http.Request) (int64, int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		return 0, 0, err
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, "quality.manage")
	if err != nil {
		return 0, 0, err
	}
	return tenantID, access.UserID, nil
}

// writeProgramQualityLogs 将一次执行拆成两个独立文件：
// execution 文件记录输入、执行时间、规则数量和异常；
// result 文件只记录程序质检的结构化结果。
//
// 两个文件均采用追加写入，保证重复点击时不会覆盖历史记录。当前文件
// 包含完整病历内容，仅用于开发环境调试，生产环境不得继续记录敏感信息。
//
// 署名：Codex
// 时间：2026-09-14
func (h *Handler) writeProgramQualityLogs(req CheckPreviewRequest, response map[string]any, callErr error) error {
	h.logMu.Lock()
	defer h.logMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(h.executionLogPath), 0o755); err != nil {
		return err
	}

	executionEntry := map[string]any{
		"logged_at": time.Now().UTC().Format(time.RFC3339Nano),
		"request":   req,
		"execution": map[string]any{
			"started_at":   response["execution_started_at"],
			"completed_at": response["execution_completed_at"],
			"duration_ms":  response["execution_duration_ms"],
			"result_count": response["result_count"],
		},
	}
	if callErr != nil {
		executionEntry["error"] = callErr.Error()
	}
	executionRaw, err := json.MarshalIndent(executionEntry, "", "  ")
	if err != nil {
		return err
	}
	if err := appendLog(h.executionLogPath, executionRaw); err != nil {
		return err
	}

	resultEntry := map[string]any{
		"logged_at":           time.Now().UTC().Format(time.RFC3339Nano),
		"tenant_id":           req.TenantID,
		"template_version_id": req.TemplateVersionID,
		"overall_result":      response["overall_result"],
		"result_count":        response["result_count"],
		"results":             response["results"],
	}
	if callErr != nil {
		resultEntry["error"] = callErr.Error()
	}
	resultRaw, err := json.MarshalIndent(resultEntry, "", "  ")
	if err != nil {
		return err
	}
	return appendLog(h.resultLogPath, resultRaw)
}

func appendLog(path string, raw []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(raw, []byte("\n\n")...))
	return err
}

func (h *Handler) RunManual(w http.ResponseWriter, r *http.Request) {
	tenantID, actorID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	if recordID == "" {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	item, err := h.service.RunManual(r.Context(), tenantID, actorID, recordID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func (h *Handler) tenant(r *http.Request) (int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	return tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
}

func (h *Handler) authorizeRecord(r *http.Request, ability string) (int64, int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := h.tenant(r)
	if err != nil {
		return 0, 0, err
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, ability)
	if err != nil {
		return 0, 0, err
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	allowed, err := h.permissions.CanAccessRecord(r.Context(), access, recordID)
	if err != nil {
		return 0, 0, err
	}
	if !allowed {
		return 0, 0, fmt.Errorf("emr record access denied")
	}
	return tenantID, access.UserID, nil
}

func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	tenantID, _, err := h.authorizeRecord(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	if recordID == "" {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	items, err := h.service.ListRuns(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	tenantID, _, err := h.authorizeRecord(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	checkID := strings.TrimSpace(r.PathValue("check_id"))
	if recordID == "" || checkID == "" {
		httputil.WriteBadRequest(w, "invalid check run id")
		return
	}
	item, err := h.service.GetRun(r.Context(), tenantID, recordID, checkID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "check run not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

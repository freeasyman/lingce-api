package followup

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Handler 是随访中心 Worker API 的 HTTP 入口。
// 署名：Codex
// 时间：2026-09-09
//
// 说明：
// 1. 只保留最小实现。
// 2. 接到请求后立即确认，不返回任务生成结果。
type Handler struct {
	service *Service
}

// NewHandler 创建 Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes 注册 Worker 调用接口。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, internalToken string) {
	router.Register(mux, []router.Route{
		{
			Method:   "POST",
			Path:     "/api/v1/followup/tasks/generate",
			Handler:  h.Generate,
			AuthMode: "internal",
		},
	}, router.RouteDeps{InternalToken: strings.TrimSpace(internalToken)})
}

// Generate 接收 Worker 传入的清洗后转写资料，并写入调试日志。
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if req.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}
	if req.RecordingID <= 0 {
		httputil.WriteBadRequest(w, "recording_id is required")
		return
	}
	if req.EncounterID <= 0 {
		httputil.WriteBadRequest(w, "encounter_id is required")
		return
	}
	if req.EmployeeID <= 0 {
		httputil.WriteBadRequest(w, "employee_id is required")
		return
	}
	if strings.TrimSpace(req.DoctorName) == "" {
		httputil.WriteBadRequest(w, "doctor_name is required")
		return
	}
	if req.CustomerID <= 0 {
		httputil.WriteBadRequest(w, "customer_id is required")
		return
	}
	if strings.TrimSpace(req.CustomerName) == "" {
		httputil.WriteBadRequest(w, "customer_name is required")
		return
	}
	if strings.TrimSpace(req.RecordedAt) == "" {
		httputil.WriteBadRequest(w, "recorded_at is required")
		return
	}
	if strings.TrimSpace(req.CleanedTranscript) == "" {
		httputil.WriteBadRequest(w, "cleaned_transcript is required")
		return
	}

	resp, err := h.service.AcceptRequest(r.Context(), req)
	if err != nil {
		var invalidRefErr *InvalidReferenceError
		if errors.As(err, &invalidRefErr) {
			httputil.WriteError(w, http.StatusUnprocessableEntity, "INVALID_REFERENCE", invalidRefErr.Error(), nil)
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

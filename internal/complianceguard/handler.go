package complianceguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
	logMu   sync.Mutex
	logPath string
}

const communicationCheckLogPath = "/Users/yiliiang/Documents/lingce-web/apps/compliance/data/log/communication-check.log"

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
		logPath: communicationCheckLogPath,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "POST", Path: "/api/v1/compliance/debug/analyze-text", Handler: h.AnalyzeText, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
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
		if invalid, ok := err.(*InvalidReferenceError); ok {
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
	switch {
	case req.TenantID <= 0:
		return fmt.Errorf("tenant_id is required")
	case req.RecordingID <= 0:
		return fmt.Errorf("recording_id is required")
	case req.EncounterID <= 0:
		return fmt.Errorf("encounter_id is required")
	case req.EmployeeID <= 0:
		return fmt.Errorf("employee_id is required")
	case req.CustomerID <= 0:
		return fmt.Errorf("customer_id is required")
	case strings.TrimSpace(req.CustomerName) == "":
		return fmt.Errorf("customer_name is required")
	case strings.TrimSpace(req.DoctorName) == "":
		return fmt.Errorf("doctor_name is required")
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

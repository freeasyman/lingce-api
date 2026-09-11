package emrrecord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// InternalHandler 是 Worker 调用 EMR 的临时内部接收入口。
//
// 当前阶段只验证 Worker 到 EMR 的请求链路是否连通，因此这里故意不进入
// 正式的病历 Service，也不读取数据库、不读取模板、不读取提示词、不调用
// LLM 网关、不创建病历。请求和返回统一追加到 EMR 前端项目的 data/log
// 目录，便于开发调试时直接查看完整报文。
//
// 署名：Codex
// 时间：2026-09-12
type InternalHandler struct {
	logPath string
	logMu   sync.Mutex
}

// NewInternalHandler 创建 Worker 接收 Handler。
//
// 日志路径使用绝对路径，避免 API 从不同工作目录启动时把调试文件写到
// 不同位置。该文件是临时联调日志，不是正式审计日志，也不替代 EMR
// 操作日志表。
//
// 署名：Codex
// 时间：2026-09-12
func NewInternalHandler() *InternalHandler {
	return &InternalHandler{
		logPath: filepath.Join(
			"/Users/yiliiang/Documents/lingce-web/apps/emr",
			"data",
			"log",
			"emr_worker_debug.log",
		),
	}
}

// RegisterRoutes 注册 Worker 调用 EMR 的内部接收接口。
//
// 路径必须与 lingce-worker/internal/gateway/lingce_api.go 中的客户端保持
// 完全一致。内部路由使用 X-Internal-Token，不使用用户 JWT。
//
// 署名：Codex
// 时间：2026-09-12
func (h *InternalHandler) RegisterRoutes(mux *http.ServeMux, internalToken string) {
	router.Register(mux, []router.Route{
		{
			Method:   http.MethodPost,
			Path:     "/api/v1/internal/emr/records/generate",
			Handler:  h.Generate,
			AuthMode: "internal",
		},
	}, router.RouteDeps{InternalToken: strings.TrimSpace(internalToken)})
}

// Generate 接收 Worker 的完整工牌录音分析输入，并返回固定的接收确认。
//
// 本函数当前只用于链路调试：
// 1. 解析 JSON；
// 2. 校验请求体不是空 JSON；
// 3. 记录原始请求；
// 4. 生成不触发任何业务处理的固定 ACCEPTED 返回；
// 5. 记录完整返回；
// 6. 将返回发送给 Worker。
//
// 不在这里执行任何其他动作。后续正式接入时，应另行把异步业务处理接到
// 独立 Service，不能把本调试入口悄悄扩展成病历生成入口。
//
// 署名：Codex
// 时间：2026-09-12
func (h *InternalHandler) Generate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if len(payload) == 0 {
		httputil.WriteBadRequest(w, "request body is required")
		return
	}

	requestID := fmt.Sprintf("emr-%d", time.Now().UnixNano())
	response := map[string]any{
		"code":         "ACCEPTED",
		"status":       "accepted",
		"message":      "received",
		"request_id":   requestID,
		"tenant_id":    payload["tenant_id"],
		"recording_id": payload["recording_id"],
		"encounter_id": payload["encounter_id"],
		"employee_id":  payload["employee_id"],
		"customer_id":  payload["customer_id"],
	}
	if err := h.appendDebugLog(payload, response); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, response)
}

// appendDebugLog 将一次请求和对应返回追加到同一个文本文件。
//
// 使用互斥锁避免多个 Worker 请求同时到达时交错写入，确保每个调试块
// 仍然可以按时间完整阅读。日志目录不存在时自动创建；除此之外不执行
// 数据库写入或任何业务动作。
//
// 署名：Codex
// 时间：2026-09-12
func (h *InternalHandler) appendDebugLog(request, response map[string]any) error {
	if h == nil || strings.TrimSpace(h.logPath) == "" {
		return fmt.Errorf("emr debug log path is empty")
	}
	requestJSON, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal emr debug request: %w", err)
	}
	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal emr debug response: %w", err)
	}
	entry := fmt.Sprintf(
		"[%s]\nREQUEST:\n%s\nRESPONSE:\n%s\n\n",
		time.Now().Format(time.RFC3339),
		requestJSON,
		responseJSON,
	)

	h.logMu.Lock()
	defer h.logMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.logPath), 0o755); err != nil {
		return fmt.Errorf("create emr debug log directory: %w", err)
	}
	file, err := os.OpenFile(h.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open emr debug log: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(entry); err != nil {
		return fmt.Errorf("write emr debug log: %w", err)
	}
	return nil
}

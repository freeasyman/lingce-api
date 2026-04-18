package badge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Smart Badge Advanced Handlers

// GetDeviceHistory handles getting device history
func (h *Handler) GetDeviceHistory(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	deviceNo := r.PathValue("device_no")
	if deviceNo == "" {
		httputil.WriteBadRequest(w, "Device number is required")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	logs, total, err := h.service.ListRecordingControlLogs(r.Context(), RecordingControlLogListRequest{
		DeviceNo: &deviceNo,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"device_no": deviceNo,
		"history":   logs,
		"total":     total,
	})
}

// DeveloperCallback handles developer callback
func (h *Handler) DeveloperCallback(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	h.handleCallback(w, r, "developer_event", "developer", false)
}

// AudioCallback handles audio callback
func (h *Handler) AudioCallback(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	h.handleCallback(w, r, "audio_event", "audio", false)
}

// InternalDeveloperCallback handles callback from badge-middleware dispatch worker.
func (h *Handler) InternalDeveloperCallback(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeInternalCallback(w, r) {
		return
	}
	h.handleCallback(w, r, "developer_event", "developer", true)
}

// InternalAudioCallback handles callback from badge-middleware dispatch worker.
func (h *Handler) InternalAudioCallback(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeInternalCallback(w, r) {
		return
	}
	h.handleCallback(w, r, "audio_event", "audio", true)
}

// ProcessPendingEvents handles processing pending events
func (h *Handler) ProcessPendingEvents(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	status := "failed"
	page, _ := strconv.Atoi(toString(req["page"]))
	pageSize, _ := strconv.Atoi(toString(req["page_size"]))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	logs, total, err := h.service.ListRecordingControlLogs(r.Context(), RecordingControlLogListRequest{
		Status:   &status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	processed := 0
	for _, log := range logs {
		_ = h.service.CreateCallbackLog(r.Context(), CallbackPayload{
			DeviceNo:  log.DeviceNo,
			EventType: "retry_failed_event",
			Data: JSONObject{
				"log_id":      log.ID,
				"original_at": log.CreatedAt,
			},
		}, "processor")
		processed++
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"processed": processed,
		"total":     total,
		"message":   "Pending events processed successfully",
		"at":        time.Now().Format(time.RFC3339),
	})
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return ""
	}
}

func (h *Handler) authorizeInternalCallback(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimSpace(r.Header.Get("X-Gateway-Token"))
	if token == "" {
		httputil.WriteUnauthorized(w, "Missing X-Gateway-Token")
		return false
	}
	if strings.TrimSpace(h.callbackGatewayToken) == "" {
		httputil.WriteInternalError(w, "Callback gateway token is not configured")
		return false
	}
	if token != strings.TrimSpace(h.callbackGatewayToken) {
		httputil.WriteUnauthorized(w, "Invalid X-Gateway-Token")
		return false
	}
	return true
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request, defaultEventType, source string, internal bool) {
	payloads, err := parseCallbackPayloads(r, defaultEventType)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	processed := 0
	failed := 0
	lastErr := ""
	for _, payload := range payloads {
		if err := h.service.CreateCallbackLog(r.Context(), payload, source); err != nil {
			failed++
			lastErr = err.Error()
			continue
		}
		processed++
	}

	if processed == 0 {
		httputil.WriteInternalError(w, "failed to process callback: "+lastErr)
		return
	}

	resp := map[string]interface{}{
		"message":   "Callback processed successfully",
		"processed": processed,
		"failed":    failed,
		"internal":  internal,
	}
	httputil.WriteSuccess(w, resp)
}

func parseCallbackPayloads(r *http.Request, defaultEventType string) ([]CallbackPayload, error) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}

	// Standard single callback payload.
	if deviceNo := toString(req["device_no"]); deviceNo != "" {
		eventType := toString(req["event_type"])
		if eventType == "" {
			eventType = defaultEventType
		}
		payload := CallbackPayload{
			DeviceNo:  deviceNo,
			EventType: eventType,
		}
		if data, ok := req["data"].(map[string]interface{}); ok {
			payload.Data = JSONObject(data)
		}
		return []CallbackPayload{payload}, nil
	}

	// badge-middleware AUDIO_INFORM/DEVELOPER_INFORM batched callback payload.
	callbackType := toString(req["callback"])
	if callbackType == "" {
		callbackType = toString(req["event_type"])
	}
	eventType := defaultEventType
	if callbackType != "" {
		eventType = callbackType
	}

	rows, ok := req["data"].([]interface{})
	if !ok || len(rows) == 0 {
		return nil, fmt.Errorf("data is required")
	}
	payloads := make([]CallbackPayload, 0, len(rows))
	for _, row := range rows {
		item, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		deviceNo := toString(item["device_no"])
		if deviceNo == "" {
			deviceNo = toString(item["deviceNo"])
		}
		if deviceNo == "" {
			continue
		}
		payloads = append(payloads, CallbackPayload{
			DeviceNo:  deviceNo,
			EventType: eventType,
			Data:      JSONObject(item),
		})
	}

	if len(payloads) == 0 {
		return nil, fmt.Errorf("no valid device_no found in callback payload")
	}
	return payloads, nil
}

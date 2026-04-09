package badge

import (
	"encoding/json"
	"net/http"
	"strconv"
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

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	payload := CallbackPayload{
		DeviceNo:  toString(req["device_no"]),
		EventType: toString(req["event_type"]),
	}
	if data, ok := req["data"].(map[string]interface{}); ok {
		payload.Data = data
	}
	if payload.DeviceNo == "" {
		httputil.WriteBadRequest(w, "device_no is required")
		return
	}
	if payload.EventType == "" {
		payload.EventType = "developer_event"
	}

	if err := h.service.CreateCallbackLog(r.Context(), payload, "developer"); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"message":    "Callback processed successfully",
		"device_no":  payload.DeviceNo,
		"event_type": payload.EventType,
	})
}

// AudioCallback handles audio callback
func (h *Handler) AudioCallback(w http.ResponseWriter, r *http.Request) {
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

	payload := CallbackPayload{
		DeviceNo:  toString(req["device_no"]),
		EventType: toString(req["event_type"]),
	}
	if data, ok := req["data"].(map[string]interface{}); ok {
		payload.Data = data
	}
	if payload.DeviceNo == "" {
		httputil.WriteBadRequest(w, "device_no is required")
		return
	}
	if payload.EventType == "" {
		payload.EventType = "audio_event"
	}

	if err := h.service.CreateCallbackLog(r.Context(), payload, "audio"); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"message":    "Audio callback processed successfully",
		"device_no":  payload.DeviceNo,
		"event_type": payload.EventType,
	})
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

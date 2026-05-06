package badge

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Device Inspection Handlers

// ValidateAcceptance handles validating acceptance import
func (h *Handler) ValidateAcceptance(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can validate acceptance
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	devices, ok := req["devices"].([]interface{})
	if !ok || len(devices) == 0 {
		httputil.WriteBadRequest(w, "devices is required")
		return
	}

	var errs []string
	for i, d := range devices {
		item, ok := d.(map[string]interface{})
		if !ok {
			errs = append(errs, "invalid devices format at index "+strconv.Itoa(i))
			continue
		}
		if item["device_no"] == nil || item["manufacturer_code"] == nil {
			errs = append(errs, "device_no and manufacturer_code are required at index "+strconv.Itoa(i))
		}
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"valid":  len(errs) == 0,
		"errors": errs,
	})
}

// VendorCheck handles vendor check
func (h *Handler) VendorCheck(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can perform vendor check
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	manufacturerCode, _ := req["manufacturer_code"].(string)
	if manufacturerCode == "" {
		httputil.WriteBadRequest(w, "manufacturer_code is required")
		return
	}
	manufacturer, err := h.service.GetManufacturerByCode(r.Context(), manufacturerCode)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"verified": manufacturer.IsActive,
		"details": map[string]interface{}{
			"manufacturer_code": manufacturer.Code,
			"manufacturer_name": manufacturer.Name,
			"is_active":         manufacturer.IsActive,
		},
	})
}

// InspectDevice handles single device inspection
func (h *Handler) InspectDevice(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can inspect devices
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	deviceIDStr := r.PathValue("device_id")
	if deviceIDStr == "" {
		httputil.WriteBadRequest(w, "Device ID is required")
		return
	}
	deviceID, err := strconv.ParseInt(deviceIDStr, 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device ID")
		return
	}
	device, err := h.service.GetDeviceByID(r.Context(), deviceID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"device_id": deviceID,
		"status":    "ok",
		"details": map[string]interface{}{
			"device_no":     device.DeviceNo,
			"manufacturer":  device.ManufacturerCode,
			"battery_level": device.BatteryLevel,
			"firmware":      device.FirmwareVersion,
		},
	})
}

// BatchInspect handles batch device inspection
func (h *Handler) BatchInspect(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can batch inspect
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	rawIDs, ok := req["device_ids"].([]interface{})
	if !ok || len(rawIDs) == 0 {
		httputil.WriteBadRequest(w, "device_ids is required")
		return
	}
	results := make([]map[string]interface{}, 0, len(rawIDs))
	succeeded := 0
	for _, raw := range rawIDs {
		idFloat, ok := raw.(float64)
		if !ok {
			results = append(results, map[string]interface{}{"status": "failed", "reason": "invalid device id"})
			continue
		}
		id := int64(idFloat)
		_, err := h.service.GetDeviceByID(r.Context(), id)
		if err != nil {
			results = append(results, map[string]interface{}{"device_id": id, "status": "failed", "reason": err.Error()})
			continue
		}
		results = append(results, map[string]interface{}{"device_id": id, "status": "ok"})
		succeeded++
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"total":     len(rawIDs),
		"succeeded": succeeded,
		"failed":    len(rawIDs) - succeeded,
		"results":   results,
	})
}

// ListInspectionDevices handles listing inspection devices
func (h *Handler) ListInspectionDevices(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can list inspection devices
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
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

	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if strings.EqualFold(status, "all") {
		status = ""
	}
	var statusFilter *string
	if status != "" {
		statusFilter = &status
	}
	devices, total, err := h.service.ListDevices(r.Context(), DeviceListRequest{
		Status:   statusFilter,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, devices, int64(total), page, pageSize)
}

// GetDeviceLiveStatus handles getting device live status
func (h *Handler) GetDeviceLiveStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can get live status
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	deviceIDStr := r.PathValue("device_id")
	if deviceIDStr == "" {
		httputil.WriteBadRequest(w, "Device ID is required")
		return
	}
	deviceID, err := strconv.ParseInt(deviceIDStr, 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device ID")
		return
	}

	resp, err := h.service.V2GetLiveStatus(r.Context(), deviceID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// TestDeviceRecording handles testing device recording
func (h *Handler) TestDeviceRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can test recording
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	deviceIDStr := r.PathValue("device_id")
	if deviceIDStr == "" {
		httputil.WriteBadRequest(w, "Device ID is required")
		return
	}
	deviceID, err := strconv.ParseInt(deviceIDStr, 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device ID")
		return
	}
	device, err := h.service.GetDeviceByID(r.Context(), deviceID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	var req struct {
		Action string `json:"action"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "start"
	}
	if action != "start" && action != "stop" {
		httputil.WriteBadRequest(w, "action must be start or stop")
		return
	}

	extra := JSONObject{
		"source": "inspection_test",
	}

	if action == "start" {
		if err := h.service.StartRecording(r.Context(), device.DeviceNo, &claims.UserID, extra); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}

		httputil.WriteSuccess(w, map[string]interface{}{
			"device_id": deviceID,
			"test_id":   "rec_test_" + strconv.FormatInt(time.Now().UnixNano(), 10),
			"status":    "started",
			"started":   true,
			"ok":        true,
		})
		return
	}

	if err := h.service.StopRecording(r.Context(), device.DeviceNo, &claims.UserID, extra); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	// 停止录音后短暂轮询回调/分析链路状态，供验收流程显示逐步进展。
	callbackOK, analysisOK := h.waitRecordingChainStatus(r, device.DeviceNo, 75*time.Second)

	httputil.WriteSuccess(w, map[string]interface{}{
		"device_id":    deviceID,
		"test_id":      "rec_test_" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"status":       "stopped",
		"stopped":      true,
		"ok":           true,
		"callback_ok":  callbackOK,
		"analysis_ok":  analysisOK,
		"chain_status": map[string]bool{"stop": true, "callback": callbackOK, "analysis": analysisOK},
	})
}

func (h *Handler) waitRecordingChainStatus(r *http.Request, deviceNo string, timeout time.Duration) (bool, bool) {
	deadline := time.Now().Add(timeout)
	action := "callback"
	for time.Now().Before(deadline) {
		// First preference: callback control logs (Go callback path).
		logs, _, err := h.service.ListRecordingControlLogs(r.Context(), RecordingControlLogListRequest{
			DeviceNo: &deviceNo,
			Action:   &action,
			Page:     1,
			PageSize: 50,
		})
		if err == nil {
			callbackOK := false
			analysisOK := false
			for _, log := range logs {
				source := strings.ToLower(strings.TrimSpace(toString(log.ExtraData["source"])))
				eventType := strings.ToLower(strings.TrimSpace(toString(log.ExtraData["event_type"])))
				if source == "audio" || strings.Contains(eventType, "audio") {
					callbackOK = true
				}
				if source == "analysis" || strings.Contains(eventType, "analysis") {
					analysisOK = true
				}
			}
			if callbackOK || analysisOK {
				return callbackOK, analysisOK
			}
		}
		// Fallback: audio callback events table (legacy/other callback ingress path).
		audioCallbackOK, ingestOK, audioErr := h.service.GetLatestAudioEventStatus(r.Context(), deviceNo)
		if audioErr == nil && (audioCallbackOK || ingestOK) {
			return audioCallbackOK, ingestOK
		}
		time.Sleep(1 * time.Second)
	}
	return false, false
}

// SubmitTicketByDevice handles submitting ticket by device
func (h *Handler) SubmitTicketByDevice(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req TicketSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	ticket, err := h.service.CreateTicket(
		r.Context(),
		claims.UserID,
		claims.TenantID,
		claims.UserType != auth.UserTypeAdmin,
		req,
	)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, ticket)
}

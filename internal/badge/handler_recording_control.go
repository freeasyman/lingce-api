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

// TestDeviceRecording handles testing device recording.
func (h *Handler) TestDeviceRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
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
	if !canControlDeviceRecording(claims, device.TenantID) {
		httputil.WriteForbidden(w, "Device access denied")
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

	extra := JSONObject{"source": "inspection_test"}

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

func canControlDeviceRecording(claims *auth.Claims, deviceTenantID *int64) bool {
	if claims == nil {
		return false
	}
	if claims.UserType == auth.UserTypeAdmin {
		return true
	}
	if claims.UserType != auth.UserTypeEmployee && claims.UserType != auth.UserTypeMobile {
		return false
	}
	return claims.TenantID != nil && deviceTenantID != nil && *claims.TenantID == *deviceTenantID
}

func (h *Handler) waitRecordingChainStatus(r *http.Request, deviceNo string, timeout time.Duration) (bool, bool) {
	deadline := time.Now().Add(timeout)
	action := "callback"
	for time.Now().Before(deadline) {
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

		audioCallbackOK, ingestOK, audioErr := h.service.GetLatestAudioEventStatus(r.Context(), deviceNo)
		if audioErr == nil && (audioCallbackOK || ingestOK) {
			return audioCallbackOK, ingestOK
		}
		time.Sleep(1 * time.Second)
	}
	return false, false
}

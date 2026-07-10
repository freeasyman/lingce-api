package badge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service              *Service
	callbackGatewayToken string
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SetCallbackGatewayToken(token string) {
	h.callbackGatewayToken = token
}

// RegisterRoutes registers badge module routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)
	h.registerRebuildRoutes(mux, authMw)

	// V1 endpoints still kept only for institution-side badge usage.
	mux.Handle("GET /api/v1/badge-devices", authMw(http.HandlerFunc(h.V2ListDevices)))
	mux.Handle("GET /api/v1/badge-devices/{id}", authMw(http.HandlerFunc(h.V2GetDevice)))
	mux.Handle("POST /api/v1/badge-devices/{device_id}/actions/recording-test", authMw(http.HandlerFunc(h.TestDeviceRecording)))
	mux.Handle("GET /api/v1/badge-devices/recording-control", authMw(http.HandlerFunc(h.GetRecordingControlDevices)))
	mux.Handle("POST /api/v1/badge-devices/{device_no}/actions/start-recording", authMw(http.HandlerFunc(h.StartRecording)))
	mux.Handle("POST /api/v1/badge-devices/{device_no}/actions/stop-recording", authMw(http.HandlerFunc(h.StopRecording)))
	mux.Handle("GET /api/v1/badge-devices/recording-control/logs", authMw(http.HandlerFunc(h.GetRecordingControlLogs)))
	mux.Handle("GET /api/v1/badge-devices/{device_no}/history", authMw(http.HandlerFunc(h.GetDeviceHistory)))

	mux.Handle("GET /api/v1/badge-devices/me", authMw(http.HandlerFunc(h.GetMyBadgeStatus)))
	mux.Handle("POST /api/v1/badge-devices/me/actions/start-recording", authMw(http.HandlerFunc(h.StartMyRecording)))
	mux.Handle("POST /api/v1/badge-devices/me/actions/stop-recording", authMw(http.HandlerFunc(h.StopMyRecording)))

	mux.Handle("POST /api/v1/badge-devices/callbacks/developer", authMw(http.HandlerFunc(h.DeveloperCallback)))
	mux.Handle("POST /api/v1/badge-devices/callbacks/audio", authMw(http.HandlerFunc(h.AudioCallback)))
	mux.Handle("POST /api/v1/badge-devices/actions/process-pending", authMw(http.HandlerFunc(h.ProcessPendingEvents)))

	// Internal callback endpoints for badge-middleware dispatch worker.
	// These endpoints are token-protected (X-Gateway-Token) and do not require JWT.
	mux.Handle("POST /api/v1/smart-badge/callback/developer", http.HandlerFunc(h.InternalDeveloperCallback))
	mux.Handle("POST /api/v1/smart-badge/callback/audio", http.HandlerFunc(h.InternalAudioCallback))

	mux.Handle("GET /api/v1/badge-devices/recording-stats", authMw(http.HandlerFunc(h.GetRecordingStats)))

	h.registerTicketRoutes(mux, authMw)
}

// Ticket Handlers

// SubmitTicket handles submitting a ticket
func (h *Handler) SubmitTicket(w http.ResponseWriter, r *http.Request) {
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

// GetMyTickets handles getting my tickets
func (h *Handler) GetMyTickets(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req TicketListRequest
	req.SubmitterID = &claims.UserID

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	tickets, total, err := h.service.ListTickets(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, tickets, int64(total), req.Page, req.PageSize)
}

// ListTickets handles listing tickets
func (h *Handler) ListTickets(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req TicketListRequest

	if typeStr := r.URL.Query().Get("type"); typeStr != "" {
		req.Type = &typeStr
	} else if ticketType := r.URL.Query().Get("ticket_type"); ticketType != "" {
		req.Type = &ticketType
	}

	if status := r.URL.Query().Get("status"); status != "" {
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "submitted", "reviewing":
			status = "pending"
		case "processing":
			status = "approved"
		case "done":
			status = "completed"
		}
		req.Status = &status
	}

	if submitterIDStr := r.URL.Query().Get("submitter_id"); submitterIDStr != "" {
		submitterID, _ := strconv.ParseInt(submitterIDStr, 10, 64)
		req.SubmitterID = &submitterID
	}

	// Admin can view all tickets, employees can only view their tenant's tickets
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID != nil && *claims.TenantID > 0 {
			req.TenantID = claims.TenantID
		} else {
			// Fallback for tokens without effective tenant_id.
			req.SubmitterID = &claims.UserID
		}
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize <= 0 {
		if limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); limit > 0 {
			pageSize = limit
		}
	}
	req.Page = page
	req.PageSize = pageSize

	tickets, total, err := h.service.ListTickets(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, tickets, int64(total), req.Page, req.PageSize)
}

// GetTicket handles getting ticket detail by ID
func (h *Handler) GetTicket(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid ticket ID")
		return
	}

	ticket, err := h.service.GetTicketByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	// Admin can read all tickets; non-admin is limited to own submitted tickets in detail API.
	if claims.UserType != auth.UserTypeAdmin && ticket.SubmitterID != claims.UserID {
		httputil.WriteForbidden(w, "No permission to access this ticket")
		return
	}

	httputil.WriteSuccess(w, ticket)
}

// ReviewTicket handles reviewing a ticket
func (h *Handler) ReviewTicket(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can review tickets
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can review tickets")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("ticket_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid ticket ID")
		return
	}

	var req TicketReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	approved := req.Approved
	if req.Approve != nil {
		approved = *req.Approve
	}
	notes := req.Notes
	if notes == nil {
		notes = req.ReviewNote
	}

	if err := h.service.ReviewTicket(r.Context(), id, claims.UserID, approved, notes); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Ticket reviewed successfully"})
}

// ExecuteTicket handles executing a ticket
func (h *Handler) ExecuteTicket(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can execute tickets
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can execute tickets")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("ticket_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid ticket ID")
		return
	}

	var req TicketExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	success := true
	if req.Success != nil {
		success = *req.Success
	}
	notes := req.Notes
	if notes == nil {
		notes = req.ResultMessage
	}

	if err := h.service.ExecuteTicket(r.Context(), id, claims.UserID, success, notes); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Ticket executed successfully"})
}

// Device Management Handlers

// ListDevices handles listing devices
func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req DeviceListRequest

	// Admin can view all devices, employees can only view their tenant's devices
	if claims.UserType != auth.UserTypeAdmin {
		req.TenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

	if employeeIDStr := r.URL.Query().Get("employee_id"); employeeIDStr != "" {
		employeeID, _ := strconv.ParseInt(employeeIDStr, 10, 64)
		req.EmployeeID = &employeeID
	}

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if manufacturerCode := r.URL.Query().Get("manufacturer_code"); manufacturerCode != "" {
		req.ManufacturerCode = &manufacturerCode
	}

	if deviceNo := r.URL.Query().Get("device_no"); deviceNo != "" {
		req.DeviceNo = &deviceNo
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	devices, total, err := h.service.ListDevices(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, devices, int64(total), req.Page, req.PageSize)
}

// GetRecordingControlDevices handles getting recording control devices
func (h *Handler) GetRecordingControlDevices(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Get tenant ID
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	var req DeviceListRequest
	req.TenantID = &tenantID
	req.Status = stringPtr("in_use")

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	devices, total, err := h.service.ListDevices(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, devices, int64(total), req.Page, req.PageSize)
}

// StartRecording handles starting recording
func (h *Handler) StartRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	deviceNo := r.PathValue("device_no")

	var req RecordingControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.StartRecording(r.Context(), deviceNo, &claims.UserID, req.ExtraData); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Recording started successfully"})
}

// StopRecording handles stopping recording
func (h *Handler) StopRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	deviceNo := r.PathValue("device_no")

	var req RecordingControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.StopRecording(r.Context(), deviceNo, &claims.UserID, req.ExtraData); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Recording stopped successfully"})
}

// GetRecordingControlLogs handles getting recording control logs
func (h *Handler) GetRecordingControlLogs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req RecordingControlLogListRequest

	// Admin can view all logs, employees can only view their tenant's logs
	if claims.UserType != auth.UserTypeAdmin {
		req.TenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

	if employeeIDStr := r.URL.Query().Get("employee_id"); employeeIDStr != "" {
		employeeID, _ := strconv.ParseInt(employeeIDStr, 10, 64)
		req.EmployeeID = &employeeID
	}

	if deviceNo := r.URL.Query().Get("device_no"); deviceNo != "" {
		req.DeviceNo = &deviceNo
	}

	if action := r.URL.Query().Get("action"); action != "" {
		req.Action = &action
	}

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if startDate := r.URL.Query().Get("start_date"); startDate != "" {
		req.StartDate = &startDate
	}

	if endDate := r.URL.Query().Get("end_date"); endDate != "" {
		req.EndDate = &endDate
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	logs, total, err := h.service.ListRecordingControlLogs(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, logs, int64(total), req.Page, req.PageSize)
}

// GetMyBadgeStatus handles getting my badge status
func (h *Handler) GetMyBadgeStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	device, err := h.getMyAssignedDevice(r, claims.UserID)
	if err != nil {
		httputil.WriteSuccess(w, &MyBadgeStatusResponse{IsOnline: false})
		return
	}

	status := &MyBadgeStatusResponse{
		DeviceNo:                 &device.DeviceNo,
		DeviceID:                 &device.ID,
		Status:                   &device.Status,
		IsOnline:                 device.Status != "reclaimed" && device.Status != "retired",
		IsRecording:              false,
		RecordStatus:             intPtr(0),
		RecordingDurationSeconds: intPtr(0),
		BatteryLevel:             device.BatteryLevel,
		FirmwareVersion:          device.FirmwareVersion,
		LastOnlineAt:             device.LastOnlineAt,
	}

	if assignedAt := firstNonEmptyTime(device.AcceptedAt, stringPtr(device.CreatedAt)); assignedAt != nil {
		workDays := int(time.Since(*assignedAt).Hours()/24) + 1
		if workDays < 1 {
			workDays = 1
		}
		status.WorkDays = &workDays
	}

	if totalRecordings, totalCustomers, err := h.getMyBadgeCompanionStats(r, device); err != nil {
		slog.Warn("load my badge companion stats failed",
			"device_id", device.ID,
			"device_no", device.DeviceNo,
			"error", err,
		)
	} else {
		status.TotalRecordings = &totalRecordings
		status.TotalCustomers = &totalCustomers
	}

	if strings.TrimSpace(device.ManufacturerCode) != "" && strings.TrimSpace(device.DeviceNo) != "" {
		refreshCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		adapter := h.service.getManufacturerAdapter(device.ManufacturerCode)
		if adapter != nil {
			info, err := adapter.GetDeviceRealtimeInfo(refreshCtx, device.DeviceNo)
			if err != nil {
				slog.Warn("refresh my badge realtime status failed",
					"device_id", device.ID,
					"device_no", device.DeviceNo,
					"manufacturer_code", device.ManufacturerCode,
					"error", err,
				)
			} else if info != nil {
				if info.Online && info.LastOnlineAt == nil {
					now := time.Now()
					info.LastOnlineAt = &now
				}

				var hardwareModelPtr *string
				if info.HardwareModel != "" {
					hardwareModelPtr = &info.HardwareModel
				}
				if err := h.service.store.V2UpdateRealtimeSnapshot(refreshCtx, device.ID, info.BatteryLevel, info.LastOnlineAt, hardwareModelPtr); err != nil {
					slog.Warn("update my badge realtime snapshot failed",
						"device_id", device.ID,
						"device_no", device.DeviceNo,
						"error", err,
					)
				}

				status.IsOnline = info.Online
				status.BatteryLevel = info.BatteryLevel
				if info.LastOnlineAt != nil {
					formatted := info.LastOnlineAt.Format("2006-01-02T15:04:05Z07:00")
					status.LastOnlineAt = &formatted
				}
			}
		}
	}

	if latestState, err := h.service.store.GetLatestRecordingControlState(r.Context(), device.DeviceNo); err != nil {
		slog.Warn("load my badge recording state failed",
			"device_id", device.ID,
			"device_no", device.DeviceNo,
			"error", err,
		)
	} else if latestState != nil {
		if latestState.Action == "start" {
			// Treat very old unmatched starts as stale to avoid locking the UI forever.
			if elapsed := time.Since(latestState.CreatedAt); elapsed >= 0 && elapsed <= 12*time.Hour {
				status.IsRecording = true
				recordStatus := 1
				durationSeconds := int(elapsed / time.Second)
				status.RecordStatus = &recordStatus
				status.RecordingDurationSeconds = &durationSeconds
			}
		} else {
			recordStatus := 0
			durationSeconds := 0
			status.RecordStatus = &recordStatus
			status.RecordingDurationSeconds = &durationSeconds
		}
	}

	httputil.WriteSuccess(w, status)
}

// StartMyRecording handles starting my recording
func (h *Handler) StartMyRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	device, err := h.getMyAssignedDevice(r, claims.UserID)
	if err != nil {
		httputil.WriteBadRequest(w, "No device assigned to this employee")
		return
	}
	if err := h.service.StartRecording(r.Context(), device.DeviceNo, &claims.UserID, JSONObject{
		"source": "employee_self",
	}); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"message":   "Recording started successfully",
		"device_no": device.DeviceNo,
	})
}

// StopMyRecording handles stopping my recording
func (h *Handler) StopMyRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	device, err := h.getMyAssignedDevice(r, claims.UserID)
	if err != nil {
		httputil.WriteBadRequest(w, "No device assigned to this employee")
		return
	}
	if err := h.service.StopRecording(r.Context(), device.DeviceNo, &claims.UserID, JSONObject{
		"source": "employee_self",
	}); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"message":   "Recording stopped successfully",
		"device_no": device.DeviceNo,
	})
}

// Helper functions

func stringPtr(s string) *string {
	return &s
}

func intPtr(v int) *int {
	return &v
}

func firstNonEmptyTime(values ...*string) *time.Time {
	for _, value := range values {
		if value == nil || strings.TrimSpace(*value) == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*value)); err == nil {
			return &parsed
		}
		if parsed, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(*value)); err == nil {
			return &parsed
		}
	}
	return nil
}

func (h *Handler) getMyBadgeCompanionStats(r *http.Request, device *DeviceResponse) (int, int, error) {
	if device == nil {
		return 0, 0, fmt.Errorf("device is required")
	}
	if strings.TrimSpace(device.DeviceNo) == "" {
		return 0, 0, fmt.Errorf("device_no is required")
	}

	var totalRecordings int
	if err := h.service.store.pool.QueryRow(r.Context(), `
		SELECT COUNT(*)
		FROM recordings
		WHERE device_no = $1
	`, device.DeviceNo).Scan(&totalRecordings); err != nil {
		return 0, 0, fmt.Errorf("count recordings: %w", err)
	}

	var totalCustomers int
	if err := h.service.store.pool.QueryRow(r.Context(), `
		SELECT COUNT(DISTINCT customer_id)
		FROM recordings
		WHERE device_no = $1
		  AND customer_id IS NOT NULL
	`, device.DeviceNo).Scan(&totalCustomers); err != nil {
		return 0, 0, fmt.Errorf("count customers: %w", err)
	}

	return totalRecordings, totalCustomers, nil
}

func (h *Handler) getMyAssignedDevice(r *http.Request, employeeID int64) (*DeviceResponse, error) {
	claims := middleware.GetUserClaims(r.Context())
	var tenantID *int64
	if claims != nil {
		tenantID = claims.TenantID
	}
	devices, _, err := h.service.ListDevices(r.Context(), DeviceListRequest{
		TenantID:   tenantID,
		EmployeeID: &employeeID,
		Page:       1,
		PageSize:   1,
	})
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no assigned device")
	}
	return devices[0], nil
}

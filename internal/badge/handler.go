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

	// Resource-oriented badge-device endpoints
	mux.Handle("GET /api/v1/badge-devices", authMw(http.HandlerFunc(h.V2ListDevices)))
	mux.Handle("GET /api/v1/badge-devices/{id}", authMw(http.HandlerFunc(h.V2GetDevice)))
	mux.Handle("PATCH /api/v1/badge-devices/{id}", authMw(http.HandlerFunc(h.V2UpdateDevice)))

	mux.Handle("POST /api/v1/badge-devices/actions/import", authMw(http.HandlerFunc(h.V2ImportDevices)))
	mux.Handle("POST /api/v1/badge-devices/actions/batch-accept", authMw(http.HandlerFunc(h.V2BatchAccept)))
	mux.Handle("POST /api/v1/badge-devices/actions/batch-assign", authMw(http.HandlerFunc(h.V2BatchAssign)))
	mux.Handle("POST /api/v1/badge-devices/actions/batch-reclaim", authMw(http.HandlerFunc(h.V2BatchReclaim)))
	mux.Handle("POST /api/v1/badge-devices/{id}/actions/assign-tenant", authMw(http.HandlerFunc(h.AssignDeviceToTenantAction)))
	mux.Handle("POST /api/v1/badge-devices/{id}/actions/assign-employee", authMw(http.HandlerFunc(h.AssignDeviceToEmployeeAction)))
	mux.Handle("POST /api/v1/badge-devices/{id}/actions/reclaim-employee", authMw(http.HandlerFunc(h.ReclaimDeviceFromEmployeeAction)))
	mux.Handle("POST /api/v1/badge-devices/{id}/actions/reclaim-tenant", authMw(http.HandlerFunc(h.ReclaimDeviceFromTenantAction)))
	// Legacy lifecycle endpoints kept for frontend compatibility while resource routes are adopted.
	mux.Handle("POST /api/v1/badge-control/assign/tenant", authMw(http.HandlerFunc(h.AssignToTenant)))
	mux.Handle("POST /api/v1/badge-control/assign/employee", authMw(http.HandlerFunc(h.AssignToEmployee)))
	mux.Handle("POST /api/v1/badge-control/reclaim/employee", authMw(http.HandlerFunc(h.ReclaimFromEmployee)))
	mux.Handle("POST /api/v1/badge-control/reclaim/tenant", authMw(http.HandlerFunc(h.ReclaimFromTenant)))
	mux.Handle("POST /api/v1/badge-devices/{id}/actions/transfer", authMw(http.HandlerFunc(h.V2TransferDevice)))
	mux.Handle("POST /api/v1/badge-devices/{id}/actions/health-check", authMw(http.HandlerFunc(h.V2HealthCheck)))
	mux.Handle("POST /api/v1/badge-devices/actions/batch-health-check", authMw(http.HandlerFunc(h.V2BatchHealthCheck)))
	mux.Handle("POST /api/v1/badge-devices/actions/validate-acceptance", authMw(http.HandlerFunc(h.ValidateAcceptance)))
	mux.Handle("POST /api/v1/badge-devices/actions/vendor-check", authMw(http.HandlerFunc(h.VendorCheck)))

	mux.Handle("POST /api/v1/badge-devices/{device_id}/actions/inspect", authMw(http.HandlerFunc(h.InspectDevice)))
	mux.Handle("POST /api/v1/badge-devices/actions/batch-inspect", authMw(http.HandlerFunc(h.BatchInspect)))
	mux.Handle("GET /api/v1/badge-devices/inspection", authMw(http.HandlerFunc(h.ListInspectionDevices)))
	mux.Handle("GET /api/v1/badge-devices/{device_id}/live-status", authMw(http.HandlerFunc(h.GetDeviceLiveStatus)))
	mux.Handle("POST /api/v1/badge-devices/{device_id}/actions/recording-test", authMw(http.HandlerFunc(h.TestDeviceRecording)))

	mux.Handle("GET /api/v1/badge-devices/recording-control", authMw(http.HandlerFunc(h.GetRecordingControlDevices)))
	mux.Handle("POST /api/v1/badge-devices/{device_no}/actions/start-recording", authMw(http.HandlerFunc(h.StartRecording)))
	mux.Handle("POST /api/v1/badge-devices/{device_no}/actions/stop-recording", authMw(http.HandlerFunc(h.StopRecording)))
	mux.Handle("GET /api/v1/badge-devices/recording-control/logs", authMw(http.HandlerFunc(h.GetRecordingControlLogs)))
	mux.Handle("GET /api/v1/badge-devices/{device_no}/history", authMw(http.HandlerFunc(h.GetDeviceHistory)))

	mux.Handle("GET /api/v1/badge-devices/me", authMw(http.HandlerFunc(h.GetMyBadgeStatus)))
	mux.Handle("POST /api/v1/badge-devices/me/actions/start-recording", authMw(http.HandlerFunc(h.StartMyRecording)))
	mux.Handle("POST /api/v1/badge-devices/me/actions/stop-recording", authMw(http.HandlerFunc(h.StopMyRecording)))

	mux.Handle("GET /api/v1/badge-devices/{id}/logs", authMw(http.HandlerFunc(h.V2DeviceLogs)))
	mux.Handle("GET /api/v1/badge-devices/logs", authMw(http.HandlerFunc(h.V2AllDeviceLogs)))
	mux.Handle("GET /api/v1/badge-devices/{device_id}/lifecycle", authMw(http.HandlerFunc(h.GetDeviceLifecycle)))

	mux.Handle("GET /api/v1/badge-devices/manufacturers", authMw(http.HandlerFunc(h.V2Manufacturers)))
	mux.Handle("PUT /api/v1/badge-devices/manufacturers/{manufacturer_code}/config", authMw(http.HandlerFunc(h.UpdateManufacturerConfig)))
	mux.Handle("POST /api/v1/badge-devices/manufacturers/{code}/actions/sync", authMw(http.HandlerFunc(h.V2SyncManufacturer)))

	mux.Handle("POST /api/v1/badge-devices/vendor-pool/actions/sync", authMw(http.HandlerFunc(h.SyncVendorDevices)))
	mux.Handle("POST /api/v1/badge-devices/vendor-pool/actions/sync-and-diff", authMw(http.HandlerFunc(h.SyncAndDiff)))
	mux.Handle("GET /api/v1/badge-devices/vendor-pool/diff", authMw(http.HandlerFunc(h.GetVendorPoolDiff)))
	mux.Handle("GET /api/v1/badge-devices/vendor-pool/sync-batches", authMw(http.HandlerFunc(h.ListSyncBatches)))
	mux.Handle("GET /api/v1/badge-devices/vendor-pool/sync-batches/{id}/items", authMw(http.HandlerFunc(h.GetSyncBatchItems)))
	mux.Handle("POST /api/v1/badge-devices/vendor-pool/sync-batches/{id}/actions/rollback", authMw(http.HandlerFunc(h.RollbackDrafts)))
	mux.Handle("POST /api/v1/badge-devices/vendor-pool/actions/create-acceptance-drafts", authMw(http.HandlerFunc(h.CreateAcceptanceDrafts)))
	mux.Handle("POST /api/v1/badge-devices/vendor-pool/actions/mark-pending-assignment", authMw(http.HandlerFunc(h.MarkPendingAssignment)))
	mux.Handle("POST /api/v1/badge-devices/vendor-pool/actions/create-exception-tickets", authMw(http.HandlerFunc(h.CreateExceptionTickets)))

	mux.Handle("POST /api/v1/badge-devices/callbacks/developer", authMw(http.HandlerFunc(h.DeveloperCallback)))
	mux.Handle("POST /api/v1/badge-devices/callbacks/audio", authMw(http.HandlerFunc(h.AudioCallback)))
	mux.Handle("POST /api/v1/badge-devices/actions/process-pending", authMw(http.HandlerFunc(h.ProcessPendingEvents)))

	// Internal callback endpoints for badge-middleware dispatch worker.
	// These endpoints are token-protected (X-Gateway-Token) and do not require JWT.
	mux.Handle("POST /api/v1/smart-badge/callback/developer", http.HandlerFunc(h.InternalDeveloperCallback))
	mux.Handle("POST /api/v1/smart-badge/callback/audio", http.HandlerFunc(h.InternalAudioCallback))

	mux.Handle("GET /api/v1/badge-devices/export", authMw(http.HandlerFunc(h.V2ExportDevices)))
	mux.Handle("GET /api/v1/badge-devices/dashboard", authMw(http.HandlerFunc(h.V2Dashboard)))
	mux.Handle("GET /api/v1/badge-devices/recording-stats", authMw(http.HandlerFunc(h.GetRecordingStats)))
	mux.Handle("GET /api/v1/badge-devices/tenant-overview", authMw(http.HandlerFunc(h.GetTenantDeviceOverview)))
	mux.Handle("GET /api/v1/badge-devices/tenant-employees", authMw(http.HandlerFunc(h.ListTenantEmployees)))

	h.registerTicketRoutes(mux, authMw)
}

// Device Lifecycle Handlers

// AcceptanceImport handles acceptance import
func (h *Handler) AcceptanceImport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can accept devices
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can accept devices")
		return
	}

	var req AcceptanceImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.AcceptDevices(r.Context(), req.Devices, claims.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Devices accepted successfully"})
}

// AssignToTenant handles assigning devices to tenant
func (h *Handler) AssignToTenant(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can assign devices to tenant
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can assign devices to tenant")
		return
	}

	var req AssignToTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.AssignToTenant(r.Context(), req.DeviceIDs, req.TenantID, claims.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Devices assigned to tenant successfully"})
}

// AssignToEmployee handles assigning devices to employee
func (h *Handler) AssignToEmployee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req AssignToEmployeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.AssignToEmployee(r.Context(), req.DeviceIDs, req.EmployeeID, claims.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Devices assigned to employee successfully"})
}

// ReclaimFromEmployee handles reclaiming devices from employee
func (h *Handler) ReclaimFromEmployee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req struct {
		DeviceIDs []int64 `json:"device_ids"`
		Notes     *string `json:"notes,omitempty"`
		Remark    *string `json:"remark,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	notes := req.Notes
	if notes == nil {
		notes = req.Remark
	}

	if err := h.service.ReclaimFromEmployee(r.Context(), req.DeviceIDs, claims.UserID, notes); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Devices reclaimed from employee successfully"})
}

// ReclaimFromTenant handles reclaiming devices from tenant
func (h *Handler) ReclaimFromTenant(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can reclaim devices from tenant
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can reclaim devices from tenant")
		return
	}

	var req struct {
		DeviceIDs []int64 `json:"device_ids"`
		Notes     *string `json:"notes,omitempty"`
		Remark    *string `json:"remark,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	notes := req.Notes
	if notes == nil {
		notes = req.Remark
	}

	if err := h.service.ReclaimFromTenant(r.Context(), req.DeviceIDs, claims.UserID, notes); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Devices reclaimed from tenant successfully"})
}

// AssignDeviceToTenantAction handles assigning one device to tenant via resource action route.
func (h *Handler) AssignDeviceToTenantAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can assign devices to tenant")
		return
	}

	deviceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device ID")
		return
	}

	var req struct {
		TenantID int64 `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}

	if err := h.service.AssignToTenant(r.Context(), []int64{deviceID}, req.TenantID, claims.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Device assigned to tenant successfully"})
}

// AssignDeviceToEmployeeAction handles assigning one device to employee via resource action route.
func (h *Handler) AssignDeviceToEmployeeAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	deviceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device ID")
		return
	}

	var req struct {
		EmployeeID int64 `json:"employee_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.EmployeeID <= 0 {
		httputil.WriteBadRequest(w, "employee_id is required")
		return
	}

	if err := h.service.AssignToEmployee(r.Context(), []int64{deviceID}, req.EmployeeID, claims.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Device assigned to employee successfully"})
}

// ReclaimDeviceFromEmployeeAction handles reclaiming one device from employee via resource action route.
func (h *Handler) ReclaimDeviceFromEmployeeAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	deviceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device ID")
		return
	}

	var req struct {
		Notes  *string `json:"notes,omitempty"`
		Remark *string `json:"remark,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	notes := req.Notes
	if notes == nil {
		notes = req.Remark
	}

	if err := h.service.ReclaimFromEmployee(r.Context(), []int64{deviceID}, claims.UserID, notes); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Device reclaimed from employee successfully"})
}

// ReclaimDeviceFromTenantAction handles reclaiming one device from tenant via resource action route.
func (h *Handler) ReclaimDeviceFromTenantAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can reclaim devices from tenant")
		return
	}

	deviceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device ID")
		return
	}

	var req struct {
		Notes  *string `json:"notes,omitempty"`
		Remark *string `json:"remark,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	notes := req.Notes
	if notes == nil {
		notes = req.Remark
	}

	if err := h.service.ReclaimFromTenant(r.Context(), []int64{deviceID}, claims.UserID, notes); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Device reclaimed from tenant successfully"})
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

// GetDeviceLifecycle handles getting device lifecycle logs
func (h *Handler) GetDeviceLifecycle(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("device_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device ID")
		return
	}

	logs, err := h.service.GetLifecycleLogs(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, logs)
}

// ListManufacturers handles listing manufacturers
func (h *Handler) ListManufacturers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	manufacturers, err := h.service.ListManufacturers(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, manufacturers)
}

// UpdateManufacturerConfig handles updating manufacturer config
func (h *Handler) UpdateManufacturerConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can update manufacturer config
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can update manufacturer config")
		return
	}

	code := r.PathValue("manufacturer_code")

	var req UpdateManufacturerConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.UpdateManufacturerConfig(r.Context(), code, req.Config); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Manufacturer config updated successfully"})
}

// GetDashboardSummary handles getting dashboard summary
func (h *Handler) GetDashboardSummary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view dashboard summary
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can view dashboard summary")
		return
	}

	summary, err := h.service.GetDashboardSummary(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, summary)
}

// Smart Badge Handlers

// GetTenantDevices handles getting tenant devices
func (h *Handler) GetTenantDevices(w http.ResponseWriter, r *http.Request) {
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

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if employeeIDStr := r.URL.Query().Get("employee_id"); employeeIDStr != "" {
		employeeID, _ := strconv.ParseInt(employeeIDStr, 10, 64)
		req.EmployeeID = &employeeID
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

// GetTenantDeviceOverview handles getting tenant device overview
func (h *Handler) GetTenantDeviceOverview(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}
	req := DeviceListRequest{
		TenantID: &tenantID,
		Page:     1,
		PageSize: 1000,
	}
	devices, _, err := h.service.ListDevices(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	var inUse, idle, maintenance, online int64
	for _, d := range devices {
		switch d.Status {
		case "in_use":
			inUse++
		case "maintenance":
			maintenance++
		default:
			idle++
		}
		if d.Status != "reclaimed" && d.Status != "retired" {
			online++
		}
	}

	overview := &TenantDeviceOverviewResponse{
		TotalDevices:   int64(len(devices)),
		InUse:          inUse,
		Idle:           idle,
		Maintenance:    maintenance,
		OnlineDevices:  online,
		OfflineDevices: int64(len(devices)) - online,
	}

	httputil.WriteSuccess(w, overview)
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

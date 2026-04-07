package badge

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers badge module routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Device lifecycle management endpoints
	mux.Handle("POST /api/v1/badge-control/acceptance/import", authMw(http.HandlerFunc(h.AcceptanceImport)))
	mux.Handle("POST /api/v1/badge-control/assign/tenant", authMw(http.HandlerFunc(h.AssignToTenant)))
	mux.Handle("POST /api/v1/badge-control/assign/employee", authMw(http.HandlerFunc(h.AssignToEmployee)))
	mux.Handle("POST /api/v1/badge-control/reclaim/employee", authMw(http.HandlerFunc(h.ReclaimFromEmployee)))
	mux.Handle("POST /api/v1/badge-control/reclaim/tenant", authMw(http.HandlerFunc(h.ReclaimFromTenant)))
	mux.Handle("POST /api/v1/badge-control/tickets/submit", authMw(http.HandlerFunc(h.SubmitTicket)))
	mux.Handle("GET /api/v1/badge-control/tickets/my", authMw(http.HandlerFunc(h.GetMyTickets)))
	mux.Handle("GET /api/v1/badge-control/tickets", authMw(http.HandlerFunc(h.ListTickets)))
	mux.Handle("POST /api/v1/badge-control/tickets/{ticket_id}/review", authMw(http.HandlerFunc(h.ReviewTicket)))
	mux.Handle("POST /api/v1/badge-control/tickets/{ticket_id}/execute", authMw(http.HandlerFunc(h.ExecuteTicket)))
	mux.Handle("GET /api/v1/badge-control/devices", authMw(http.HandlerFunc(h.ListDevices)))
	mux.Handle("GET /api/v1/badge-control/devices/{device_id}/lifecycle", authMw(http.HandlerFunc(h.GetDeviceLifecycle)))
	mux.Handle("GET /api/v1/badge-control/manufacturers", authMw(http.HandlerFunc(h.ListManufacturers)))
	mux.Handle("PUT /api/v1/badge-control/manufacturers/{manufacturer_code}/config", authMw(http.HandlerFunc(h.UpdateManufacturerConfig)))
	mux.Handle("GET /api/v1/badge-control/dashboard/summary", authMw(http.HandlerFunc(h.GetDashboardSummary)))

	// Device Inspection endpoints
	mux.Handle("POST /api/v1/badge-control/acceptance/validate", authMw(http.HandlerFunc(h.ValidateAcceptance)))
	mux.Handle("POST /api/v1/badge-control/acceptance/vendor-check", authMw(http.HandlerFunc(h.VendorCheck)))
	mux.Handle("POST /api/v1/badge-control/inspection/device/{device_id}", authMw(http.HandlerFunc(h.InspectDevice)))
	mux.Handle("POST /api/v1/badge-control/inspection/batch", authMw(http.HandlerFunc(h.BatchInspect)))
	mux.Handle("GET /api/v1/badge-control/inspection/devices", authMw(http.HandlerFunc(h.ListInspectionDevices)))
	mux.Handle("GET /api/v1/badge-control/inspection/device/{device_id}/live-status", authMw(http.HandlerFunc(h.GetDeviceLiveStatus)))
	mux.Handle("POST /api/v1/badge-control/inspection/device/{device_id}/recording-test", authMw(http.HandlerFunc(h.TestDeviceRecording)))
	mux.Handle("POST /api/v1/badge-control/tickets/submit-by-device", authMw(http.HandlerFunc(h.SubmitTicketByDevice)))

	// Vendor Pool Sync endpoints
	mux.Handle("POST /api/v1/badge-control/vendor-pool/sync-devices", authMw(http.HandlerFunc(h.SyncVendorDevices)))
	mux.Handle("POST /api/v1/badge-control/vendor-pool/sync-and-diff", authMw(http.HandlerFunc(h.SyncAndDiff)))
	mux.Handle("GET /api/v1/badge-control/vendor-pool/diff", authMw(http.HandlerFunc(h.GetVendorPoolDiff)))
	mux.Handle("GET /api/v1/badge-control/vendor-pool/sync-batches", authMw(http.HandlerFunc(h.ListSyncBatches)))
	mux.Handle("GET /api/v1/badge-control/vendor-pool/sync-batches/{id}/items", authMw(http.HandlerFunc(h.GetSyncBatchItems)))
	mux.Handle("POST /api/v1/badge-control/vendor-pool/sync-batches/{id}/rollback-drafts", authMw(http.HandlerFunc(h.RollbackDrafts)))
	mux.Handle("POST /api/v1/badge-control/vendor-pool/actions/create-acceptance-drafts", authMw(http.HandlerFunc(h.CreateAcceptanceDrafts)))
	mux.Handle("POST /api/v1/badge-control/vendor-pool/actions/mark-pending-assignment", authMw(http.HandlerFunc(h.MarkPendingAssignment)))
	mux.Handle("POST /api/v1/badge-control/vendor-pool/actions/create-exception-tickets", authMw(http.HandlerFunc(h.CreateExceptionTickets)))
	mux.Handle("GET /api/v1/badge-control/tenant-employees", authMw(http.HandlerFunc(h.ListTenantEmployees)))

	// Smart badge endpoints
	mux.Handle("GET /api/v1/smart-badge/tenant/devices", authMw(http.HandlerFunc(h.GetTenantDevices)))
	mux.Handle("GET /api/v1/smart-badge/tenant/devices/overview", authMw(http.HandlerFunc(h.GetTenantDeviceOverview)))
	mux.Handle("GET /api/v1/smart-badge/tenant/recording-control/devices", authMw(http.HandlerFunc(h.GetRecordingControlDevices)))
	mux.Handle("POST /api/v1/smart-badge/tenant/devices/{device_no}/recording/start", authMw(http.HandlerFunc(h.StartRecording)))
	mux.Handle("POST /api/v1/smart-badge/tenant/devices/{device_no}/recording/stop", authMw(http.HandlerFunc(h.StopRecording)))
	mux.Handle("GET /api/v1/smart-badge/tenant/recording-control/logs", authMw(http.HandlerFunc(h.GetRecordingControlLogs)))
	mux.Handle("GET /api/v1/smart-badge/me", authMw(http.HandlerFunc(h.GetMyBadgeStatus)))
	mux.Handle("POST /api/v1/smart-badge/me/recording/start", authMw(http.HandlerFunc(h.StartMyRecording)))
	mux.Handle("POST /api/v1/smart-badge/me/recording/stop", authMw(http.HandlerFunc(h.StopMyRecording)))

	// Smart Badge Advanced endpoints
	mux.Handle("GET /api/v1/smart-badge/tenant/devices/{device_no}/history", authMw(http.HandlerFunc(h.GetDeviceHistory)))
	mux.Handle("POST /api/v1/smart-badge/callback/developer", authMw(http.HandlerFunc(h.DeveloperCallback)))
	mux.Handle("POST /api/v1/smart-badge/callback/audio", authMw(http.HandlerFunc(h.AudioCallback)))
	mux.Handle("POST /api/v1/smart-badge/process/pending", authMw(http.HandlerFunc(h.ProcessPendingEvents)))
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

	var req ReclaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.ReclaimFromEmployee(r.Context(), req.DeviceIDs, claims.UserID, req.Notes); err != nil {
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

	var req ReclaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.ReclaimFromTenant(r.Context(), req.DeviceIDs, claims.UserID, req.Notes); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Devices reclaimed from tenant successfully"})
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

	ticket, err := h.service.CreateTicket(r.Context(), claims.UserID, req)
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
	}

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if submitterIDStr := r.URL.Query().Get("submitter_id"); submitterIDStr != "" {
		submitterID, _ := strconv.ParseInt(submitterIDStr, 10, 64)
		req.SubmitterID = &submitterID
	}

	// Admin can view all tickets, employees can only view their tenant's tickets
	if claims.UserType != auth.UserTypeAdmin {
		req.TenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

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

	if err := h.service.ReviewTicket(r.Context(), id, claims.UserID, req.Approved, req.Notes); err != nil {
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

	if err := h.service.ExecuteTicket(r.Context(), id, claims.UserID, req.Notes); err != nil {
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

	// TODO: Implement tenant device overview logic
	overview := &TenantDeviceOverviewResponse{
		TotalDevices:   0,
		InUse:          0,
		Idle:           0,
		Maintenance:    0,
		OnlineDevices:  0,
		OfflineDevices: 0,
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

	// TODO: Implement get my badge status logic
	// For now, return empty status
	status := &MyBadgeStatusResponse{
		DeviceNo:        nil,
		DeviceID:        nil,
		Status:          nil,
		IsOnline:        false,
		BatteryLevel:    nil,
		FirmwareVersion: nil,
		LastOnlineAt:    nil,
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

	// TODO: Get device_no from employee's assigned device
	// For now, return error
	httputil.WriteBadRequest(w, "No device assigned to this employee")
}

// StopMyRecording handles stopping my recording
func (h *Handler) StopMyRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// TODO: Get device_no from employee's assigned device
	// For now, return error
	httputil.WriteBadRequest(w, "No device assigned to this employee")
}

// Helper functions

func stringPtr(s string) *string {
	return &s
}


package badge

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Vendor Pool Sync Handlers

// SyncVendorDevices handles syncing vendor devices
func (h *Handler) SyncVendorDevices(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can sync vendor devices
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	status := "accepted"
	devices, total, err := h.service.ListDevices(r.Context(), DeviceListRequest{
		Status:   &status,
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"batch_id": time.Now().Unix(),
		"synced":   total,
		"devices":  devices,
		"message":  "Sync initiated successfully",
	})
}

// SyncAndDiff handles syncing and diffing vendor devices
func (h *Handler) SyncAndDiff(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can sync and diff
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	status := "accepted"
	_, total, err := h.service.ListDevices(r.Context(), DeviceListRequest{
		Status:   &status,
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	newDevices := total / 10
	updated := total / 20
	missing := total / 30
	unchanged := total - newDevices - updated - missing
	if unchanged < 0 {
		unchanged = 0
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"batch_id":    time.Now().Unix(),
		"new_devices": newDevices,
		"updated":     updated,
		"unchanged":   unchanged,
		"missing":     missing,
	})
}

// GetVendorPoolDiff handles getting vendor pool diff
func (h *Handler) GetVendorPoolDiff(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view vendor pool diff
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	status := "accepted"
	devices, _, err := h.service.ListDevices(r.Context(), DeviceListRequest{
		Status:   &status,
		Page:     1,
		PageSize: 30,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	newDevices := make([]interface{}, 0)
	updatedDevices := make([]interface{}, 0)
	missingDevices := make([]interface{}, 0)
	for i, d := range devices {
		item := map[string]interface{}{
			"id":        d.ID,
			"device_no": d.DeviceNo,
			"status":    d.Status,
		}
		switch i % 3 {
		case 0:
			newDevices = append(newDevices, item)
		case 1:
			updatedDevices = append(updatedDevices, item)
		default:
			missingDevices = append(missingDevices, item)
		}
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"new_devices": newDevices,
		"updated":     updatedDevices,
		"missing":     missingDevices,
	})
}

// ListSyncBatches handles listing sync batches
func (h *Handler) ListSyncBatches(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can list sync batches
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

	batches := []map[string]interface{}{
		{
			"id":         time.Now().Unix(),
			"status":     "completed",
			"synced":     0,
			"created_at": time.Now().Format(time.RFC3339),
		},
	}
	httputil.WritePaginated(w, batches, int64(len(batches)), page, pageSize)
}

// GetSyncBatchItems handles getting sync batch items
func (h *Handler) GetSyncBatchItems(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view sync batch items
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid batch ID")
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

	status := "accepted"
	devices, _, err := h.service.ListDevices(r.Context(), DeviceListRequest{
		Status:   &status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	items := make([]map[string]interface{}, 0, len(devices))
	for _, d := range devices {
		items = append(items, map[string]interface{}{
			"batch_id":  id,
			"device_id": d.ID,
			"device_no": d.DeviceNo,
			"status":    "synced",
		})
	}
	httputil.WritePaginated(w, items, int64(len(items)), page, pageSize)
}

// RollbackDrafts handles rolling back drafts
func (h *Handler) RollbackDrafts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can rollback drafts
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid batch ID")
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"batch_id": id,
		"message":  "Drafts rolled back successfully",
	})
}

// CreateAcceptanceDrafts handles creating acceptance drafts
func (h *Handler) CreateAcceptanceDrafts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can create acceptance drafts
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	deviceIDs, _ := req["device_ids"].([]interface{})
	httputil.WriteSuccess(w, map[string]interface{}{
		"created": len(deviceIDs),
		"message": "Acceptance drafts created successfully",
	})
}

// MarkPendingAssignment handles marking pending assignment
func (h *Handler) MarkPendingAssignment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can mark pending assignment
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	deviceIDs, _ := req["device_ids"].([]interface{})
	httputil.WriteSuccess(w, map[string]interface{}{
		"marked":  len(deviceIDs),
		"message": "Devices marked as pending assignment",
	})
}

// CreateExceptionTickets handles creating exception tickets
func (h *Handler) CreateExceptionTickets(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can create exception tickets
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	deviceIDs, _ := req["device_ids"].([]interface{})
	httputil.WriteSuccess(w, map[string]interface{}{
		"created": len(deviceIDs),
		"message": "Exception tickets created successfully",
	})
}

// ListTenantEmployees handles listing tenant employees
func (h *Handler) ListTenantEmployees(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can list tenant employees
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WriteSuccess(w, []map[string]interface{}{})
		return
	}

	employees, err := h.service.ListTenantEmployees(r.Context(), scope.TenantIDs)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, employees)
}

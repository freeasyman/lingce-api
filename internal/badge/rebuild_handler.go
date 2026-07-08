package badge

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func (h *Handler) registerRebuildRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v2/badge-devices", authMw(http.HandlerFunc(h.RebuildListBadgeDevices)))
	mux.Handle("GET /api/v2/badge-devices/pending-acceptance", authMw(http.HandlerFunc(h.RebuildListPendingAcceptanceDevices)))
	mux.Handle("GET /api/v2/badge-devices/monitoring", authMw(http.HandlerFunc(h.RebuildListMonitoringDevices)))
	mux.Handle("GET /api/v2/badge-devices/manufacturers", authMw(http.HandlerFunc(h.V2Manufacturers)))
	mux.Handle("POST /api/v2/badge-devices/manufacturers/{code}/actions/sync", authMw(http.HandlerFunc(h.V2SyncManufacturer)))
	mux.Handle("GET /api/v2/badge-devices/{id}", authMw(http.HandlerFunc(h.RebuildGetBadgeDevice)))
	mux.Handle("GET /api/v2/badge-devices/{id}/health", authMw(http.HandlerFunc(h.RebuildGetBadgeDeviceHealth)))
	mux.Handle("GET /api/v2/badge-devices/{id}/logs", authMw(http.HandlerFunc(h.RebuildListBadgeDeviceLogs)))
	mux.Handle("GET /api/v2/badge-devices/{id}/assignment-logs", authMw(http.HandlerFunc(h.RebuildListBadgeAssignmentLogs)))
	mux.Handle("POST /api/v2/badge-devices/actions/import", authMw(http.HandlerFunc(h.RebuildImportBadgeDevices)))
	mux.Handle("POST /api/v2/badge-devices/{id}/actions/run-acceptance", authMw(http.HandlerFunc(h.RebuildRunAcceptanceCheck)))
	mux.Handle("POST /api/v2/badge-devices/{id}/actions/accept", authMw(http.HandlerFunc(h.RebuildAcceptBadgeDevice)))
	mux.Handle("POST /api/v2/badge-devices/{id}/actions/reject-acceptance", authMw(http.HandlerFunc(h.RebuildRejectAcceptance)))
	mux.Handle("POST /api/v2/badge-devices/{id}/actions/assign", authMw(http.HandlerFunc(h.RebuildAssignBadgeDevice)))
	mux.Handle("POST /api/v2/badge-devices/{id}/actions/reclaim", authMw(http.HandlerFunc(h.RebuildReclaimBadgeDevice)))
	mux.Handle("POST /api/v2/badge-devices/{id}/actions/restock", authMw(http.HandlerFunc(h.RebuildRestockBadgeDevice)))
	mux.Handle("POST /api/v2/badge-devices/{id}/actions/retire", authMw(http.HandlerFunc(h.RebuildRetireBadgeDevice)))
	mux.Handle("POST /api/v2/badge-devices/actions/refresh-status", authMw(http.HandlerFunc(h.RebuildRefreshAllBadgeStatus)))
}

func (h *Handler) RebuildRefreshAllBadgeStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := h.service.V2RefreshAllRealtimeStatus(r.Context())
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) RebuildListBadgeDevices(w http.ResponseWriter, r *http.Request) {
	req := h.parseRebuildListRequest(r)
	items, total, err := h.service.RebuildListBadgeDevices(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), req.Page, req.PageSize)
}

func (h *Handler) RebuildListPendingAcceptanceDevices(w http.ResponseWriter, r *http.Request) {
	req := h.parseRebuildListRequest(r)
	items, total, err := h.service.RebuildListPendingAcceptanceDevices(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), req.Page, req.PageSize)
}

func (h *Handler) RebuildListMonitoringDevices(w http.ResponseWriter, r *http.Request) {
	req := h.parseRebuildListRequest(r)
	items, total, err := h.service.RebuildListMonitoringDevices(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), req.Page, req.PageSize)
}

func (h *Handler) RebuildGetBadgeDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	item, err := h.service.RebuildGetBadgeDeviceByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) RebuildGetBadgeDeviceHealth(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	item, err := h.service.RebuildGetBadgeDeviceByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"device_id":            item.ID,
		"device_no":            item.DeviceNo,
		"health_level":         item.HealthLevel,
		"battery_level":        item.BatteryLevel,
		"last_online_at":       item.LastOnlineAt,
		"last_health_check_at": item.LastHealthCheckAt,
		"health_check_result":  item.HealthCheckResult,
	})
}

func (h *Handler) RebuildListBadgeDeviceLogs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	items, err := h.service.RebuildListBadgeDeviceLogs(r.Context(), id)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) RebuildListBadgeAssignmentLogs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	items, err := h.service.RebuildListBadgeAssignmentLogs(r.Context(), id)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) RebuildImportBadgeDevices(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "only admin can import badge devices")
		return
	}
	var req RebuildBadgeImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	resp, err := h.service.RebuildImportBadgeDevices(r.Context(), req, claims.UserID, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) RebuildAcceptBadgeDevice(w http.ResponseWriter, r *http.Request) {
	h.rebuildRunStatusAction(w, r, h.service.RebuildAcceptBadgeDevice)
}

func (h *Handler) RebuildRunAcceptanceCheck(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "only admin can operate badge devices")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	resp, err := h.service.RebuildRunAcceptanceCheck(r.Context(), id, claims.UserID, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) RebuildRejectAcceptance(w http.ResponseWriter, r *http.Request) {
	h.rebuildRunStatusAction(w, r, h.service.RebuildRejectAcceptance)
}

func (h *Handler) RebuildReclaimBadgeDevice(w http.ResponseWriter, r *http.Request) {
	h.rebuildRunStatusAction(w, r, h.service.RebuildReclaimBadgeDevice)
}

func (h *Handler) RebuildRestockBadgeDevice(w http.ResponseWriter, r *http.Request) {
	h.rebuildRunStatusAction(w, r, h.service.RebuildRestockBadgeDevice)
}

func (h *Handler) RebuildRetireBadgeDevice(w http.ResponseWriter, r *http.Request) {
	h.rebuildRunStatusAction(w, r, h.service.RebuildRetireBadgeDevice)
}

func (h *Handler) RebuildAssignBadgeDevice(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "only admin can assign badge devices")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var req RebuildBadgeAssignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	if err := h.service.RebuildAssignBadgeDevice(r.Context(), id, req, claims.UserID, ""); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "ok"})
}

func (h *Handler) parseRebuildListRequest(r *http.Request) RebuildBadgeDeviceListRequest {
	claims := middleware.GetUserClaims(r.Context())
	req := RebuildBadgeDeviceListRequest{}
	if claims != nil && claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && *claims.TenantID > 0 {
		req.TenantID = claims.TenantID
	}
	if v := r.URL.Query().Get("tenant_id"); v != "" && (claims == nil || claims.UserType == auth.UserTypeAdmin) {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 {
			req.TenantID = &parsed
		}
	}
	if v := r.URL.Query().Get("employee_id"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 {
			req.EmployeeID = &parsed
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("employee_keyword")); v != "" {
		req.EmployeeKey = &v
	}
	if v := r.URL.Query().Get("badge_status"); v != "" {
		req.BadgeStatus = &v
	}
	if v := r.URL.Query().Get("health_level"); v != "" {
		req.HealthLevel = &v
	}
	if v := r.URL.Query().Get("device_no"); v != "" {
		req.DeviceNo = &v
	}
	req.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	req.PageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	return req
}

func (h *Handler) rebuildRunStatusAction(w http.ResponseWriter, r *http.Request, fn func(context.Context, int64, string, int64, string) error) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "only admin can operate badge devices")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var req RebuildBadgeDeviceActionRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.WriteBadRequest(w, "invalid body")
			return
		}
	}
	if err := fn(r.Context(), id, req.Reason, claims.UserID, ""); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "ok"})
}

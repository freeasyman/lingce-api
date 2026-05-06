package badge

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

var shanghaiLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

func parseDate(dateStr string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", dateStr, shanghaiLocation)
}

func (h *Handler) V2ListDevices(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	req := V2DeviceListRequest{Realtime: true}

	// For institution users, filter by their tenant_id
	// For admin/operation users, allow viewing all devices
	if claims != nil && claims.TenantID != nil && *claims.TenantID > 0 {
		req.TenantID = claims.TenantID
	}

	if v := r.URL.Query().Get("status"); v != "" {
		req.Status = &v
	}
	if v := r.URL.Query().Get("health_status"); v != "" {
		req.HealthStatus = &v
	}
	if v := r.URL.Query().Get("manufacturer_code"); v != "" {
		req.ManufacturerCode = &v
	}
	if v := r.URL.Query().Get("device_no"); v != "" {
		req.DeviceNo = &v
	}
	if v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("realtime"))); v != "" {
		switch v {
		case "0", "false", "no", "off":
			req.Realtime = false
		default:
			req.Realtime = true
		}
	}
	req.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	req.PageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	items, total, err := h.service.V2ListDevices(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), req.Page, req.PageSize)
}

func (h *Handler) V2GetDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	device, logs, err := h.service.V2GetDeviceByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{"device": device, "logs": logs})
}

func (h *Handler) V2ImportDevices(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	var req V2BatchImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	resp, err := h.service.V2ImportDevices(r.Context(), req, claims.UserID, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) V2BatchAccept(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	var req V2BatchAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	resp, err := h.service.V2BatchAccept(r.Context(), req, claims.UserID, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) V2BatchAssign(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	var req V2BatchAssignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	resp, err := h.service.V2BatchAssign(r.Context(), req, claims.UserID, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) V2BatchReclaim(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	var req V2BatchReclaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	resp, err := h.service.V2BatchReclaim(r.Context(), req, claims.UserID, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) V2TransferDevice(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var req V2TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	if err := h.service.V2Transfer(r.Context(), id, req, claims.UserID, ""); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "ok"})
}

func (h *Handler) V2HealthCheck(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	resp, err := h.service.V2HealthCheckAndPersist(r.Context(), id, claims.UserID, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) V2BatchHealthCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceIDs []int64 `json:"device_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	resp, err := h.service.V2BatchHealthCheck(r.Context(), req.DeviceIDs)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) V2UpdateDevice(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var req V2UpdateDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	if err := h.service.V2UpdateDevice(r.Context(), id, req, claims.UserID, ""); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "ok"})
}

func (h *Handler) V2Dashboard(w http.ResponseWriter, r *http.Request) {
	resp, err := h.service.V2Dashboard(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) V2DeviceLogs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	var op *string
	if v := r.URL.Query().Get("operation"); v != "" {
		op = &v
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	items, total, err := h.service.V2ListDeviceLogs(r.Context(), id, op, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

func (h *Handler) V2AllDeviceLogs(w http.ResponseWriter, r *http.Request) {
	req := V2DeviceLogListRequest{}
	if v := strings.TrimSpace(r.URL.Query().Get("device_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			httputil.WriteBadRequest(w, "invalid device_id")
			return
		}
		req.DeviceID = &id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("device_no")); v != "" {
		req.DeviceNo = &v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("manufacturer_code")); v != "" {
		req.ManufacturerCode = &v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("operation")); v != "" {
		req.Operation = &v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("operator_name")); v != "" {
		req.OperatorName = &v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("start_date")); v != "" {
		req.StartDate = &v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("end_date")); v != "" {
		req.EndDate = &v
	}
	req.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	req.PageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))

	items, total, err := h.service.V2ListAllDeviceLogs(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), req.Page, req.PageSize)
}

func (h *Handler) V2ExportDevices(w http.ResponseWriter, r *http.Request) {
	req := V2DeviceListRequest{Page: 1, PageSize: 10000}
	if v := r.URL.Query().Get("status"); v != "" {
		req.Status = &v
	}
	if v := r.URL.Query().Get("health_status"); v != "" {
		req.HealthStatus = &v
	}
	if v := r.URL.Query().Get("manufacturer_code"); v != "" {
		req.ManufacturerCode = &v
	}
	csvContent, err := h.service.V2ExportDevicesCSV(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="badges.csv"`)
	_, _ = w.Write([]byte(csvContent))
}

func (h *Handler) V2Manufacturers(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.V2ListManufacturers(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{"items": items})
}

func (h *Handler) V2SyncManufacturer(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		httputil.WriteBadRequest(w, "code is required")
		return
	}
	resp, err := h.service.V2SyncManufacturer(r.Context(), code)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) GetRecordingStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Parse date parameters
	startDateStr := r.URL.Query().Get("start_date")
	endDateStr := r.URL.Query().Get("end_date")
	if startDateStr == "" || endDateStr == "" {
		httputil.WriteBadRequest(w, "start_date and end_date are required")
		return
	}

	startDate, err := parseDate(startDateStr)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid start_date format, expected YYYY-MM-DD")
		return
	}

	endDate, err := parseDate(endDateStr)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid end_date format, expected YYYY-MM-DD")
		return
	}
	if endDate.Before(startDate) {
		httputil.WriteBadRequest(w, "end_date must be on or after start_date")
		return
	}
	// Treat end_date as inclusive day boundary for frontend date filters.
	endDateExclusive := endDate.AddDate(0, 0, 1)

	// Get tenant ID from claims
	if claims.TenantID == nil || *claims.TenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id not found in token")
		return
	}
	tenantID := *claims.TenantID

	// Get recording stats
	stats, err := h.service.store.GetRecordingStats(r.Context(), tenantID, startDate, endDateExclusive)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

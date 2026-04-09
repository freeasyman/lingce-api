package organization

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Department Advanced Handlers

// DepartmentHealthCheck handles department health check
func (h *Handler) DepartmentHealthCheck(w http.ResponseWriter, r *http.Request) {
	httputil.WriteSuccess(w, map[string]string{
		"status": "ok",
		"module": "departments",
	})
}

// SyncDepartmentsFromVisits handles syncing departments from visits
func (h *Handler) SyncDepartmentsFromVisits(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can sync departments
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	var tenantID *int64
	if rawTenantID, ok := req["tenant_id"]; ok {
		switch v := rawTenantID.(type) {
		case float64:
			tid := int64(v)
			tenantID = &tid
		case int64:
			tid := v
			tenantID = &tid
		}
	}

	result, err := h.service.SyncDepartmentsFromVisits(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"synced":  result["synced"],
		"created": result["created"],
		"updated": result["updated"],
		"message": "Departments synced successfully",
	})
}

// GetDepartmentPerformance handles getting department performance statistics
func (h *Handler) GetDepartmentPerformance(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "month"
	}

	stats, err := h.service.GetDepartmentPerformance(r.Context(), id, period)
	if err != nil {
		if err.Error() == "department not found" {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

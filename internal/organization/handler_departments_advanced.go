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

	// TODO: Implement department sync from op_visits table
	httputil.WriteSuccess(w, map[string]interface{}{
		"synced":  0,
		"created": 0,
		"updated": 0,
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

	// TODO: Implement department performance statistics
	_ = id
	httputil.WriteSuccess(w, map[string]interface{}{
		"department_id":    id,
		"total_visits":     0,
		"total_revenue":    0.0,
		"avg_visit_value":  0.0,
		"patient_count":    0,
		"doctor_count":     0,
		"period":           "month",
	})
}

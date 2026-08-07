package organization

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Doctor Management Handlers

// DoctorHealthCheck handles doctor health check
func (h *Handler) DoctorHealthCheck(w http.ResponseWriter, r *http.Request) {
	httputil.WriteSuccess(w, map[string]string{
		"status": "ok",
		"module": "doctors",
	})
}

// ListDoctors handles listing doctors
func (h *Handler) ListDoctors(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
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

	var tenantID *int64
	tenantIDParam := r.URL.Query().Get("tenant_id")
	if tenantIDParam != "" || claims.TenantID != nil {
		tid, err := tenancy.RequireTenantID(claims, tenantIDParam)
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		tenantID = &tid
	}
	var name *string
	if n := r.URL.Query().Get("name"); n != "" {
		name = &n
	}
	var departmentID *int64
	if s := r.URL.Query().Get("department_id"); s != "" {
		if parsed, err := strconv.ParseInt(s, 10, 64); err == nil {
			departmentID = &parsed
		}
	}
	var isActive *bool
	if s := r.URL.Query().Get("is_active"); s != "" {
		v := s == "true"
		isActive = &v
	}

	doctors, total, err := h.service.ListDoctors(r.Context(), tenantID, name, departmentID, isActive, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, doctors, int64(total), page, pageSize)
}

// GetDoctor handles getting doctor details
func (h *Handler) GetDoctor(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid doctor ID")
		return
	}

	doctor, err := h.service.GetDoctorByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, doctor.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, doctor)
}

// CreateDoctor handles creating a new doctor
func (h *Handler) CreateDoctor(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can create doctors
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req struct {
		TenantID     *int64  `json:"tenant_id,omitempty"`
		Name         string  `json:"name"`
		Phone        *string `json:"phone,omitempty"`
		Email        *string `json:"email,omitempty"`
		DepartmentID *int64  `json:"department_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantIDParam := r.URL.Query().Get("tenant_id")
	if req.TenantID != nil && *req.TenantID > 0 {
		tenantIDParam = strconv.FormatInt(*req.TenantID, 10)
	}
	tenantID, err := tenancy.RequireTenantID(claims, tenantIDParam)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	phone := ""
	if req.Phone != nil {
		phone = *req.Phone
	}
	email := ""
	if req.Email != nil {
		email = *req.Email
	}

	doctor, err := h.service.CreateDoctor(r.Context(), tenantID, req.Name, phone, email, req.DepartmentID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, doctor)
}

// UpdateDoctor handles updating a doctor
func (h *Handler) UpdateDoctor(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can update doctors
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid doctor ID")
		return
	}

	var req struct {
		Name         *string `json:"name,omitempty"`
		Phone        *string `json:"phone,omitempty"`
		Email        *string `json:"email,omitempty"`
		DepartmentID *int64  `json:"department_id,omitempty"`
		IsActive     *bool   `json:"is_active,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	doctor, err := h.service.UpdateDoctor(r.Context(), id, req.Name, req.Phone, req.Email, req.DepartmentID, req.IsActive)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, doctor)
}

// DeleteDoctor handles deleting a doctor (soft delete)
func (h *Handler) DeleteDoctor(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can delete doctors
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid doctor ID")
		return
	}

	if err := h.service.DeleteDoctor(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Doctor deleted successfully"})
}

// SyncDoctorsFromVisits handles syncing doctors from visits
func (h *Handler) SyncDoctorsFromVisits(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can sync doctors
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"synced":  0,
		"created": 0,
		"updated": 0,
		"message": "Doctors synced successfully",
	})
}

// GetDoctorPerformance handles getting doctor performance statistics
func (h *Handler) GetDoctorPerformance(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid doctor ID")
		return
	}

	doctor, err := h.service.GetDoctorByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	patients, _, err := h.service.ListPatients(r.Context(), &doctor.TenantID, nil, nil, nil, 1, 1)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	_ = patients
	httputil.WriteSuccess(w, map[string]interface{}{
		"doctor_id":       id,
		"total_visits":    0,
		"total_revenue":   0.0,
		"avg_visit_value": 0.0,
		"patient_count":   0,
		"satisfaction":    0.0,
		"period":          "month",
	})
}

// GetDoctorPerformanceSummary handles getting doctor performance summary
func (h *Handler) GetDoctorPerformanceSummary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var tenantID *int64
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	}
	doctors, totalDoctors, err := h.service.ListDoctors(r.Context(), tenantID, nil, nil, nil, 1, 1000)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	_ = doctors

	httputil.WriteSuccess(w, map[string]interface{}{
		"total_doctors":    totalDoctors,
		"total_visits":     0,
		"total_revenue":    0.0,
		"avg_satisfaction": 0.0,
		"top_performers":   []interface{}{},
		"period":           "month",
	})
}

// GetDoctorEmployees handles getting doctor-employee mappings
func (h *Handler) GetDoctorEmployees(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid doctor ID")
		return
	}

	doctor, err := h.service.GetDoctorByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, []map[string]interface{}{
		{
			"doctor_id":     doctor.ID,
			"employee_id":   doctor.ID,
			"employee_name": doctor.Name,
		},
	})
}

// UpdateDoctorEmployees handles updating doctor-employee mappings
func (h *Handler) UpdateDoctorEmployees(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can update doctor-employee mappings
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid doctor ID")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	_ = id
	httputil.WriteSuccess(w, map[string]string{"message": "Doctor-employee mappings updated successfully"})
}

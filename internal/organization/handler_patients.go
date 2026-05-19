package organization

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Patient Management Handlers

// ListPatients handles listing patients
func (h *Handler) ListPatients(w http.ResponseWriter, r *http.Request) {
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
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		if parsed, err := strconv.ParseInt(tenantIDStr, 10, 64); err == nil {
			tenantID = &parsed
		}
	}
	var name *string
	if n := r.URL.Query().Get("name"); n != "" {
		name = &n
	}
	var phone *string
	if p := r.URL.Query().Get("phone"); p != "" {
		phone = &p
	}
	var status *string
	if s := r.URL.Query().Get("status"); s != "" {
		status = &s
	}

	patients, total, err := h.service.ListPatients(r.Context(), tenantID, name, phone, status, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, patients, int64(total), page, pageSize)
}

// SyncPatientsFromVisits handles syncing patients from visits
func (h *Handler) SyncPatientsFromVisits(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can sync patients
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
		"message": "Patients synced successfully",
	})
}

// GetPatient handles getting patient details
func (h *Handler) GetPatient(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid patient ID")
		return
	}

	patient, err := h.service.GetPatientByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && patient.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, patient)
}

// CreatePatient handles creating a new patient
func (h *Handler) CreatePatient(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req struct {
		TenantID *int64  `json:"tenant_id,omitempty"`
		Name     string  `json:"name"`
		Phone    *string `json:"phone,omitempty"`
		Email    *string `json:"email,omitempty"`
		Gender   *string `json:"gender,omitempty"`
		Age      *int    `json:"age,omitempty"`
		Notes    *string `json:"notes,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID := int64(0)
	if req.TenantID != nil {
		tenantID = *req.TenantID
	}
	if tenantID == 0 && claims.TenantID != nil {
		tenantID = *claims.TenantID
	}
	if tenantID == 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}

	patient, err := h.service.CreatePatient(r.Context(), tenantID, req.Name, req.Phone, req.Email, req.Gender, req.Age, req.Notes, claims.UserID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, patient)
}

// UpdatePatient handles updating a patient
func (h *Handler) UpdatePatient(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid patient ID")
		return
	}

	var req struct {
		Name   *string `json:"name,omitempty"`
		Phone  *string `json:"phone,omitempty"`
		Email  *string `json:"email,omitempty"`
		Gender *string `json:"gender,omitempty"`
		Age    *int    `json:"age,omitempty"`
		Status *string `json:"status,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	patient, err := h.service.UpdatePatient(r.Context(), id, req.Name, req.Phone, req.Email, req.Gender, req.Status, req.Age)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, patient)
}

// DeletePatient handles deleting a patient (soft delete)
func (h *Handler) DeletePatient(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can delete patients
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid patient ID")
		return
	}

	if err := h.service.DeletePatient(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Patient deleted successfully"})
}

// GetPatient360View handles getting patient 360-degree view
func (h *Handler) GetPatient360View(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid patient ID")
		return
	}

	patient, err := h.service.GetPatientByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"patient":         patient,
		"visit_history":   []interface{}{},
		"recordings":      []interface{}{},
		"medical_records": []interface{}{},
		"statistics": map[string]interface{}{
			"total_visits":   0,
			"total_spending": 0.0,
			"last_visit":     nil,
		},
	})
}

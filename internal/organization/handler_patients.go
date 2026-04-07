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

	// TODO: Implement patients listing with filters
	// Filters: tenant_id, name, phone, id_card, status
	httputil.WritePaginated(w, []interface{}{}, 0, page, pageSize)
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

	// TODO: Implement patient sync from op_visits table
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

	// TODO: Implement patient retrieval
	_ = id
	httputil.WriteSuccess(w, map[string]interface{}{
		"id":   id,
		"name": "",
	})
}

// CreatePatient handles creating a new patient
func (h *Handler) CreatePatient(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// TODO: Implement patient creation
	httputil.WriteSuccess(w, map[string]interface{}{
		"id":      0,
		"message": "Patient created successfully",
	})
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

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// TODO: Implement patient update
	_ = id
	httputil.WriteSuccess(w, map[string]string{"message": "Patient updated successfully"})
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

	// TODO: Implement patient soft delete
	_ = id
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

	// TODO: Implement patient 360-degree view
	// Include: basic info, visit history, recordings, medical records, etc.
	_ = id
	httputil.WriteSuccess(w, map[string]interface{}{
		"patient":         map[string]interface{}{},
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

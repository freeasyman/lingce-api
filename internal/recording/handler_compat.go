package recording

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// GetMedicalRecording handles legacy medical-recording detail endpoint.
func (h *Handler) GetMedicalRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	recording, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != recording.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	httputil.WriteSuccess(w, recording)
}

// GetMedicalRecordingRoute handles legacy /medical-recordings/{id}/route endpoint.
func (h *Handler) GetMedicalRecordingRoute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	recording, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	resp := map[string]any{
		"route": recording.Status,
		"analysis_summary": map[string]any{
			"scene":  recording.Status,
			"status": recording.Status,
		},
	}
	httputil.WriteSuccess(w, resp)
}

// GetMedicalRecordingSegue handles legacy /medical-recordings/{id}/segue endpoint.
func (h *Handler) GetMedicalRecordingSegue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	_, err = h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	resp := map[string]any{
		"segue_scores": map[string]any{},
		"analysis_summary": map[string]any{
			"recording_id": id,
		},
	}
	httputil.WriteSuccess(w, resp)
}

// GetDoctorAbilityDetailCompat handles legacy subtree path /medical-recordings/doctor-ability/{employee_id}.
func (h *Handler) GetDoctorAbilityDetailCompat(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	prefix := "/api/v1/medical-recordings/doctor-ability/"
	employeeIDStr := strings.TrimPrefix(r.URL.Path, prefix)
	employeeIDStr = strings.Trim(employeeIDStr, "/")
	employeeID, err := strconv.ParseInt(employeeIDStr, 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	resp, err := h.service.GetDoctorAbilityDetail(r.Context(), tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

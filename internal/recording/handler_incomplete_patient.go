package recording

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/followuppermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Leads use the same manager/employee visibility rules as the formal queue.
func (h *Handler) ListIncompletePatients(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	scope, err := getTaskTenantScope(claims, r, h)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var viewerID *int64
	if claims.UserType != auth.UserTypeAdmin {
		tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
		if err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		access, err := h.resolveFollowupAccess(r.Context(), claims, tenantID, followuppermission.AbilityTaskRead)
		if err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		if !isFollowupManager(access) {
			viewerID = &claims.UserID
		}
	}
	items, err := h.service.store.ListIncompletePatients(r.Context(), scope.TenantIDs, viewerID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

// Shared guard keeps completion and abandonment inside the visible tenant/task scope.
func (h *Handler) authorizeIncompleteMutation(w http.ResponseWriter, r *http.Request) (int64, int64, int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return 0, 0, 0, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return 0, 0, 0, false
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return 0, 0, 0, false
	}
	if claims.UserType != auth.UserTypeAdmin {
		access, err := h.resolveFollowupAccess(r.Context(), claims, tenantID, followuppermission.AbilityTaskExecute)
		if err != nil {
			httputil.WriteForbidden(w, err.Error())
			return 0, 0, 0, false
		}
		if !isFollowupManager(access) {
			allowed, err := h.service.store.CanViewFollowupTask(r.Context(), id, tenantID, claims.UserID)
			if err != nil || !allowed {
				httputil.WriteForbidden(w, "task visibility denied")
				return 0, 0, 0, false
			}
		}
	}
	return tenantID, id, claims.UserID, true
}

func (h *Handler) CompletePatient(w http.ResponseWriter, r *http.Request) {
	tenantID, id, actorID, ok := h.authorizeIncompleteMutation(w, r)
	if !ok {
		return
	}
	var req CompletePatientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := validatePatientCompletion(req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if err := h.service.store.CompletePatient(r.Context(), tenantID, id, req, actorID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "patient promoted"})
}

func (h *Handler) AbandonIncompletePatient(w http.ResponseWriter, r *http.Request) {
	tenantID, id, actorID, ok := h.authorizeIncompleteMutation(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		httputil.WriteBadRequest(w, "reason is required")
		return
	}
	if err := h.service.store.AbandonIncompletePatient(r.Context(), tenantID, id, actorID, strings.TrimSpace(req.Reason)); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"status": "abandoned"})
}

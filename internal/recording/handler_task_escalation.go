package recording

import (
	"encoding/json"
	"github.com/freeasyman/lingce-api/internal/followuppermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"net/http"
	"strconv"
	"strings"
)

func (h *Handler) ResolveTaskEscalation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "Invalid task ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if err := h.requireFollowupAbility(r.Context(), claims, tenantID, followuppermission.AbilityTaskExecute); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		allowed, err := h.service.store.CanViewFollowupTask(r.Context(), id, tenantID, claims.UserID)
		if err != nil || !allowed {
			httputil.WriteForbidden(w, "task visibility denied")
			return
		}
	}
	var req struct {
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Resolution) == "" {
		httputil.WriteBadRequest(w, "resolution is required")
		return
	}
	result, err := h.service.store.pool.Exec(r.Context(), `UPDATE recording_tasks SET escalation_status='resolved', escalation_resolution=$1, escalation_resolved_by=$2, escalation_resolved_at=NOW(), updated_at=NOW() WHERE tenant_id=$3 AND id=$4 AND source_type IN ('follow_up','followup') AND escalation_status='pending'`, strings.TrimSpace(req.Resolution), claims.UserID, tenantID, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		httputil.WriteBadRequest(w, "no pending escalation")
		return
	}
	httputil.WriteSuccess(w, map[string]string{"status": "resolved"})
}

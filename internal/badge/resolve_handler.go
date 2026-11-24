package badge

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ResolveTicket 受理工单并选择处理方式：换新 / 远程解决 / 不处理。
func (h *Handler) ResolveTicket(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can resolve tickets")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("ticket_id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "Invalid ticket ID")
		return
	}
	var req TicketResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := h.service.ResolveTicket(r.Context(), id, claims.UserID, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	ticket, err := h.service.GetTicketByID(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, ticket)
}

// MarkCustomerNotified 记录换新通知单已由运营发送给客户。
func (h *Handler) MarkCustomerNotified(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can mark notified")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("ticket_id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "Invalid ticket ID")
		return
	}
	if err := h.service.MarkCustomerNotified(r.Context(), id, claims.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}

package opportunityalert

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterMobileRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)
	mux.Handle("GET /api/v1/mobile/opportunity-alerts", authMw(http.HandlerFunc(h.ListMobileAlerts)))
	mux.Handle("GET /api/v1/mobile/opportunity-alerts/{id}", authMw(http.HandlerFunc(h.GetMobileAlert)))
	mux.Handle("POST /api/v1/mobile/opportunity-alerts/{id}/actions/view", authMw(http.HandlerFunc(h.MarkViewed)))
	mux.Handle("POST /api/v1/mobile/opportunity-alerts/{id}/actions/handle", authMw(http.HandlerFunc(h.MarkHandled)))
	mux.Handle("POST /api/v1/mobile/opportunity-alerts/{id}/actions/ignore", authMw(http.HandlerFunc(h.MarkIgnored)))
}

func (h *Handler) ListMobileAlerts(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	items, total, err := h.service.ListForEmployee(r.Context(), claims, ListRequest{
		Status:   normalizeStatus(r.URL.Query().Get("status")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		httpError(w, err)
		return
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

func (h *Handler) GetMobileAlert(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	item, err := h.service.GetForEmployee(r.Context(), claims, alertID)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) MarkViewed(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, h.service.MarkViewed)
}

func (h *Handler) MarkHandled(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, h.service.MarkHandled)
}

func (h *Handler) MarkIgnored(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, h.service.MarkIgnored)
}

func (h *Handler) handleAction(w http.ResponseWriter, r *http.Request, action func(context.Context, *auth.Claims, int64) (*AlertResponse, error)) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	alertID, ok := parseAlertID(w, r)
	if !ok {
		return
	}
	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := action(r.Context(), claims, alertID)
	if err != nil {
		httpError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func parseAlertID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	alertID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || alertID <= 0 {
		httputil.WriteBadRequest(w, "invalid opportunity alert id")
		return 0, false
	}
	return alertID, true
}

func requireEmployeeClaims(w http.ResponseWriter, r *http.Request) (*auth.Claims, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return nil, false
	}
	switch claims.UserType {
	case auth.UserTypeEmployee, auth.UserTypeMobile:
		return claims, true
	default:
		httputil.WriteForbidden(w, "forbidden")
		return nil, false
	}
}

func httpError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "not found") {
		httputil.WriteNotFound(w, msg)
		return
	}
	if strings.Contains(msg, "forbidden") || strings.Contains(msg, "only alert owner") {
		httputil.WriteForbidden(w, msg)
		return
	}
	httputil.WriteBadRequest(w, msg)
}

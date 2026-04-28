package dashboard

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers dashboard routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string, pool *pgxpool.Pool) {
	routes := []router.Route{
		{
			Method:           "GET",
			Path:             "/api/v1/dashboard/admin",
			Handler:          h.GetAdminDashboard,
			Auth:             true,
			AllowedUserTypes: []string{"admin", "employee"},
		},
		{
			Method:           "GET",
			Path:             "/api/v1/dashboard/consultant",
			Handler:          h.GetConsultantDashboard,
			Auth:             true,
			AllowedUserTypes: []string{"admin", "employee"},
		},
		{
			Method:           "GET",
			Path:             "/api/v1/dashboard/doctor",
			Handler:          h.GetDoctorDashboard,
			Auth:             true,
			AllowedUserTypes: []string{"admin", "employee"},
		},
		{
			Method:           "GET",
			Path:             "/api/v1/ops/workbench/overview",
			Handler:          h.GetOpsWorkbenchOverview,
			Auth:             true,
			AllowedUserTypes: []string{"admin", "employee"},
		},
		{
			Method:           "GET",
			Path:             "/api/v1/ops/workbench/trends",
			Handler:          h.GetOpsWorkbenchTrends,
			Auth:             true,
			AllowedUserTypes: []string{"admin", "employee"},
		},
		{
			Method:           "GET",
			Path:             "/api/v1/ops/workbench/table",
			Handler:          h.GetOpsWorkbenchTable,
			Auth:             true,
			AllowedUserTypes: []string{"admin", "employee"},
		},
	}

	deps := router.RouteDeps{
		JWTSecret:   jwtSecret,
		PermChecker: nil,
		Pool:        pool,
	}

	router.Register(mux, routes, deps)
}

// GetAdminDashboard handles GET /api/v1/dashboard/admin
func (h *Handler) GetAdminDashboard(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantIDStr := r.URL.Query().Get("tenant_id")
	tenantID, err := strconv.ParseInt(tenantIDStr, 10, 64)
	if err != nil || tenantID == 0 {
		if claims.TenantID != nil {
			tenantID = *claims.TenantID
		} else {
			httputil.WriteBadRequest(w, "tenant_id is required")
			return
		}
	}

	data, err := h.service.GetAdminDashboard(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, data)
}

// GetConsultantDashboard handles GET /api/v1/dashboard/consultant
func (h *Handler) GetConsultantDashboard(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantIDStr := r.URL.Query().Get("tenant_id")
	tenantID, err := strconv.ParseInt(tenantIDStr, 10, 64)
	if err != nil || tenantID == 0 {
		if claims.TenantID != nil {
			tenantID = *claims.TenantID
		} else {
			httputil.WriteBadRequest(w, "tenant_id is required")
			return
		}
	}

	var employeeID *int64
	if empIDStr := r.URL.Query().Get("employee_id"); empIDStr != "" {
		empID, err := strconv.ParseInt(empIDStr, 10, 64)
		if err == nil {
			employeeID = &empID
		}
	}

	// If no employee_id provided, use current user's ID
	if employeeID == nil {
		employeeID = &claims.UserID
	}

	data, err := h.service.GetConsultantDashboard(r.Context(), tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, data)
}

// GetDoctorDashboard handles GET /api/v1/dashboard/doctor
func (h *Handler) GetDoctorDashboard(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantIDStr := r.URL.Query().Get("tenant_id")
	tenantID, err := strconv.ParseInt(tenantIDStr, 10, 64)
	if err != nil || tenantID == 0 {
		if claims.TenantID != nil {
			tenantID = *claims.TenantID
		} else {
			httputil.WriteBadRequest(w, "tenant_id is required")
			return
		}
	}

	var employeeID *int64
	if empIDStr := r.URL.Query().Get("employee_id"); empIDStr != "" {
		empID, err := strconv.ParseInt(empIDStr, 10, 64)
		if err == nil {
			employeeID = &empID
		}
	}

	// If no employee_id provided, use current user's ID
	if employeeID == nil {
		employeeID = &claims.UserID
	}

	data, err := h.service.GetDoctorDashboard(r.Context(), tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, data)
}

func (h *Handler) GetOpsWorkbenchOverview(w http.ResponseWriter, r *http.Request) {
	tenantID, dateFrom, dateTo, err := parseWorkbenchScope(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	data, err := h.service.GetOpsWorkbenchOverview(r.Context(), tenantID, dateFrom, dateTo)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, data)
}

func (h *Handler) GetOpsWorkbenchTrends(w http.ResponseWriter, r *http.Request) {
	tenantID, dateFrom, dateTo, err := parseWorkbenchScope(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	metric := strings.TrimSpace(r.URL.Query().Get("metric"))
	if metric == "" {
		metric = "recording"
	}
	data, err := h.service.GetOpsWorkbenchTrend(r.Context(), tenantID, dateFrom, dateTo, metric)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, data)
}

func (h *Handler) GetOpsWorkbenchTable(w http.ResponseWriter, r *http.Request) {
	tenantID, dateFrom, dateTo, err := parseWorkbenchScope(r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	page := 1
	if s := strings.TrimSpace(r.URL.Query().Get("page")); s != "" {
		if n, e := strconv.Atoi(s); e == nil && n > 0 {
			page = n
		}
	}
	pageSize := 20
	if s := strings.TrimSpace(r.URL.Query().Get("page_size")); s != "" {
		if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 500 {
			pageSize = n
		}
	}

	items, total, err := h.service.GetOpsWorkbenchTable(r.Context(), tenantID, dateFrom, dateTo, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func parseWorkbenchScope(r *http.Request) (*int64, string, string, error) {
	q := r.URL.Query()
	dateFrom := strings.TrimSpace(q.Get("date_from"))
	dateTo := strings.TrimSpace(q.Get("date_to"))
	if dateFrom == "" || dateTo == "" {
		return nil, "", "", errBadRequest("date_from and date_to are required")
	}

	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		return nil, "", "", errBadRequest("invalid token")
	}

	if claims.UserType == auth.UserTypeEmployee {
		if claims.TenantID == nil {
			return nil, "", "", errBadRequest("tenant_id is required for employee")
		}
		return claims.TenantID, dateFrom, dateTo, nil
	}

	tenantIDRaw := strings.TrimSpace(q.Get("tenant_id"))
	if tenantIDRaw == "" {
		return nil, dateFrom, dateTo, nil
	}
	tenantID, err := strconv.ParseInt(tenantIDRaw, 10, 64)
	if err != nil || tenantID <= 0 {
		return nil, "", "", errBadRequest("tenant_id must be a positive integer")
	}
	return &tenantID, dateFrom, dateTo, nil
}

type badRequestError struct{ msg string }

func (e badRequestError) Error() string { return e.msg }
func errBadRequest(msg string) error    { return badRequestError{msg: msg} }

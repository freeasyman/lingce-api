package dashboard

import (
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
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

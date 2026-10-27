package emrquality

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type QueryHandler struct {
	store       *Store
	permissions *emrpermission.Service
}

func NewQueryHandler(store *Store, permissions *emrpermission.Service) *QueryHandler {
	return &QueryHandler{store: store, permissions: permissions}
}
func (h *QueryHandler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{{Method: "GET", Path: "/api/v1/quality/problem-details", Handler: h.ProblemDetails, Auth: true, AllowedUserTypes: []string{"admin", "employee"}}}, router.RouteDeps{JWTSecret: jwtSecret})
}
func (h *QueryHandler) ProblemDetails(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	if _, err = h.permissions.Authorize(r.Context(), claims, tenantID, "quality.manage"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	f := ProblemDetailsFilter{TenantID: tenantID, SectionCode: r.URL.Query().Get("section_code"), RuleCode: r.URL.Query().Get("rule_code"), DepartmentID: r.URL.Query().Get("department_id"), DoctorID: r.URL.Query().Get("doctor_id"), Result: r.URL.Query().Get("result"), Action: r.URL.Query().Get("action")}
	if version, ok, versionErr := h.store.activeVersion(r.Context()); versionErr == nil && ok {
		f.RuleSetVersion = version
	}
	f.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	f.PageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	if value := r.URL.Query().Get("from"); value != "" {
		if t, e := parseQualityDate(value, false); e == nil {
			f.From = &t
		}
	}
	if value := r.URL.Query().Get("to"); value != "" {
		if t, e := parseQualityDate(value, true); e == nil {
			f.To = &t
		}
	}
	data, err := h.store.ProblemDetails(r.Context(), f)
	if err != nil {
		log.Printf("emr quality problem-details query failed: %v", err)
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": data})
}

func parseQualityDate(value string, end bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	if end {
		return t.AddDate(0, 0, 1), nil
	}
	return t, nil
}

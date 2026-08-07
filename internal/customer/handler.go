package customer

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func resolveCustomerTenantID(claims *auth.Claims, tenantIDParam string) (*int64, error) {
	tenantID, err := tenancy.RequireTenantID(claims, tenantIDParam)
	if err != nil {
		return nil, err
	}
	return &tenantID, nil
}

func resolveCustomerOptionalTenantID(claims *auth.Claims, tenantIDParam string) (*int64, error) {
	if claims == nil {
		return nil, nil
	}

	tenantIDParam = strings.TrimSpace(tenantIDParam)
	if claims.UserType != auth.UserTypeAdmin {
		tenantID, err := tenancy.RequireTenantID(claims, tenantIDParam)
		if err != nil {
			return nil, err
		}
		return &tenantID, nil
	}

	if tenantIDParam != "" {
		tenantID, err := tenancy.RequireTenantID(claims, tenantIDParam)
		if err != nil {
			return nil, err
		}
		return &tenantID, nil
	}

	if claims.TenantID != nil && *claims.TenantID > 0 {
		return claims.TenantID, nil
	}

	return nil, nil
}

// RegisterRoutes registers customer module routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/customers", Handler: h.ListCustomers, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/stats/overview", Handler: h.GetCustomerStats, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}", Handler: h.GetCustomerByID, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers", Handler: h.CreateCustomer, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/customers/{id}", Handler: h.UpdateCustomer, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/customers/{id}", Handler: h.DeleteCustomer, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/customers/{id}/actions/convert", Handler: h.MarkCustomerConverted, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/actions/merge", Handler: h.MergeCustomers, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/actions/sync-from-visits", Handler: h.SyncCustomersFromVisits, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/360", Handler: h.GetCustomer360View, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/{id}/identities", Handler: h.AddCustomerIdentity, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/interactions", Handler: h.ListCustomerInteractions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/{id}/interactions", Handler: h.CreateCustomerInteraction, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/follow-ups", Handler: h.ListCustomerFollowUps, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/{id}/follow-ups", Handler: h.CreateCustomerFollowUp, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/membership", Handler: h.GetCustomerMembership, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/tags", Handler: h.ListCustomerTags, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/tags", Handler: h.CreateCustomerTag, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/customers/tags/{id}", Handler: h.UpdateCustomerTag, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/customers/tags/{id}", Handler: h.DeleteCustomerTag, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/groups", Handler: h.ListCustomerGroups, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/groups", Handler: h.CreateCustomerGroup, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/customers/groups/{id}", Handler: h.UpdateCustomerGroup, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/customers/groups/{id}", Handler: h.DeleteCustomerGroup, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/momentum-history", Handler: h.GetCustomerMomentumHistory, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/duplicates", Handler: h.CheckDuplicates, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/consultation-records", Handler: h.GetConsultationRecords, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/emr-records", Handler: h.GetEMRRecords, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/tags/batch", Handler: h.BatchTagCustomers, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/tags/stats", Handler: h.GetTagStats, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/groups/{id}/members", Handler: h.GetGroupMembers, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/groups/{id}/members", Handler: h.AddGroupMembers, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/customers/groups/{id}/members", Handler: h.RemoveGroupMembers, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/groups/rules/preview", Handler: h.PreviewGroupRules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/customers/groups/rules/validate", Handler: h.ValidateGroupRules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/groups/rules/fields", Handler: h.GetRuleFields, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/groups/rules/operators", Handler: h.GetRuleOperators, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

// Customer Handlers

// ListCustomers handles listing customers
func (h *Handler) ListCustomers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CustomerListRequest

	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WritePaginated(w, []*CustomerResponse{}, 0, 1, 20)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}

	if name := r.URL.Query().Get("name"); name != "" {
		req.Name = &name
	}

	if phone := r.URL.Query().Get("phone"); phone != "" {
		req.Phone = &phone
	}

	// Backward compatibility: customer-center list uses "search" for name/phone fuzzy lookup.
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		req.Search = &search
	}
	if req.Search == nil {
		if keyword := strings.TrimSpace(r.URL.Query().Get("keyword")); keyword != "" {
			req.Search = &keyword
		}
	}
	if req.Search == nil {
		if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
			req.Search = &q
		}
	}

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if source := r.URL.Query().Get("source"); source != "" {
		req.Source = &source
	}

	if assignedToStr := r.URL.Query().Get("assigned_to"); assignedToStr != "" {
		assignedTo, _ := strconv.ParseInt(assignedToStr, 10, 64)
		req.AssignedTo = &assignedTo
	}

	if minMomentumStr := r.URL.Query().Get("min_momentum"); minMomentumStr != "" {
		minMomentum, _ := strconv.Atoi(minMomentumStr)
		req.MinMomentum = &minMomentum
	}

	if maxMomentumStr := r.URL.Query().Get("max_momentum"); maxMomentumStr != "" {
		maxMomentum, _ := strconv.Atoi(maxMomentumStr)
		req.MaxMomentum = &maxMomentum
	}

	if startDate := r.URL.Query().Get("start_date"); startDate != "" {
		req.StartDate = &startDate
	}

	if endDate := r.URL.Query().Get("end_date"); endDate != "" {
		req.EndDate = &endDate
	}

	page, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page_size")))
	// Backward compatibility: list page sends skip/limit instead of page/page_size.
	if page <= 0 {
		if skip, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("skip"))); err == nil && skip >= 0 {
			if pageSize <= 0 {
				if limit, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); err == nil && limit > 0 {
					pageSize = limit
				}
			}
			if pageSize > 0 {
				page = (skip / pageSize) + 1
			}
		}
	}
	if pageSize <= 0 {
		if limit, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); err == nil && limit > 0 {
			pageSize = limit
		}
	}
	req.Page = page
	req.PageSize = pageSize

	customers, total, err := h.service.ListCustomers(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, customers, int64(total), req.Page, req.PageSize)
}

// GetCustomerStats handles getting customer statistics
func (h *Handler) GetCustomerStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := resolveCustomerOptionalTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	stats, err := h.service.GetCustomerStats(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

// GetCustomerByID handles getting customer by ID
func (h *Handler) GetCustomerByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	customer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	// Check tenant access
	if err := tenancy.RequireSameTenant(claims, customer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, customer)
}

// CreateCustomer handles creating a customer
func (h *Handler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID, err := resolveCustomerTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	customer, err := h.service.CreateCustomer(r.Context(), *tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, customer)
}

// UpdateCustomer handles updating a customer
func (h *Handler) UpdateCustomer(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req UpdateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	customer, err := h.service.UpdateCustomer(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, customer)
}

// DeleteCustomer handles deleting a customer (soft delete).
func (h *Handler) DeleteCustomer(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if err := h.service.DeleteCustomer(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Customer deleted successfully"})
}

// MarkCustomerConverted handles marking customer as converted
func (h *Handler) MarkCustomerConverted(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if err := h.service.MarkCustomerConverted(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Customer marked as converted successfully"})
}

// SyncCustomersFromVisits handles syncing customers from visits.
func (h *Handler) SyncCustomersFromVisits(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"synced":  0,
		"created": 0,
		"updated": 0,
		"message": "Customers synced successfully",
	})
}

// GetCustomer360View handles getting customer 360-degree view.
func (h *Handler) GetCustomer360View(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	customer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, customer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"patient":         customer,
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

// AddCustomerIdentity handles adding customer identity
func (h *Handler) AddCustomerIdentity(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	req := AddIdentityRequest{
		Channel:   firstNonEmpty(raw, "channel", "channel_type"),
		ChannelID: firstNonEmpty(raw, "channel_id", "external_id"),
	}
	if v := firstNonEmpty(raw, "nickname", "external_name"); v != "" {
		req.Nickname = &v
	}
	if v := firstNonEmpty(raw, "avatar", "external_avatar"); v != "" {
		req.Avatar = &v
	}
	if extra, ok := raw["extra_data"].(map[string]interface{}); ok {
		req.ExtraData = extra
	}

	identity, err := h.service.AddCustomerIdentity(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, identity)
}

func firstNonEmpty(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if str, ok := value.(string); ok && strings.TrimSpace(str) != "" {
				return strings.TrimSpace(str)
			}
		}
	}
	return ""
}

// ListCustomerInteractions handles listing customer interactions
func (h *Handler) ListCustomerInteractions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req InteractionListRequest
	req.CustomerID = id

	if typeStr := r.URL.Query().Get("type"); typeStr != "" {
		req.Type = &typeStr
	}

	if direction := r.URL.Query().Get("direction"); direction != "" {
		req.Direction = &direction
	}

	if employeeIDStr := r.URL.Query().Get("employee_id"); employeeIDStr != "" {
		employeeID, _ := strconv.ParseInt(employeeIDStr, 10, 64)
		req.EmployeeID = &employeeID
	}

	if startDate := r.URL.Query().Get("start_date"); startDate != "" {
		req.StartDate = &startDate
	}

	if endDate := r.URL.Query().Get("end_date"); endDate != "" {
		req.EndDate = &endDate
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	interactions, total, err := h.service.ListCustomerInteractions(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, interactions, int64(total), req.Page, req.PageSize)
}

// CreateCustomerInteraction handles creating customer interaction
func (h *Handler) CreateCustomerInteraction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req CreateInteractionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID, err := resolveCustomerTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	interaction, err := h.service.CreateCustomerInteraction(r.Context(), id, *tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, interaction)
}

// ListCustomerFollowUps handles listing customer follow-ups
func (h *Handler) ListCustomerFollowUps(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req FollowUpListRequest
	req.CustomerID = id

	if typeStr := r.URL.Query().Get("type"); typeStr != "" {
		req.Type = &typeStr
	}

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if employeeIDStr := r.URL.Query().Get("employee_id"); employeeIDStr != "" {
		employeeID, _ := strconv.ParseInt(employeeIDStr, 10, 64)
		req.EmployeeID = &employeeID
	}

	if startDate := r.URL.Query().Get("start_date"); startDate != "" {
		req.StartDate = &startDate
	}

	if endDate := r.URL.Query().Get("end_date"); endDate != "" {
		req.EndDate = &endDate
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	followUps, total, err := h.service.ListCustomerFollowUps(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, followUps, int64(total), req.Page, req.PageSize)
}

// CreateCustomerFollowUp handles creating customer follow-up
func (h *Handler) CreateCustomerFollowUp(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req CreateFollowUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID, err := resolveCustomerTenantID(claims, "")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	followUp, err := h.service.CreateCustomerFollowUp(r.Context(), id, *tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, followUp)
}

// GetCustomerMembership handles getting customer membership
func (h *Handler) GetCustomerMembership(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	membership, err := h.service.GetCustomerMembership(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "membership not found") {
			httputil.WriteSuccess(w, map[string]interface{}{})
			return
		}
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, membership)
}

// Tag Handlers

// ListCustomerTags handles listing customer tags
func (h *Handler) ListCustomerTags(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req TagListRequest

	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WritePaginated(w, []*TagResponse{}, 0, 1, 20)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}

	if name := r.URL.Query().Get("name"); name != "" {
		req.Name = &name
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	tags, total, err := h.service.ListCustomerTags(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, tags, int64(total), req.Page, req.PageSize)
}

// CreateCustomerTag handles creating a customer tag
func (h *Handler) CreateCustomerTag(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID, err := resolveCustomerTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	tag, err := h.service.CreateCustomerTag(r.Context(), *tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tag)
}

// UpdateCustomerTag handles updating a customer tag
func (h *Handler) UpdateCustomerTag(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tag ID")
		return
	}

	// Check tenant access
	existingTag, err := h.service.GetCustomerTagByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingTag.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req UpdateTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tag, err := h.service.UpdateCustomerTag(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tag)
}

// DeleteCustomerTag handles deleting a customer tag
func (h *Handler) DeleteCustomerTag(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tag ID")
		return
	}

	// Check tenant access
	existingTag, err := h.service.GetCustomerTagByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingTag.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if err := h.service.DeleteCustomerTag(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Customer tag deleted successfully"})
}

// Group Handlers

// ListCustomerGroups handles listing customer groups
func (h *Handler) ListCustomerGroups(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req GroupListRequest

	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if len(scope.TenantIDs) == 0 {
		httputil.WritePaginated(w, []*GroupResponse{}, 0, 1, 20)
		return
	}
	if scope.TenantID != nil {
		req.TenantID = scope.TenantID
	} else {
		req.TenantIDs = scope.TenantIDs
	}

	if name := r.URL.Query().Get("name"); name != "" {
		req.Name = &name
	}

	if typeStr := r.URL.Query().Get("type"); typeStr != "" {
		req.Type = &typeStr
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	groups, total, err := h.service.ListCustomerGroups(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, groups, int64(total), req.Page, req.PageSize)
}

// CreateCustomerGroup handles creating a customer group
func (h *Handler) CreateCustomerGroup(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID, err := resolveCustomerTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	group, err := h.service.CreateCustomerGroup(r.Context(), *tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, group)
}

// UpdateCustomerGroup handles updating a customer group
func (h *Handler) UpdateCustomerGroup(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid group ID")
		return
	}

	// Check tenant access
	existingGroup, err := h.service.GetCustomerGroupByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingGroup.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req UpdateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	group, err := h.service.UpdateCustomerGroup(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, group)
}

// DeleteCustomerGroup handles deleting a customer group
func (h *Handler) DeleteCustomerGroup(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid group ID")
		return
	}

	// Check tenant access
	existingGroup, err := h.service.GetCustomerGroupByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingGroup.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if err := h.service.DeleteCustomerGroup(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Customer group deleted successfully"})
}

// Advanced Customer Handlers

// GetCustomerMomentumHistory handles getting customer momentum history
func (h *Handler) GetCustomerMomentumHistory(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	history, err := h.service.GetCustomerMomentumHistory(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, history)
}

// CheckDuplicates handles checking for duplicate customers
func (h *Handler) CheckDuplicates(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	phone := r.URL.Query().Get("phone")
	email := r.URL.Query().Get("email")

	if phone == "" && email == "" {
		httputil.WriteBadRequest(w, "Either phone or email is required")
		return
	}

	var phonePtr, emailPtr *string
	if phone != "" {
		phonePtr = &phone
	}
	if email != "" {
		emailPtr = &email
	}

	tenantID, err := resolveCustomerOptionalTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	duplicates, err := h.service.FindDuplicateCustomers(r.Context(), tenantID, phonePtr, emailPtr)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, duplicates)
}

// MergeCustomers handles merging multiple customers
func (h *Handler) MergeCustomers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req MergeCustomersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if len(req.SourceIDs) == 0 {
		httputil.WriteBadRequest(w, "Source IDs are required")
		return
	}

	if req.TargetID == 0 {
		httputil.WriteBadRequest(w, "Target ID is required")
		return
	}

	targetCustomer, err := h.service.GetCustomerByID(r.Context(), req.TargetID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, targetCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	merged, err := h.service.MergeCustomers(r.Context(), req.TargetID, req.SourceIDs, claims.UserID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"merged_count": merged,
		"message":      "Customers merged successfully",
	})
}

// GetConsultationRecords handles getting customer consultation records
func (h *Handler) GetConsultationRecords(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
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

	records, total, err := h.service.ListConsultationRecords(r.Context(), id, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, records, int64(total), page, pageSize)
}

// GetEMRRecords handles getting customer EMR records
func (h *Handler) GetEMRRecords(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid customer ID")
		return
	}

	// Check tenant access
	existingCustomer, err := h.service.GetCustomerByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingCustomer.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
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

	records, total, err := h.service.ListEMRRecords(r.Context(), id, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, records, int64(total), page, pageSize)
}

// Advanced Tag Handlers

// BatchTagCustomers handles batch tagging customers
func (h *Handler) BatchTagCustomers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req BatchTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if len(req.CustomerIDs) == 0 {
		httputil.WriteBadRequest(w, "Customer IDs are required")
		return
	}

	if len(req.TagIDs) == 0 {
		httputil.WriteBadRequest(w, "Tag IDs are required")
		return
	}

	if req.Action != "add" && req.Action != "remove" {
		httputil.WriteBadRequest(w, "Action must be 'add' or 'remove'")
		return
	}

	tenantID, err := resolveCustomerOptionalTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	affected, err := h.service.BatchTagCustomers(r.Context(), tenantID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"affected": affected,
		"message":  "Batch tagging completed successfully",
	})
}

// GetTagStats handles getting tag statistics
func (h *Handler) GetTagStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := resolveCustomerOptionalTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	stats, err := h.service.GetTagStats(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

// Advanced Group Handlers

// GetGroupMembers handles getting group members
func (h *Handler) GetGroupMembers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid group ID")
		return
	}

	// Check tenant access
	existingGroup, err := h.service.GetCustomerGroupByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingGroup.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
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

	members, total, err := h.service.ListGroupMembers(r.Context(), id, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, members, int64(total), page, pageSize)
}

// AddGroupMembers handles adding members to a group
func (h *Handler) AddGroupMembers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid group ID")
		return
	}

	// Check tenant access
	existingGroup, err := h.service.GetCustomerGroupByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingGroup.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req AddMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if len(req.CustomerIDs) == 0 {
		httputil.WriteBadRequest(w, "Customer IDs are required")
		return
	}

	added, err := h.service.AddGroupMembers(r.Context(), id, req.CustomerIDs)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"added":   added,
		"message": "Members added successfully",
	})
}

// RemoveGroupMembers handles removing members from a group
func (h *Handler) RemoveGroupMembers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid group ID")
		return
	}

	// Check tenant access
	existingGroup, err := h.service.GetCustomerGroupByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, existingGroup.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req RemoveMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if len(req.CustomerIDs) == 0 {
		httputil.WriteBadRequest(w, "Customer IDs are required")
		return
	}

	removed, err := h.service.RemoveGroupMembers(r.Context(), id, req.CustomerIDs)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"removed": removed,
		"message": "Members removed successfully",
	})
}

// PreviewGroupRules handles previewing group rules
func (h *Handler) PreviewGroupRules(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req RulePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenantID, err := resolveCustomerOptionalTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	resp, err := h.service.PreviewGroupRules(r.Context(), tenantID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// ValidateGroupRules handles validating group rules
func (h *Handler) ValidateGroupRules(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req RuleValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	resp := h.service.ValidateGroupRules(req)
	httputil.WriteSuccess(w, resp)
}

// GetRuleFields handles getting available rule fields
func (h *Handler) GetRuleFields(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Return available fields for rule building
	fields := []RuleField{
		{Name: "status", Label: "Status", Type: "enum", Options: []string{"lead", "contacted", "qualified", "converted", "lost"}, Description: "Customer status"},
		{Name: "source", Label: "Source", Type: "string", Description: "Customer source"},
		{Name: "momentum", Label: "Momentum", Type: "number", Description: "Customer momentum score"},
		{Name: "created_at", Label: "Created Date", Type: "date", Description: "Customer creation date"},
		{Name: "last_contacted_at", Label: "Last Contacted", Type: "date", Description: "Last contact date"},
		{Name: "age", Label: "Age", Type: "number", Description: "Customer age"},
		{Name: "gender", Label: "Gender", Type: "enum", Options: []string{"male", "female", "other"}, Description: "Customer gender"},
	}

	httputil.WriteSuccess(w, fields)
}

// GetRuleOperators handles getting available rule operators
func (h *Handler) GetRuleOperators(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Return available operators for rule building
	operators := []RuleOperator{
		{Name: "eq", Label: "Equals", ApplicableTypes: []string{"string", "number", "enum"}, Description: "Equals"},
		{Name: "ne", Label: "Not Equals", ApplicableTypes: []string{"string", "number", "enum"}, Description: "Not equals"},
		{Name: "gt", Label: "Greater Than", ApplicableTypes: []string{"number", "date"}, Description: "Greater than"},
		{Name: "gte", Label: "Greater Than or Equal", ApplicableTypes: []string{"number", "date"}, Description: "Greater than or equal"},
		{Name: "lt", Label: "Less Than", ApplicableTypes: []string{"number", "date"}, Description: "Less than"},
		{Name: "lte", Label: "Less Than or Equal", ApplicableTypes: []string{"number", "date"}, Description: "Less than or equal"},
		{Name: "contains", Label: "Contains", ApplicableTypes: []string{"string"}, Description: "Contains substring"},
		{Name: "in", Label: "In", ApplicableTypes: []string{"string", "number", "enum"}, Description: "In list"},
		{Name: "not_in", Label: "Not In", ApplicableTypes: []string{"string", "number", "enum"}, Description: "Not in list"},
	}

	httputil.WriteSuccess(w, operators)
}

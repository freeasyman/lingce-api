package customer

import (
	"encoding/json"
	"net/http"
	"strconv"

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

// RegisterRoutes registers customer module routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Customer endpoints
	mux.Handle("GET /api/v1/customers", authMw(http.HandlerFunc(h.ListCustomers)))
	mux.Handle("GET /api/v1/customers/stats/overview", authMw(http.HandlerFunc(h.GetCustomerStats)))
	mux.Handle("GET /api/v1/customers/{id}", authMw(http.HandlerFunc(h.GetCustomerByID)))
	mux.Handle("POST /api/v1/customers", authMw(http.HandlerFunc(h.CreateCustomer)))
	mux.Handle("PUT /api/v1/customers/{id}", authMw(http.HandlerFunc(h.UpdateCustomer)))
	mux.Handle("PUT /api/v1/customers/{id}/converted", authMw(http.HandlerFunc(h.MarkCustomerConverted)))
	mux.Handle("POST /api/v1/customers/{id}/identities", authMw(http.HandlerFunc(h.AddCustomerIdentity)))
	mux.Handle("GET /api/v1/customers/{id}/interactions", authMw(http.HandlerFunc(h.ListCustomerInteractions)))
	mux.Handle("POST /api/v1/customers/{id}/interactions", authMw(http.HandlerFunc(h.CreateCustomerInteraction)))
	mux.Handle("GET /api/v1/customers/{id}/follow-ups", authMw(http.HandlerFunc(h.ListCustomerFollowUps)))
	mux.Handle("POST /api/v1/customers/{id}/follow-ups", authMw(http.HandlerFunc(h.CreateCustomerFollowUp)))
	mux.Handle("GET /api/v1/customers/{id}/membership", authMw(http.HandlerFunc(h.GetCustomerMembership)))

	// Tag endpoints
	mux.Handle("GET /api/v1/customer-tags", authMw(http.HandlerFunc(h.ListCustomerTags)))
	mux.Handle("POST /api/v1/customer-tags", authMw(http.HandlerFunc(h.CreateCustomerTag)))
	mux.Handle("PUT /api/v1/customer-tags/{id}", authMw(http.HandlerFunc(h.UpdateCustomerTag)))
	mux.Handle("DELETE /api/v1/customer-tags/{id}", authMw(http.HandlerFunc(h.DeleteCustomerTag)))

	// Group endpoints
	mux.Handle("GET /api/v1/customer-groups", authMw(http.HandlerFunc(h.ListCustomerGroups)))
	mux.Handle("POST /api/v1/customer-groups", authMw(http.HandlerFunc(h.CreateCustomerGroup)))
	mux.Handle("PUT /api/v1/customer-groups/{id}", authMw(http.HandlerFunc(h.UpdateCustomerGroup)))
	mux.Handle("DELETE /api/v1/customer-groups/{id}", authMw(http.HandlerFunc(h.DeleteCustomerGroup)))

	// Advanced customer endpoints
	mux.Handle("GET /api/v1/customers/{id}/momentum-history", authMw(http.HandlerFunc(h.GetCustomerMomentumHistory)))
	mux.Handle("GET /api/v1/customers/duplicates", authMw(http.HandlerFunc(h.CheckDuplicates)))
	mux.Handle("POST /api/v1/customers/merge", authMw(http.HandlerFunc(h.MergeCustomers)))
	mux.Handle("GET /api/v1/customers/{id}/consultation-records", authMw(http.HandlerFunc(h.GetConsultationRecords)))
	mux.Handle("GET /api/v1/customers/{id}/emr-records", authMw(http.HandlerFunc(h.GetEMRRecords)))

	// Advanced tag endpoints
	mux.Handle("POST /api/v1/customer-tags/batch", authMw(http.HandlerFunc(h.BatchTagCustomers)))
	mux.Handle("GET /api/v1/customer-tags/stats", authMw(http.HandlerFunc(h.GetTagStats)))

	// Advanced group endpoints
	mux.Handle("GET /api/v1/customer-groups/{id}/members", authMw(http.HandlerFunc(h.GetGroupMembers)))
	mux.Handle("POST /api/v1/customer-groups/{id}/members", authMw(http.HandlerFunc(h.AddGroupMembers)))
	mux.Handle("DELETE /api/v1/customer-groups/{id}/members", authMw(http.HandlerFunc(h.RemoveGroupMembers)))
	mux.Handle("POST /api/v1/customer-groups/rules/preview", authMw(http.HandlerFunc(h.PreviewGroupRules)))
	mux.Handle("POST /api/v1/customer-groups/rules/validate", authMw(http.HandlerFunc(h.ValidateGroupRules)))
	mux.Handle("GET /api/v1/customer-groups/rules/fields", authMw(http.HandlerFunc(h.GetRuleFields)))
	mux.Handle("GET /api/v1/customer-groups/rules/operators", authMw(http.HandlerFunc(h.GetRuleOperators)))
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

	// Admin can view all tenants, employees can only view their own tenant
	if claims.UserType != auth.UserTypeAdmin {
		req.TenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

	if name := r.URL.Query().Get("name"); name != "" {
		req.Name = &name
	}

	if phone := r.URL.Query().Get("phone"); phone != "" {
		req.Phone = &phone
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

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
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

	var tenantID *int64
	// Admin can view all tenants, employees can only view their own tenant
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tid, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		tenantID = &tid
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
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && customer.TenantID != *claims.TenantID {
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

	// Get tenant ID
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	customer, err := h.service.CreateCustomer(r.Context(), tenantID, claims.UserID, req)
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if err := h.service.MarkCustomerConverted(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Customer marked as converted successfully"})
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req AddIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	identity, err := h.service.AddCustomerIdentity(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, identity)
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req CreateInteractionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// Get tenant ID
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	interaction, err := h.service.CreateCustomerInteraction(r.Context(), id, tenantID, claims.UserID, req)
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var req CreateFollowUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// Get tenant ID
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	followUp, err := h.service.CreateCustomerFollowUp(r.Context(), id, tenantID, claims.UserID, req)
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	membership, err := h.service.GetCustomerMembership(r.Context(), id)
	if err != nil {
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

	// Admin can view all tenants, employees can only view their own tenant
	if claims.UserType != auth.UserTypeAdmin {
		req.TenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
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

	// Get tenant ID
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	tag, err := h.service.CreateCustomerTag(r.Context(), tenantID, claims.UserID, req)
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingTag.TenantID != *claims.TenantID {
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingTag.TenantID != *claims.TenantID {
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

	// Admin can view all tenants, employees can only view their own tenant
	if claims.UserType != auth.UserTypeAdmin {
		req.TenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
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

	// Get tenant ID
	tenantID := int64(0)
	if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	group, err := h.service.CreateCustomerGroup(r.Context(), tenantID, claims.UserID, req)
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingGroup.TenantID != *claims.TenantID {
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingGroup.TenantID != *claims.TenantID {
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	// TODO: Implement momentum history retrieval from store
	httputil.WriteSuccess(w, []interface{}{})
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

	// TODO: Implement duplicate detection logic
	httputil.WriteSuccess(w, []interface{}{})
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

	// TODO: Implement customer merge logic
	httputil.WriteSuccess(w, map[string]string{"message": "Customers merged successfully"})
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
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

	// TODO: Implement consultation records retrieval from recordings table
	httputil.WritePaginated(w, []interface{}{}, 0, page, pageSize)
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingCustomer.TenantID != *claims.TenantID {
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

	// TODO: Implement EMR records retrieval from medical records table
	httputil.WritePaginated(w, []interface{}{}, 0, page, pageSize)
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

	// TODO: Implement batch tagging logic
	httputil.WriteSuccess(w, map[string]string{"message": "Batch tagging completed successfully"})
}

// GetTagStats handles getting tag statistics
func (h *Handler) GetTagStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var tenantID *int64
	// Admin can view all tenants, employees can only view their own tenant
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tid, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		tenantID = &tid
	}

	// TODO: Implement tag statistics calculation
	_ = tenantID // Will be used in implementation
	httputil.WriteSuccess(w, map[string]interface{}{
		"total_tags":        0,
		"total_assignments": 0,
		"most_used_tags":    []interface{}{},
	})
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingGroup.TenantID != *claims.TenantID {
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

	// TODO: Implement group members retrieval
	httputil.WritePaginated(w, []interface{}{}, 0, page, pageSize)
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingGroup.TenantID != *claims.TenantID {
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

	// TODO: Implement add members logic
	httputil.WriteSuccess(w, map[string]string{"message": "Members added successfully"})
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

	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil && existingGroup.TenantID != *claims.TenantID {
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

	// TODO: Implement remove members logic
	httputil.WriteSuccess(w, map[string]string{"message": "Members removed successfully"})
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

	// TODO: Implement rule preview logic
	httputil.WriteSuccess(w, RulePreviewResponse{
		MatchCount: 0,
		Customers:  []int64{},
	})
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

	// TODO: Implement rule validation logic
	httputil.WriteSuccess(w, RuleValidateResponse{
		IsValid: true,
		Errors:  []string{},
	})
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

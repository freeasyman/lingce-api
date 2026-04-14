package support

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

// RegisterRoutes registers support module routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Notification endpoints
	mux.Handle("POST /api/v1/notifications/device-tokens", authMw(http.HandlerFunc(h.RegisterDeviceToken)))
	mux.Handle("DELETE /api/v1/notifications/device-tokens/{id}", authMw(http.HandlerFunc(h.DeleteDeviceTokenByID)))
	mux.Handle("GET /api/v1/notifications", authMw(http.HandlerFunc(h.ListNotifications)))
	mux.Handle("GET /api/v1/notifications/{id}", authMw(http.HandlerFunc(h.GetNotificationByID)))
	mux.Handle("POST /api/v1/notifications/{id}/actions/read", authMw(http.HandlerFunc(h.MarkNotificationAsRead)))
	mux.Handle("POST /api/v1/notifications/actions/read-all", authMw(http.HandlerFunc(h.MarkAllNotificationsAsRead)))

	// Operation log endpoints
	mux.Handle("GET /api/v1/operation-logs", authMw(http.HandlerFunc(h.ListOperationLogs)))
	mux.Handle("GET /api/v1/operation-logs/stats", authMw(http.HandlerFunc(h.GetOperationLogStats)))
	mux.Handle("GET /api/v1/operation-logs/{id}", authMw(http.HandlerFunc(h.GetOperationLogByID)))

	// LLM model config endpoints
	mux.Handle("GET /api/v1/llm/models", authMw(http.HandlerFunc(h.ListLLMModelConfigs)))
	mux.Handle("POST /api/v1/llm/models", authMw(http.HandlerFunc(h.CreateLLMModelConfig)))
	mux.Handle("GET /api/v1/llm/models/{id}", authMw(http.HandlerFunc(h.GetLLMModelConfig)))
	mux.Handle("PUT /api/v1/llm/models/{id}", authMw(http.HandlerFunc(h.UpdateLLMModelConfig)))
	mux.Handle("DELETE /api/v1/llm/models/{id}", authMw(http.HandlerFunc(h.DeleteLLMModelConfig)))
	mux.Handle("POST /api/v1/llm/models/{id}/actions/set-default", authMw(http.HandlerFunc(h.SetDefaultLLMModelConfig)))
	mux.Handle("POST /api/v1/llm/models/{id}/set-default", authMw(http.HandlerFunc(h.SetDefaultLLMModelConfig)))

	// LLM call record endpoints
	mux.Handle("GET /api/v1/llm/records", authMw(http.HandlerFunc(h.ListLLMCallRecords)))
	mux.Handle("GET /api/v1/llm/records/stats", authMw(http.HandlerFunc(h.GetLLMCallRecordStats)))
	mux.Handle("GET /api/v1/llm/records/{id}", authMw(http.HandlerFunc(h.GetLLMCallRecordByID)))

	// LLM cost endpoints
	mux.Handle("GET /api/v1/llm/costs/by-tenant/{tenant_id}", authMw(http.HandlerFunc(h.GetLLMCostByTenant)))
	mux.Handle("GET /api/v1/llm/costs/summary", authMw(http.HandlerFunc(h.GetLLMCostSummary)))
	mux.Handle("GET /api/v1/llm/cost/tenant/{tenant_id}", authMw(http.HandlerFunc(h.GetLLMCostByTenant)))
	mux.Handle("GET /api/v1/llm/cost/summary", authMw(http.HandlerFunc(h.GetLLMCostSummary)))

	// Metadata endpoints
	mux.Handle("GET /api/v1/metadata/fields", authMw(http.HandlerFunc(h.GetMetadataFields)))
	mux.Handle("POST /api/v1/metadata/validate-template", authMw(http.HandlerFunc(h.ValidateTemplate)))

	// Data browser endpoints
	mux.Handle("GET /api/v1/operation-logs/data-browser/tables", authMw(http.HandlerFunc(h.ListTables)))
	mux.Handle("GET /api/v1/operation-logs/data-browser/tables/{table_name}/structure", authMw(http.HandlerFunc(h.GetTableStructure)))
	mux.Handle("GET /api/v1/operation-logs/data-browser/tables/{table_name}/data", authMw(http.HandlerFunc(h.GetTableData)))
	mux.Handle("GET /api/v1/operation-logs/data-browser/tables/{table_name}/export", authMw(http.HandlerFunc(h.ExportTableData)))
	mux.Handle("GET /api/v1/operation-logs/data-browser/statistics", authMw(http.HandlerFunc(h.GetDatabaseStatistics)))
	mux.Handle("DELETE /api/v1/operation-logs/data-browser/tables/{table_name}/truncate", authMw(http.HandlerFunc(h.TruncateTable)))
	mux.Handle("POST /api/v1/operation-logs/data-browser/clear-import-data", authMw(http.HandlerFunc(h.ClearImportData)))

	// Visit management endpoints
	mux.Handle("GET /api/v1/visits/health", authMw(http.HandlerFunc(h.VisitsHealthCheck)))
	mux.Handle("GET /api/v1/visits", authMw(http.HandlerFunc(h.ListVisits)))
	mux.Handle("GET /api/v1/visits/statistics", authMw(http.HandlerFunc(h.GetVisitStatistics)))
	mux.Handle("GET /api/v1/visits/filters", authMw(http.HandlerFunc(h.GetVisitFilters)))
	mux.Handle("GET /api/v1/visits/{id}", authMw(http.HandlerFunc(h.GetVisitByID)))
}

// Notification Handlers

// RegisterDeviceToken handles registering device token
func (h *Handler) RegisterDeviceToken(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req RegisterDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	token, err := h.service.RegisterDeviceToken(r.Context(), claims.UserID, string(claims.UserType), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, token)
}

// UnregisterDeviceToken handles unregistering device token
func (h *Handler) UnregisterDeviceToken(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req UnregisterDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.UnregisterDeviceToken(r.Context(), claims.UserID, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Device token unregistered successfully"})
}

// DeleteDeviceTokenByID handles deleting device token by ID.
func (h *Handler) DeleteDeviceTokenByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid device token ID")
		return
	}

	if err := h.service.DeleteDeviceTokenByID(r.Context(), claims.UserID, id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Device token unregistered successfully"})
}

// GetMyDeviceTokens handles getting user's device tokens
func (h *Handler) GetMyDeviceTokens(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tokens, err := h.service.GetUserDeviceTokens(r.Context(), claims.UserID, string(claims.UserType))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tokens)
}

// PushNotification handles pushing notification
func (h *Handler) PushNotification(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can push notifications
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req PushNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.PushNotification(r.Context(), req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Notifications pushed successfully"})
}

// ListNotifications handles listing notifications
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req NotificationListRequest
	req.UserID = claims.UserID
	req.UserType = string(claims.UserType)

	if typeStr := r.URL.Query().Get("type"); typeStr != "" {
		req.Type = &typeStr
	}

	if isReadStr := r.URL.Query().Get("is_read"); isReadStr != "" {
		isRead := isReadStr == "true"
		req.IsRead = &isRead
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	notifications, total, err := h.service.ListNotifications(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, notifications, int64(total), req.Page, req.PageSize)
}

// GetNotificationByID handles getting notification detail.
func (h *Handler) GetNotificationByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid notification ID")
		return
	}

	notification, err := h.service.GetNotificationByID(r.Context(), id, claims.UserID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, notification)
}

// GetUnreadCount handles getting unread notification count
func (h *Handler) GetUnreadCount(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	count, err := h.service.GetUnreadCount(r.Context(), claims.UserID, string(claims.UserType))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, UnreadCountResponse{UnreadCount: count})
}

// MarkNotificationAsRead handles marking notification as read
func (h *Handler) MarkNotificationAsRead(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid notification ID")
		return
	}

	if err := h.service.MarkNotificationAsRead(r.Context(), id, claims.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Notification marked as read"})
}

// MarkAllNotificationsAsRead handles marking all notifications as read
func (h *Handler) MarkAllNotificationsAsRead(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	if err := h.service.MarkAllNotificationsAsRead(r.Context(), claims.UserID, string(claims.UserType)); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "All notifications marked as read"})
}

// Operation Log Handlers

// ListOperationLogs handles listing operation logs
func (h *Handler) ListOperationLogs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view operation logs
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req OperationLogListRequest

	if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

	if userIDStr := r.URL.Query().Get("user_id"); userIDStr != "" {
		userID, _ := strconv.ParseInt(userIDStr, 10, 64)
		req.UserID = &userID
	}

	if action := r.URL.Query().Get("action"); action != "" {
		req.Action = &action
	}

	if resource := r.URL.Query().Get("resource"); resource != "" {
		req.Resource = &resource
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

	logs, total, err := h.service.ListOperationLogs(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, logs, int64(total), req.Page, req.PageSize)
}

// GetOperationLogStats handles getting operation log statistics
func (h *Handler) GetOperationLogStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view operation log stats
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var tenantID *int64
	if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tid, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		tenantID = &tid
	}

	var startDate, endDate *string
	if sd := r.URL.Query().Get("start_date"); sd != "" {
		startDate = &sd
	}
	if ed := r.URL.Query().Get("end_date"); ed != "" {
		endDate = &ed
	}

	stats, err := h.service.GetOperationLogStats(r.Context(), tenantID, startDate, endDate)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

// GetOperationLogByID handles getting operation log by ID
func (h *Handler) GetOperationLogByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view operation logs
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid log ID")
		return
	}

	log, err := h.service.GetOperationLogByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, log)
}

// LLM Model Config Handlers

// ListLLMModelConfigs handles listing LLM model configs
func (h *Handler) ListLLMModelConfigs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view LLM model configs
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req LLMModelConfigListRequest

	if provider := r.URL.Query().Get("provider"); provider != "" {
		req.Provider = &provider
	}

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	configs, total, err := h.service.ListLLMModelConfigs(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, configs, int64(total), req.Page, req.PageSize)
}

// GetLLMModelConfig handles getting LLM model config by ID
func (h *Handler) GetLLMModelConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view LLM model configs
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid config ID")
		return
	}

	config, err := h.service.GetLLMModelConfig(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, config)
}

// CreateLLMModelConfig handles creating LLM model config
func (h *Handler) CreateLLMModelConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can create LLM model configs
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateLLMModelConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	config, err := h.service.CreateLLMModelConfig(r.Context(), claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, config)
}

// UpdateLLMModelConfig handles updating LLM model config
func (h *Handler) UpdateLLMModelConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can update LLM model configs
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid config ID")
		return
	}

	var req UpdateLLMModelConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	config, err := h.service.UpdateLLMModelConfig(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, config)
}

// DeleteLLMModelConfig handles deleting LLM model config
func (h *Handler) DeleteLLMModelConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can delete LLM model configs
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid config ID")
		return
	}

	if err := h.service.DeleteLLMModelConfig(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "LLM model config deleted successfully"})
}

// SetDefaultLLMModelConfig handles setting default LLM model config
func (h *Handler) SetDefaultLLMModelConfig(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can set default LLM model config
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid config ID")
		return
	}

	if err := h.service.SetDefaultLLMModelConfig(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Default LLM model config set successfully"})
}

// LLM Call Record Handlers

// ListLLMCallRecords handles listing LLM call records
func (h *Handler) ListLLMCallRecords(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view LLM call records
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req LLMCallRecordListRequest

	if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tenantID, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		req.TenantID = &tenantID
	}

	if userIDStr := r.URL.Query().Get("user_id"); userIDStr != "" {
		userID, _ := strconv.ParseInt(userIDStr, 10, 64)
		req.UserID = &userID
	}

	if modelConfigIDStr := r.URL.Query().Get("model_config_id"); modelConfigIDStr != "" {
		modelConfigID, _ := strconv.ParseInt(modelConfigIDStr, 10, 64)
		req.ModelConfigID = &modelConfigID
	}

	if provider := r.URL.Query().Get("provider"); provider != "" {
		req.Provider = &provider
	}

	if status := r.URL.Query().Get("status"); status != "" {
		req.Status = &status
	}

	if purpose := r.URL.Query().Get("purpose"); purpose != "" {
		req.Purpose = &purpose
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

	records, total, err := h.service.ListLLMCallRecords(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, records, int64(total), req.Page, req.PageSize)
}

// GetLLMCallRecordStats handles getting LLM call record statistics
func (h *Handler) GetLLMCallRecordStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view LLM call record stats
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var tenantID *int64
	if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		tid, _ := strconv.ParseInt(tenantIDStr, 10, 64)
		tenantID = &tid
	}

	var startDate, endDate *string
	if sd := r.URL.Query().Get("start_date"); sd != "" {
		startDate = &sd
	}
	if ed := r.URL.Query().Get("end_date"); ed != "" {
		endDate = &ed
	}

	stats, err := h.service.GetLLMCallRecordStats(r.Context(), tenantID, startDate, endDate)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}

// GetLLMCallRecordByID handles getting LLM call record by ID
func (h *Handler) GetLLMCallRecordByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view LLM call records
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid record ID")
		return
	}

	record, err := h.service.GetLLMCallRecordByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, record)
}

// LLM Cost Handlers

// GetLLMCostByTenant handles getting LLM cost by tenant
func (h *Handler) GetLLMCostByTenant(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view LLM costs
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	tenantID, err := strconv.ParseInt(r.PathValue("tenant_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}

	var startDate, endDate *string
	if sd := r.URL.Query().Get("start_date"); sd != "" {
		startDate = &sd
	}
	if ed := r.URL.Query().Get("end_date"); ed != "" {
		endDate = &ed
	}

	cost, err := h.service.GetLLMCostByTenant(r.Context(), tenantID, startDate, endDate)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, cost)
}

// GetLLMCostSummary handles getting overall LLM cost summary
func (h *Handler) GetLLMCostSummary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can view LLM cost summary
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var startDate, endDate *string
	if sd := r.URL.Query().Get("start_date"); sd != "" {
		startDate = &sd
	}
	if ed := r.URL.Query().Get("end_date"); ed != "" {
		endDate = &ed
	}

	summary, err := h.service.GetLLMCostSummary(r.Context(), startDate, endDate)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, summary)
}

// Metadata Handlers

// GetMetadataFields handles getting metadata fields
func (h *Handler) GetMetadataFields(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	fields, err := h.service.GetMetadataFields(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, fields)
}

// ValidateTemplate handles validating template
func (h *Handler) ValidateTemplate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req ValidateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	result, err := h.service.ValidateTemplate(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, result)
}

// Data Browser Handlers

// ListTables handles listing database tables
func (h *Handler) ListTables(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can access data browser
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can access data browser")
		return
	}

	tables, err := h.service.ListTables(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"tables": tables,
		"count":  len(tables),
	})
}

// GetTableStructure handles getting table structure
func (h *Handler) GetTableStructure(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can access data browser
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can access data browser")
		return
	}

	tableName := r.PathValue("table_name")
	if tableName == "" {
		httputil.WriteBadRequest(w, "Table name is required")
		return
	}

	columns, err := h.service.GetTableStructure(r.Context(), tableName)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"table_name": tableName,
		"columns":    columns,
	})
}

// GetTableData handles getting table data
func (h *Handler) GetTableData(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can access data browser
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can access data browser")
		return
	}

	tableName := r.PathValue("table_name")
	if tableName == "" {
		httputil.WriteBadRequest(w, "Table name is required")
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

	data, total, err := h.service.GetTableData(r.Context(), tableName, page, pageSize)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WritePaginated(w, data, total, page, pageSize)
}

// ExportTableData handles exporting table data
func (h *Handler) ExportTableData(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can access data browser
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can access data browser")
		return
	}

	tableName := r.PathValue("table_name")
	if tableName == "" {
		httputil.WriteBadRequest(w, "Table name is required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	csvData, total, err := h.service.ExportTableDataCSV(r.Context(), tableName, limit)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"table_name":  tableName,
		"total_rows":  total,
		"csv_preview": csvData,
		"message":     "CSV export generated",
	})
}

// GetDatabaseStatistics handles getting database statistics
func (h *Handler) GetDatabaseStatistics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can access data browser
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can access data browser")
		return
	}

	stats, err := h.service.GetDatabaseStatistics(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, stats)
}

// TruncateTable handles truncating a table
func (h *Handler) TruncateTable(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can access data browser
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can access data browser")
		return
	}

	tableName := r.PathValue("table_name")
	if tableName == "" {
		httputil.WriteBadRequest(w, "Table name is required")
		return
	}

	if err := h.service.TruncateTable(r.Context(), tableName); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{
		"message": "Table truncated successfully",
	})
}

// ClearImportData handles clearing import data
func (h *Handler) ClearImportData(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can access data browser
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Only admin can access data browser")
		return
	}

	count, err := h.service.ClearImportData(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{
		"message": "Import data cleared successfully: " + strconv.Itoa(count) + " tables",
	})
}

// Visit Management Handlers

// VisitsHealthCheck handles visits health check
func (h *Handler) VisitsHealthCheck(w http.ResponseWriter, r *http.Request) {
	httputil.WriteSuccess(w, map[string]string{
		"status": "ok",
	})
}

// ListVisits handles listing visits
func (h *Handler) ListVisits(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
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

	var tenantID *int64
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		if parsed, err := strconv.ParseInt(tenantIDStr, 10, 64); err == nil {
			tenantID = &parsed
		}
	}
	items, total, err := h.service.ListVisits(r.Context(), tenantID, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

// GetVisitStatistics handles getting visit statistics
func (h *Handler) GetVisitStatistics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var tenantID *int64
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		if parsed, err := strconv.ParseInt(tenantIDStr, 10, 64); err == nil {
			tenantID = &parsed
		}
	}
	stats, err := h.service.GetVisitStatistics(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, stats)
}

// GetVisitFilters handles getting visit filters
func (h *Handler) GetVisitFilters(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var tenantID *int64
	if claims.UserType != auth.UserTypeAdmin {
		tenantID = claims.TenantID
	} else if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
		if parsed, err := strconv.ParseInt(tenantIDStr, 10, 64); err == nil {
			tenantID = &parsed
		}
	}
	filters, err := h.service.GetVisitFilters(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, filters)
}

// GetVisitByID handles getting visit by ID
func (h *Handler) GetVisitByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	visitIDStr := r.PathValue("id")
	if visitIDStr == "" {
		httputil.WriteBadRequest(w, "Visit ID is required")
		return
	}
	visitID, err := strconv.ParseInt(visitIDStr, 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid visit ID")
		return
	}

	visit, err := h.service.GetVisitByID(r.Context(), visitID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin && claims.TenantID != nil {
		if tenantID, ok := visit["tenant_id"].(int64); ok && tenantID != *claims.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}
	httputil.WriteSuccess(w, visit)
}

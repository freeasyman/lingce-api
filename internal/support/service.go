package support

import (
	"context"
	"fmt"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Notification Services

// RegisterDeviceToken registers a device token for push notifications
func (s *Service) RegisterDeviceToken(ctx context.Context, userID int64, userType string, req RegisterDeviceTokenRequest) (*DeviceToken, error) {
	// Validate request
	if req.Token == "" {
		return nil, fmt.Errorf("token is required")
	}
	if req.Platform == "" {
		return nil, fmt.Errorf("platform is required")
	}
	if req.Platform != "ios" && req.Platform != "android" {
		return nil, fmt.Errorf("platform must be 'ios' or 'android'")
	}

	return s.store.RegisterDeviceToken(ctx, userID, userType, req)
}

// UnregisterDeviceToken unregisters a device token
func (s *Service) UnregisterDeviceToken(ctx context.Context, userID int64, req UnregisterDeviceTokenRequest) error {
	if req.Token == "" {
		return fmt.Errorf("token is required")
	}

	return s.store.UnregisterDeviceToken(ctx, userID, req.Token)
}

// GetUserDeviceTokens retrieves device tokens for a user
func (s *Service) GetUserDeviceTokens(ctx context.Context, userID int64, userType string) ([]*DeviceToken, error) {
	return s.store.GetUserDeviceTokens(ctx, userID, userType)
}

// PushNotification pushes a notification to users
func (s *Service) PushNotification(ctx context.Context, req PushNotificationRequest) error {
	// Validate request
	if len(req.UserIDs) == 0 {
		return fmt.Errorf("user_ids is required")
	}
	if req.UserType == "" {
		return fmt.Errorf("user_type is required")
	}
	if req.Title == "" {
		return fmt.Errorf("title is required")
	}
	if req.Content == "" {
		return fmt.Errorf("content is required")
	}
	if req.Type == "" {
		return fmt.Errorf("type is required")
	}

	// Create notifications for each user
	for _, userID := range req.UserIDs {
		notification := &Notification{
			UserID:      userID,
			UserType:    req.UserType,
			Title:       req.Title,
			Content:     req.Content,
			Type:        req.Type,
			RelatedID:   req.RelatedID,
			RelatedType: req.RelatedType,
			ExtraData:   req.ExtraData,
		}

		if err := s.store.CreateNotification(ctx, notification); err != nil {
			return fmt.Errorf("failed to create notification for user %d: %w", userID, err)
		}
	}

	// TODO: Send push notifications via Expo Push API

	return nil
}

// ListNotifications retrieves a paginated list of notifications
func (s *Service) ListNotifications(ctx context.Context, req NotificationListRequest) ([]*NotificationResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	notifications, total, err := s.store.ListNotifications(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*NotificationResponse, len(notifications))
	for i, n := range notifications {
		responses[i] = toNotificationResponse(n)
	}

	return responses, total, nil
}

// GetUnreadCount retrieves the count of unread notifications
func (s *Service) GetUnreadCount(ctx context.Context, userID int64, userType string) (int64, error) {
	return s.store.GetUnreadCount(ctx, userID, userType)
}

// MarkNotificationAsRead marks a notification as read
func (s *Service) MarkNotificationAsRead(ctx context.Context, notificationID, userID int64) error {
	return s.store.MarkNotificationAsRead(ctx, notificationID, userID)
}

// MarkAllNotificationsAsRead marks all notifications as read for a user
func (s *Service) MarkAllNotificationsAsRead(ctx context.Context, userID int64, userType string) error {
	return s.store.MarkAllNotificationsAsRead(ctx, userID, userType)
}

// Operation Log Services

// ListOperationLogs retrieves a paginated list of operation logs
func (s *Service) ListOperationLogs(ctx context.Context, req OperationLogListRequest) ([]*OperationLogResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	logs, total, err := s.store.ListOperationLogs(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*OperationLogResponse, len(logs))
	for i, l := range logs {
		responses[i] = toOperationLogResponse(l)
	}

	return responses, total, nil
}

// GetOperationLogStats retrieves operation log statistics
func (s *Service) GetOperationLogStats(ctx context.Context, tenantID *int64, startDate, endDate *string) (*OperationLogStatsResponse, error) {
	return s.store.GetOperationLogStats(ctx, tenantID, startDate, endDate)
}

// GetOperationLogByID retrieves an operation log by ID
func (s *Service) GetOperationLogByID(ctx context.Context, id int64) (*OperationLogResponse, error) {
	log, err := s.store.GetOperationLogByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toOperationLogResponse(log), nil
}

// LLM Model Config Services

// ListLLMModelConfigs retrieves a paginated list of LLM model configs
func (s *Service) ListLLMModelConfigs(ctx context.Context, req LLMModelConfigListRequest) ([]*LLMModelConfigResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	configs, total, err := s.store.ListLLMModelConfigs(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*LLMModelConfigResponse, len(configs))
	for i, c := range configs {
		responses[i] = toLLMModelConfigResponse(c)
	}

	return responses, total, nil
}

// GetLLMModelConfig retrieves an LLM model config by ID
func (s *Service) GetLLMModelConfig(ctx context.Context, id int64) (*LLMModelConfigResponse, error) {
	config, err := s.store.GetLLMModelConfigByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toLLMModelConfigResponse(config), nil
}

// CreateLLMModelConfig creates a new LLM model config
func (s *Service) CreateLLMModelConfig(ctx context.Context, createdBy int64, req CreateLLMModelConfigRequest) (*LLMModelConfigResponse, error) {
	// Validate request
	if req.ModelName == "" {
		return nil, fmt.Errorf("model_name is required")
	}
	if req.Provider == "" {
		return nil, fmt.Errorf("provider is required")
	}
	if req.APIEndpoint == "" {
		return nil, fmt.Errorf("api_endpoint is required")
	}
	if req.APIKey == "" {
		return nil, fmt.Errorf("api_key is required")
	}

	config, err := s.store.CreateLLMModelConfig(ctx, createdBy, req)
	if err != nil {
		return nil, err
	}

	return toLLMModelConfigResponse(config), nil
}

// UpdateLLMModelConfig updates an LLM model config
func (s *Service) UpdateLLMModelConfig(ctx context.Context, id int64, req UpdateLLMModelConfigRequest) (*LLMModelConfigResponse, error) {
	config, err := s.store.UpdateLLMModelConfig(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toLLMModelConfigResponse(config), nil
}

// DeleteLLMModelConfig deletes an LLM model config
func (s *Service) DeleteLLMModelConfig(ctx context.Context, id int64) error {
	return s.store.DeleteLLMModelConfig(ctx, id)
}

// SetDefaultLLMModelConfig sets an LLM model config as default
func (s *Service) SetDefaultLLMModelConfig(ctx context.Context, id int64) error {
	return s.store.SetDefaultLLMModelConfig(ctx, id)
}

// LLM Call Record Services

// ListLLMCallRecords retrieves a paginated list of LLM call records
func (s *Service) ListLLMCallRecords(ctx context.Context, req LLMCallRecordListRequest) ([]*LLMCallRecordResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	records, total, err := s.store.ListLLMCallRecords(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*LLMCallRecordResponse, len(records))
	for i, r := range records {
		responses[i] = toLLMCallRecordResponse(r)
	}

	return responses, total, nil
}

// GetLLMCallRecordStats retrieves LLM call record statistics
func (s *Service) GetLLMCallRecordStats(ctx context.Context, tenantID *int64, startDate, endDate *string) (*LLMCallRecordStatsResponse, error) {
	return s.store.GetLLMCallRecordStats(ctx, tenantID, startDate, endDate)
}

// GetLLMCallRecordByID retrieves an LLM call record by ID
func (s *Service) GetLLMCallRecordByID(ctx context.Context, id int64) (*LLMCallRecordResponse, error) {
	record, err := s.store.GetLLMCallRecordByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toLLMCallRecordResponse(record), nil
}

// LLM Cost Services

// GetLLMCostByTenant retrieves LLM cost for a tenant
func (s *Service) GetLLMCostByTenant(ctx context.Context, tenantID int64, startDate, endDate *string) (*LLMCostTenantResponse, error) {
	return s.store.GetLLMCostByTenant(ctx, tenantID, startDate, endDate)
}

// GetLLMCostSummary retrieves overall LLM cost summary
func (s *Service) GetLLMCostSummary(ctx context.Context, startDate, endDate *string) (*LLMCostSummaryResponse, error) {
	return s.store.GetLLMCostSummary(ctx, startDate, endDate)
}

// Metadata Services

// GetMetadataFields retrieves metadata fields
func (s *Service) GetMetadataFields(ctx context.Context) (*MetadataFieldsResponse, error) {
	// TODO: Implement metadata field retrieval from database schema
	// This would typically query information_schema or use reflection
	return &MetadataFieldsResponse{
		Fields: []MetadataField{},
	}, nil
}

// ValidateTemplate validates a template
func (s *Service) ValidateTemplate(ctx context.Context, req ValidateTemplateRequest) (*ValidateTemplateResponse, error) {
	// TODO: Implement template validation logic
	// This would parse the template and check for valid field references
	return &ValidateTemplateResponse{
		IsValid: true,
		Errors:  []string{},
	}, nil
}

// Helper functions

// toNotificationResponse converts a Notification to NotificationResponse
func toNotificationResponse(n *Notification) *NotificationResponse {
	resp := &NotificationResponse{
		ID:          n.ID,
		Title:       n.Title,
		Content:     n.Content,
		Type:        n.Type,
		RelatedID:   n.RelatedID,
		RelatedType: n.RelatedType,
		IsRead:      n.IsRead,
		ExtraData:   n.ExtraData,
		CreatedAt:   n.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if n.ReadAt != nil {
		formatted := n.ReadAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ReadAt = &formatted
	}

	return resp
}

// toOperationLogResponse converts an OperationLog to OperationLogResponse
func toOperationLogResponse(l *OperationLog) *OperationLogResponse {
	return &OperationLogResponse{
		ID:           l.ID,
		TenantID:     l.TenantID,
		UserID:       l.UserID,
		Username:     l.Username,
		Action:       l.Action,
		Resource:     l.Resource,
		ResourceID:   l.ResourceID,
		Method:       l.Method,
		Path:         l.Path,
		IPAddress:    l.IPAddress,
		ResponseCode: l.ResponseCode,
		ErrorMessage: l.ErrorMessage,
		Duration:     l.Duration,
		CreatedAt:    l.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// toLLMModelConfigResponse converts an LLMModelConfig to LLMModelConfigResponse
func toLLMModelConfigResponse(c *LLMModelConfig) *LLMModelConfigResponse {
	return &LLMModelConfigResponse{
		ID:          c.ID,
		ModelName:   c.ModelName,
		Provider:    c.Provider,
		APIEndpoint: c.APIEndpoint,
		ModelParams: c.ModelParams,
		IsDefault:   c.IsDefault,
		IsActive:    c.IsActive,
		Description: c.Description,
		CreatedAt:   c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// toLLMCallRecordResponse converts an LLMCallRecord to LLMCallRecordResponse
func toLLMCallRecordResponse(r *LLMCallRecord) *LLMCallRecordResponse {
	return &LLMCallRecordResponse{
		ID:               r.ID,
		TenantID:         r.TenantID,
		UserID:           r.UserID,
		ModelName:        r.ModelName,
		Provider:         r.Provider,
		PromptTokens:     r.PromptTokens,
		CompletionTokens: r.CompletionTokens,
		TotalTokens:      r.TotalTokens,
		Cost:             r.Cost,
		Duration:         r.Duration,
		Status:           r.Status,
		ErrorMessage:     r.ErrorMessage,
		Purpose:          r.Purpose,
		RelatedID:        r.RelatedID,
		RelatedType:      r.RelatedType,
		CreatedAt:        r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

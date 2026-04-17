package support

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

type Service struct {
	store     *Store
	llmClient *llmgateway.Client
}

func NewService(store *Store, llmClient *llmgateway.Client) *Service {
	return &Service{store: store, llmClient: llmClient}
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

// DeleteDeviceTokenByID unregisters a device token by record ID.
func (s *Service) DeleteDeviceTokenByID(ctx context.Context, userID, tokenID int64) error {
	return s.store.DeleteDeviceTokenByID(ctx, userID, tokenID)
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

// GetNotificationByID retrieves a notification by ID.
func (s *Service) GetNotificationByID(ctx context.Context, notificationID, userID int64) (*NotificationResponse, error) {
	notification, err := s.store.GetNotificationByID(ctx, notificationID, userID)
	if err != nil {
		return nil, err
	}

	return toNotificationResponse(notification), nil
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
	if s.llmClient == nil {
		return nil, 0, fmt.Errorf("LLM gateway is not configured")
	}

	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	size := req.Size
	if size <= 0 {
		size = req.PageSize
	}
	if size <= 0 {
		size = 50
	}
	if size > 100 {
		size = 100
	}

	startDate, endDate := normalizeDateRange(req.StartDate, req.EndDate)
	params := map[string]string{
		"page":       fmt.Sprintf("%d", req.Page),
		"size":       fmt.Sprintf("%d", size),
		"start_date": startDate,
		"end_date":   endDate,
	}
	if req.TenantID != nil {
		params["tenant_id"] = fmt.Sprintf("%d", *req.TenantID)
	}
	if req.FunctionType != nil {
		params["function_type"] = *req.FunctionType
	}
	if req.Module != nil {
		params["module"] = *req.Module
	}
	if req.ModelCode != nil {
		params["model_code"] = *req.ModelCode
	}
	if req.Provider != nil {
		params["provider"] = *req.Provider
	}
	if req.Success != nil {
		params["success"] = fmt.Sprintf("%t", *req.Success)
	}
	if req.TraceID != nil {
		params["trace_id"] = *req.TraceID
	}

	resp, err := s.llmClient.ListAuditRecords(ctx, params)
	if err != nil {
		return nil, 0, err
	}

	tenantIDs := make([]int64, 0, len(resp.Items))
	seen := make(map[int64]struct{})
	for _, item := range resp.Items {
		if _, ok := seen[item.TenantID]; ok {
			continue
		}
		seen[item.TenantID] = struct{}{}
		tenantIDs = append(tenantIDs, item.TenantID)
	}
	tenantNameMap, err := s.store.GetTenantNameMap(ctx, tenantIDs)
	if err != nil {
		return nil, 0, err
	}

	out := make([]*LLMCallRecordResponse, 0, len(resp.Items))
	for _, item := range resp.Items {
		tenantID := item.TenantID
		tenantName := tenantNameMap[tenantID]
		functionType := item.FunctionType
		module := item.Module
		traceID := item.RequestID
		modelCode := item.ModelCode
		success := item.Success
		inCost := item.InputCost
		outCost := item.OutputCost
		out = append(out, &LLMCallRecordResponse{
			ID:               item.ID,
			RequestID:        item.RequestID,
			TenantID:         &tenantID,
			TenantName:       &tenantName,
			ModelName:        item.ModelCode,
			FunctionType:     &functionType,
			Module:           &module,
			TraceID:          &traceID,
			ModelCode:        &modelCode,
			Provider:         item.Provider,
			PromptTokens:     int(item.InputTokens),
			CompletionTokens: int(item.OutputTokens),
			TotalTokens:      int(item.TotalTokens),
			Cost:             item.TotalCost,
			Duration:         int(item.LatencyMS),
			Status:           boolToStatus(item.Success),
			Success:          &success,
			InputCost:        &inCost,
			OutputCost:       &outCost,
			CreatedAt:        item.CreatedAt,
		})
	}

	return out, int(resp.Total), nil
}

// GetLLMCallRecordStats retrieves LLM call record statistics
func (s *Service) GetLLMCallRecordStats(ctx context.Context, tenantID *int64, startDate, endDate *string) (*LLMCallRecordStatsResponse, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM gateway is not configured")
	}
	sd, ed := normalizeDateRange(startDate, endDate)
	params := map[string]string{
		"start_date": sd,
		"end_date":   ed,
	}
	if tenantID != nil {
		params["tenant_id"] = fmt.Sprintf("%d", *tenantID)
	}

	resp, err := s.llmClient.GetAuditRecordStats(ctx, params)
	if err != nil {
		return nil, err
	}

	return &LLMCallRecordStatsResponse{
		TotalCalls:        resp.TotalCalls,
		SuccessCalls:      resp.SuccessCount,
		FailedCalls:       resp.FailureCount,
		SuccessCount:      resp.SuccessCount,
		FailureCount:      resp.FailureCount,
		SuccessRate:       resp.SuccessRate,
		TotalTokens:       resp.TotalTokens,
		TotalInputTokens:  resp.TotalInputTokens,
		TotalOutputTokens: resp.TotalOutputTokens,
		TotalCost:         resp.TotalCost,
		AvgDuration:       resp.AvgLatencyMS,
		AvgLatencyMS:      resp.AvgLatencyMS,
	}, nil
}

// GetLLMCallRecordByRequestID retrieves an LLM call record by request ID.
func (s *Service) GetLLMCallRecordByRequestID(ctx context.Context, requestID string) (*LLMCallRecordResponse, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM gateway is not configured")
	}
	record, err := s.llmClient.GetAuditRecordByRequestID(ctx, requestID)
	if err != nil {
		return nil, err
	}

	tenantID := record.TenantID
	tenantName := ""
	if nameMap, err := s.store.GetTenantNameMap(ctx, []int64{tenantID}); err == nil {
		tenantName = nameMap[tenantID]
	}
	functionType := record.FunctionType
	module := record.Module
	traceID := record.TraceID
	modelCode := record.ModelCode
	success := record.Success
	inCost := record.InputCost
	outCost := record.OutputCost
	errMessage := record.ErrorMessage
	return &LLMCallRecordResponse{
		ID:               record.ID,
		RequestID:        record.RequestID,
		TenantID:         &tenantID,
		TenantName:       &tenantName,
		ModelName:        record.ModelCode,
		FunctionType:     &functionType,
		Module:           &module,
		TraceID:          &traceID,
		ModelCode:        &modelCode,
		Provider:         record.Provider,
		PromptTokens:     int(record.InputTokens),
		CompletionTokens: int(record.OutputTokens),
		TotalTokens:      int(record.TotalTokens),
		Cost:             record.TotalCost,
		Duration:         int(record.LatencyMS),
		Status:           boolToStatus(record.Success),
		Success:          &success,
		InputCost:        &inCost,
		OutputCost:       &outCost,
		ErrorMessage:     &errMessage,
		CreatedAt:        record.CreatedAt,
	}, nil
}

// LLM Cost Services

// GetLLMCostByTenant retrieves LLM cost for a tenant
func (s *Service) GetLLMCostByTenant(ctx context.Context, tenantID int64, startDate, endDate *string) (*LLMCostTenantResponse, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM gateway is not configured")
	}
	sd, ed := normalizeDateRange(startDate, endDate)
	resp, err := s.llmClient.GetAuditCostByTenant(ctx, tenantID, sd, ed)
	if err != nil {
		return nil, err
	}
	tenantName := ""
	if nameMap, err := s.store.GetTenantNameMap(ctx, []int64{tenantID}); err == nil {
		tenantName = nameMap[tenantID]
	}
	return &LLMCostTenantResponse{
		TenantID:           tenantID,
		TenantName:         tenantName,
		TotalCost:          resp.TotalCost,
		TotalCalls:         resp.TotalCalls,
		TotalTokens:        resp.TotalTokens,
		CostByFunctionType: toCostBreakdown(resp.CostByFunctionType),
		CostByModule:       toCostBreakdown(resp.CostByModule),
		CostByModel:        toCostBreakdown(resp.CostByModel),
		CostTrend:          toCostTrend(resp.CostTrend),
	}, nil
}

// GetLLMCostSummary retrieves overall LLM cost summary
func (s *Service) GetLLMCostSummary(ctx context.Context, startDate, endDate *string) (*LLMCostSummaryResponse, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM gateway is not configured")
	}
	sd, ed := normalizeDateRange(startDate, endDate)
	resp, err := s.llmClient.GetAuditCostSummary(ctx, sd, ed)
	if err != nil {
		return nil, err
	}

	tenantIDs := make([]int64, 0, len(resp.CostByTenant))
	for _, item := range resp.CostByTenant {
		tenantIDs = append(tenantIDs, item.TenantID)
	}
	tenantNameMap, err := s.store.GetTenantNameMap(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}

	byTenant := make([]TenantCostSummary, 0, len(resp.CostByTenant))
	for _, item := range resp.CostByTenant {
		byTenant = append(byTenant, TenantCostSummary{
			TenantID:    item.TenantID,
			TenantName:  tenantNameMap[item.TenantID],
			CallCount:   item.CallCount,
			TotalTokens: item.TotalTokens,
			TotalCost:   item.TotalCost,
		})
	}

	return &LLMCostSummaryResponse{
		TotalCost:          resp.TotalCost,
		TotalCalls:         resp.TotalCalls,
		TotalTokens:        resp.TotalTokens,
		CostByTenant:       byTenant,
		CostByFunctionType: toCostBreakdown(resp.CostByFunctionType),
		CostByModule:       toCostBreakdown(resp.CostByModule),
		CostByModel:        toCostBreakdown(resp.CostByModel),
	}, nil
}

// Metadata Services

// GetMetadataFields retrieves metadata fields
func (s *Service) GetMetadataFields(ctx context.Context) (*MetadataFieldsResponse, error) {
	tables, err := s.store.ListTables(ctx)
	if err != nil {
		return nil, err
	}

	fields := make([]MetadataField, 0, 256)
	for _, table := range tables {
		structure, err := s.store.GetTableStructure(ctx, table)
		if err != nil {
			return nil, err
		}
		for _, col := range structure {
			columnName, _ := col["column_name"].(string)
			dataType, _ := col["data_type"].(string)
			isNullable, _ := col["is_nullable"].(bool)
			var defaultValue *string
			switch v := col["column_default"].(type) {
			case *string:
				defaultValue = v
			case string:
				if strings.TrimSpace(v) != "" {
					value := v
					defaultValue = &value
				}
			}

			fields = append(fields, MetadataField{
				FieldName:    table + "." + columnName,
				FieldType:    normalizeMetadataFieldType(dataType),
				Description:  fmt.Sprintf("Field %s from table %s", columnName, table),
				TableName:    table,
				IsRequired:   !isNullable,
				DefaultValue: defaultValue,
			})
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		if fields[i].TableName == fields[j].TableName {
			return fields[i].FieldName < fields[j].FieldName
		}
		return fields[i].TableName < fields[j].TableName
	})

	return &MetadataFieldsResponse{
		Fields: fields,
	}, nil
}

// ValidateTemplate validates a template
func (s *Service) ValidateTemplate(ctx context.Context, req ValidateTemplateRequest) (*ValidateTemplateResponse, error) {
	template := strings.TrimSpace(req.Template)
	if template == "" {
		return &ValidateTemplateResponse{
			IsValid: false,
			Errors:  []string{"template is required"},
		}, nil
	}

	fieldsResp, err := s.GetMetadataFields(ctx)
	if err != nil {
		return nil, err
	}

	validFields := make(map[string]struct{}, len(fieldsResp.Fields))
	for _, f := range fieldsResp.Fields {
		validFields[f.FieldName] = struct{}{}
	}

	re := regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_\.]+)\s*\}\}`)
	matches := re.FindAllStringSubmatch(template, -1)
	usedSet := map[string]struct{}{}
	missingSet := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		field := strings.TrimSpace(match[1])
		if field == "" {
			continue
		}
		usedSet[field] = struct{}{}
		if _, ok := validFields[field]; !ok {
			missingSet[field] = struct{}{}
		}
	}

	usedFields := setToSortedSlice(usedSet)
	missingFields := setToSortedSlice(missingSet)
	errors := []string{}
	if len(missingFields) > 0 {
		errors = append(errors, fmt.Sprintf("unknown fields: %s", strings.Join(missingFields, ", ")))
	}

	return &ValidateTemplateResponse{
		IsValid:       len(errors) == 0,
		Errors:        errors,
		UsedFields:    usedFields,
		MissingFields: missingFields,
	}, nil
}

// Data browser services

func (s *Service) ListTables(ctx context.Context) ([]string, error) {
	return s.store.ListTables(ctx)
}

func (s *Service) GetTableStructure(ctx context.Context, tableName string) ([]map[string]interface{}, error) {
	return s.store.GetTableStructure(ctx, tableName)
}

func (s *Service) GetTableData(ctx context.Context, tableName string, page, pageSize int) ([]map[string]interface{}, int64, error) {
	return s.store.GetTableData(ctx, tableName, page, pageSize)
}

func (s *Service) ExportTableDataCSV(ctx context.Context, tableName string, limit int) (string, int64, error) {
	return s.store.ExportTableDataCSV(ctx, tableName, limit)
}

func (s *Service) GetDatabaseStatistics(ctx context.Context) (map[string]interface{}, error) {
	return s.store.GetDatabaseStatistics(ctx)
}

func (s *Service) TruncateTable(ctx context.Context, tableName string) error {
	return s.store.TruncateTable(ctx, tableName)
}

func (s *Service) ClearImportData(ctx context.Context) (int, error) {
	return s.store.ClearImportData(ctx)
}

// Visit services

func (s *Service) ListVisits(ctx context.Context, tenantID *int64, page, pageSize int) ([]map[string]interface{}, int64, error) {
	return s.store.ListVisits(ctx, tenantID, page, pageSize)
}

func (s *Service) GetVisitStatistics(ctx context.Context, tenantID *int64) (map[string]int64, error) {
	return s.store.GetVisitStatistics(ctx, tenantID)
}

func (s *Service) GetVisitFilters(ctx context.Context, tenantID *int64) (map[string]interface{}, error) {
	return s.store.GetVisitFilters(ctx, tenantID)
}

func (s *Service) GetVisitByID(ctx context.Context, id int64) (map[string]interface{}, error) {
	return s.store.GetVisitByID(ctx, id)
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
		ID:           c.ID,
		TenantID:     c.TenantID,
		ModelCode:    c.ModelCode,
		FunctionType: c.FunctionType,
		ModelName:    c.ModelName,
		Provider:     c.Provider,
		APIEndpoint:  c.APIEndpoint,
		ModelParams:  c.ModelParams,
		IsDefault:    c.IsDefault,
		IsActive:     c.IsActive,
		Description:  c.Description,
		CreatedAt:    c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func boolToStatus(success bool) string {
	if success {
		return "success"
	}
	return "failed"
}

func normalizeDateRange(startDate, endDate *string) (string, string) {
	if startDate != nil && endDate != nil {
		return *startDate, *endDate
	}
	now := time.Now()
	end := now.Format("2006-01-02")
	start := now.AddDate(0, 0, -30).Format("2006-01-02")
	if startDate != nil {
		start = *startDate
	}
	if endDate != nil {
		end = *endDate
	}
	return start, end
}

func toCostBreakdown(items []llmgateway.AuditCostByGroupItem) []CostBreakdownItem {
	out := make([]CostBreakdownItem, 0, len(items))
	for _, item := range items {
		out = append(out, CostBreakdownItem{
			Key:         item.Key,
			CallCount:   item.CallCount,
			TotalTokens: item.TotalTokens,
			TotalCost:   item.TotalCost,
		})
	}
	return out
}

func toCostTrend(items []llmgateway.AuditCostByGroupItem) []CostTrendItem {
	out := make([]CostTrendItem, 0, len(items))
	for _, item := range items {
		out = append(out, CostTrendItem{
			Date:        item.Key,
			CallCount:   item.CallCount,
			TotalTokens: item.TotalTokens,
			TotalCost:   item.TotalCost,
		})
	}
	return out
}

func normalizeMetadataFieldType(dataType string) string {
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "smallint", "integer", "bigint", "numeric", "decimal", "real", "double precision":
		return "number"
	case "boolean":
		return "boolean"
	case "date", "timestamp without time zone", "timestamp with time zone", "time without time zone", "time with time zone":
		return "date"
	case "json", "jsonb":
		return "object"
	case "array":
		return "array"
	default:
		return "string"
	}
}

func setToSortedSlice(input map[string]struct{}) []string {
	if len(input) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(input))
	for item := range input {
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

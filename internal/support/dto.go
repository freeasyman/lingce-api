package support

// Notification DTOs

// RegisterDeviceTokenRequest represents the request for registering device token
type RegisterDeviceTokenRequest struct {
	Token       string  `json:"token"`
	Platform    string  `json:"platform"` // "ios", "android"
	DeviceModel *string `json:"device_model,omitempty"`
	AppVersion  *string `json:"app_version,omitempty"`
}

// UnregisterDeviceTokenRequest represents the request for unregistering device token
type UnregisterDeviceTokenRequest struct {
	Token string `json:"token"`
}

// PushNotificationRequest represents the request for pushing notification
type PushNotificationRequest struct {
	UserIDs     []int64    `json:"user_ids"`
	UserType    string     `json:"user_type"` // "admin", "employee"
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Type        string     `json:"type"`
	RelatedID   *int64     `json:"related_id,omitempty"`
	RelatedType *string    `json:"related_type,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
}

// NotificationListRequest represents the request for listing notifications
type NotificationListRequest struct {
	UserID   int64   `json:"user_id"`
	UserType string  `json:"user_type"`
	Type     *string `json:"type,omitempty"`
	IsRead   *bool   `json:"is_read,omitempty"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

// NotificationResponse represents a notification response
type NotificationResponse struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Type        string     `json:"type"`
	RelatedID   *int64     `json:"related_id,omitempty"`
	RelatedType *string    `json:"related_type,omitempty"`
	IsRead      bool       `json:"is_read"`
	ReadAt      *string    `json:"read_at,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedAt   string     `json:"created_at"`
}

// UnreadCountResponse represents the unread notification count
type UnreadCountResponse struct {
	UnreadCount int64 `json:"unread_count"`
}

// Operation Log DTOs

// OperationLogListRequest represents the request for listing operation logs
type OperationLogListRequest struct {
	TenantID     *int64  `json:"tenant_id,omitempty"`
	UserID       *int64  `json:"user_id,omitempty"`
	Action       *string `json:"action,omitempty"`
	Resource     *string `json:"resource,omitempty"`
	StartDate    *string `json:"start_date,omitempty"`
	EndDate      *string `json:"end_date,omitempty"`
	Page         int     `json:"page"`
	PageSize     int     `json:"page_size"`
}

// OperationLogResponse represents an operation log response
type OperationLogResponse struct {
	ID           int64   `json:"id"`
	TenantID     *int64  `json:"tenant_id,omitempty"`
	UserID       *int64  `json:"user_id,omitempty"`
	Username     *string `json:"username,omitempty"`
	Action       string  `json:"action"`
	Resource     string  `json:"resource"`
	ResourceID   *int64  `json:"resource_id,omitempty"`
	Method       string  `json:"method"`
	Path         string  `json:"path"`
	IPAddress    *string `json:"ip_address,omitempty"`
	ResponseCode int     `json:"response_code"`
	ErrorMessage *string `json:"error_message,omitempty"`
	Duration     int     `json:"duration"`
	CreatedAt    string  `json:"created_at"`
}

// OperationLogStatsResponse represents operation log statistics
type OperationLogStatsResponse struct {
	TotalLogs       int64              `json:"total_logs"`
	SuccessCount    int64              `json:"success_count"`
	FailureCount    int64              `json:"failure_count"`
	AvgDuration     float64            `json:"avg_duration"`
	TopActions      []ActionCount      `json:"top_actions"`
	TopResources    []ResourceCount    `json:"top_resources"`
	TopUsers        []UserActivityCount `json:"top_users"`
}

// ActionCount represents action count
type ActionCount struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

// ResourceCount represents resource count
type ResourceCount struct {
	Resource string `json:"resource"`
	Count    int64  `json:"count"`
}

// UserActivityCount represents user activity count
type UserActivityCount struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Count    int64  `json:"count"`
}

// LLM Model Config DTOs

// LLMModelConfigListRequest represents the request for listing LLM model configs
type LLMModelConfigListRequest struct {
	Provider *string `json:"provider,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

// CreateLLMModelConfigRequest represents the request for creating LLM model config
type CreateLLMModelConfigRequest struct {
	ModelName   string     `json:"model_name"`
	Provider    string     `json:"provider"`
	APIEndpoint string     `json:"api_endpoint"`
	APIKey      string     `json:"api_key"`
	ModelParams JSONObject `json:"model_params,omitempty"`
	IsDefault   bool       `json:"is_default"`
	IsActive    bool       `json:"is_active"`
	Description *string    `json:"description,omitempty"`
}

// UpdateLLMModelConfigRequest represents the request for updating LLM model config
type UpdateLLMModelConfigRequest struct {
	ModelName   *string    `json:"model_name,omitempty"`
	APIEndpoint *string    `json:"api_endpoint,omitempty"`
	APIKey      *string    `json:"api_key,omitempty"`
	ModelParams JSONObject `json:"model_params,omitempty"`
	IsActive    *bool      `json:"is_active,omitempty"`
	Description *string    `json:"description,omitempty"`
}

// LLMModelConfigResponse represents an LLM model config response
type LLMModelConfigResponse struct {
	ID          int64      `json:"id"`
	ModelName   string     `json:"model_name"`
	Provider    string     `json:"provider"`
	APIEndpoint string     `json:"api_endpoint"`
	ModelParams JSONObject `json:"model_params,omitempty"`
	IsDefault   bool       `json:"is_default"`
	IsActive    bool       `json:"is_active"`
	Description *string    `json:"description,omitempty"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

// LLM Call Record DTOs

// LLMCallRecordListRequest represents the request for listing LLM call records
type LLMCallRecordListRequest struct {
	TenantID      *int64  `json:"tenant_id,omitempty"`
	UserID        *int64  `json:"user_id,omitempty"`
	ModelConfigID *int64  `json:"model_config_id,omitempty"`
	Provider      *string `json:"provider,omitempty"`
	Status        *string `json:"status,omitempty"`
	Purpose       *string `json:"purpose,omitempty"`
	StartDate     *string `json:"start_date,omitempty"`
	EndDate       *string `json:"end_date,omitempty"`
	Page          int     `json:"page"`
	PageSize      int     `json:"page_size"`
}

// LLMCallRecordResponse represents an LLM call record response
type LLMCallRecordResponse struct {
	ID               int64   `json:"id"`
	TenantID         *int64  `json:"tenant_id,omitempty"`
	UserID           *int64  `json:"user_id,omitempty"`
	ModelName        string  `json:"model_name"`
	Provider         string  `json:"provider"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	Duration         int     `json:"duration"`
	Status           string  `json:"status"`
	ErrorMessage     *string `json:"error_message,omitempty"`
	Purpose          *string `json:"purpose,omitempty"`
	RelatedID        *int64  `json:"related_id,omitempty"`
	RelatedType      *string `json:"related_type,omitempty"`
	CreatedAt        string  `json:"created_at"`
}

// LLMCallRecordStatsResponse represents LLM call record statistics
type LLMCallRecordStatsResponse struct {
	TotalCalls       int64              `json:"total_calls"`
	SuccessCalls     int64              `json:"success_calls"`
	FailedCalls      int64              `json:"failed_calls"`
	TotalTokens      int64              `json:"total_tokens"`
	TotalCost        float64            `json:"total_cost"`
	AvgDuration      float64            `json:"avg_duration"`
	TopModels        []ModelUsageCount  `json:"top_models"`
	TopPurposes      []PurposeCount     `json:"top_purposes"`
}

// ModelUsageCount represents model usage count
type ModelUsageCount struct {
	ModelName string  `json:"model_name"`
	Count     int64   `json:"count"`
	TotalCost float64 `json:"total_cost"`
}

// PurposeCount represents purpose count
type PurposeCount struct {
	Purpose string `json:"purpose"`
	Count   int64  `json:"count"`
}

// LLM Cost DTOs

// LLMCostTenantResponse represents tenant LLM cost
type LLMCostTenantResponse struct {
	TenantID    int64              `json:"tenant_id"`
	TenantName  string             `json:"tenant_name"`
	TotalCost   float64            `json:"total_cost"`
	TotalCalls  int64              `json:"total_calls"`
	TotalTokens int64              `json:"total_tokens"`
	ByModel     []ModelCostSummary `json:"by_model"`
	ByPurpose   []PurposeCostSummary `json:"by_purpose"`
}

// ModelCostSummary represents model cost summary
type ModelCostSummary struct {
	ModelName string  `json:"model_name"`
	Cost      float64 `json:"cost"`
	Calls     int64   `json:"calls"`
	Tokens    int64   `json:"tokens"`
}

// PurposeCostSummary represents purpose cost summary
type PurposeCostSummary struct {
	Purpose string  `json:"purpose"`
	Cost    float64 `json:"cost"`
	Calls   int64   `json:"calls"`
}

// LLMCostSummaryResponse represents overall LLM cost summary
type LLMCostSummaryResponse struct {
	TotalCost      float64              `json:"total_cost"`
	TotalCalls     int64                `json:"total_calls"`
	TotalTokens    int64                `json:"total_tokens"`
	ByTenant       []TenantCostSummary  `json:"by_tenant"`
	ByModel        []ModelCostSummary   `json:"by_model"`
	ByProvider     []ProviderCostSummary `json:"by_provider"`
}

// TenantCostSummary represents tenant cost summary
type TenantCostSummary struct {
	TenantID   int64   `json:"tenant_id"`
	TenantName string  `json:"tenant_name"`
	Cost       float64 `json:"cost"`
	Calls      int64   `json:"calls"`
}

// ProviderCostSummary represents provider cost summary
type ProviderCostSummary struct {
	Provider string  `json:"provider"`
	Cost     float64 `json:"cost"`
	Calls    int64   `json:"calls"`
}

// Metadata DTOs

// MetadataFieldsResponse represents metadata fields response
type MetadataFieldsResponse struct {
	Fields []MetadataField `json:"fields"`
}

// ValidateTemplateRequest represents the request for validating template
type ValidateTemplateRequest struct {
	Template string `json:"template"`
}

// ValidateTemplateResponse represents the response for validating template
type ValidateTemplateResponse struct {
	IsValid      bool     `json:"is_valid"`
	Errors       []string `json:"errors,omitempty"`
	UsedFields   []string `json:"used_fields,omitempty"`
	MissingFields []string `json:"missing_fields,omitempty"`
}

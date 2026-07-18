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
	TenantID  *int64  `json:"tenant_id,omitempty"`
	UserID    *int64  `json:"user_id,omitempty"`
	Action    *string `json:"action,omitempty"`
	Resource  *string `json:"resource,omitempty"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
	Page      int     `json:"page"`
	PageSize  int     `json:"page_size"`
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
	TotalLogs    int64               `json:"total_logs"`
	SuccessCount int64               `json:"success_count"`
	FailureCount int64               `json:"failure_count"`
	AvgDuration  float64             `json:"avg_duration"`
	TopActions   []ActionCount       `json:"top_actions"`
	TopResources []ResourceCount     `json:"top_resources"`
	TopUsers     []UserActivityCount `json:"top_users"`
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
	TenantID     *int64  `json:"tenant_id,omitempty"`
	FunctionType *string `json:"function_type,omitempty"`
	Provider     *string `json:"provider,omitempty"`
	IsActive     *bool   `json:"is_active,omitempty"`
	Page         int     `json:"page"`
	PageSize     int     `json:"page_size"`
}

// CreateLLMModelConfigRequest represents the request for creating LLM model config
type CreateLLMModelConfigRequest struct {
	TenantID         *int64     `json:"tenant_id,omitempty"`
	ModelCode        string     `json:"model_code,omitempty"`
	FunctionType     string     `json:"function_type,omitempty"`
	ModelName        string     `json:"model_name"`
	Provider         string     `json:"provider"`
	APIEndpoint      string     `json:"api_endpoint"`
	APIBaseURL       *string    `json:"api_base_url,omitempty"`
	APIKey           string     `json:"api_key"`
	ModelParams      JSONObject `json:"model_params,omitempty"`
	ExtraParams      JSONObject `json:"extra_params,omitempty"`
	InputTokenPrice  *float64   `json:"input_token_price,omitempty"`
	OutputTokenPrice *float64   `json:"output_token_price,omitempty"`
	DailyLimit       *int       `json:"daily_limit,omitempty"`
	MonthlyLimit     *int       `json:"monthly_limit,omitempty"`
	IsDefault        bool       `json:"is_default"`
	IsActive         bool       `json:"is_active"`
	Description      *string    `json:"description,omitempty"`
}

// UpdateLLMModelConfigRequest represents the request for updating LLM model config
type UpdateLLMModelConfigRequest struct {
	TenantID         *int64     `json:"tenant_id,omitempty"`
	ModelCode        *string    `json:"model_code,omitempty"`
	FunctionType     *string    `json:"function_type,omitempty"`
	ModelName        *string    `json:"model_name,omitempty"`
	Provider         *string    `json:"provider,omitempty"`
	APIEndpoint      *string    `json:"api_endpoint,omitempty"`
	APIBaseURL       *string    `json:"api_base_url,omitempty"`
	APIKey           *string    `json:"api_key,omitempty"`
	ModelParams      JSONObject `json:"model_params,omitempty"`
	ExtraParams      JSONObject `json:"extra_params,omitempty"`
	InputTokenPrice  *float64   `json:"input_token_price,omitempty"`
	OutputTokenPrice *float64   `json:"output_token_price,omitempty"`
	DailyLimit       *int       `json:"daily_limit,omitempty"`
	MonthlyLimit     *int       `json:"monthly_limit,omitempty"`
	IsDefault        *bool      `json:"is_default,omitempty"`
	IsActive         *bool      `json:"is_active,omitempty"`
	Description      *string    `json:"description,omitempty"`
}

// LLMModelConfigResponse represents an LLM model config response
type LLMModelConfigResponse struct {
	ID               int64      `json:"id"`
	TenantID         int64      `json:"tenant_id"`
	TenantName       *string    `json:"tenant_name,omitempty"`
	ModelCode        string     `json:"model_code"`
	FunctionType     string     `json:"function_type"`
	ModelName        string     `json:"model_name"`
	Provider         string     `json:"provider"`
	APIEndpoint      string     `json:"api_endpoint"`
	APIBaseURL       *string    `json:"api_base_url,omitempty"`
	ModelParams      JSONObject `json:"model_params,omitempty"`
	ExtraParams      JSONObject `json:"extra_params,omitempty"`
	InputTokenPrice  *float64   `json:"input_token_price,omitempty"`
	OutputTokenPrice *float64   `json:"output_token_price,omitempty"`
	DailyLimit       *int       `json:"daily_limit,omitempty"`
	MonthlyLimit     *int       `json:"monthly_limit,omitempty"`
	IsDefault        bool       `json:"is_default"`
	IsActive         bool       `json:"is_active"`
	Description      *string    `json:"description,omitempty"`
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
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
	FunctionType  *string `json:"function_type,omitempty"`
	Module        *string `json:"module,omitempty"`
	ModelCode     *string `json:"model_code,omitempty"`
	Success       *bool   `json:"success,omitempty"`
	TraceID       *string `json:"trace_id,omitempty"`
	StartDate     *string `json:"start_date,omitempty"`
	EndDate       *string `json:"end_date,omitempty"`
	Page          int     `json:"page"`
	PageSize      int     `json:"page_size"`
	Size          int     `json:"size"`
}

// AIUsageListRequest represents unified AI usage query filters.
type AIUsageListRequest struct {
	TenantID           *int64  `json:"tenant_id,omitempty"`
	BusinessDomain     *string `json:"business_domain,omitempty"`
	BusinessObjectType *string `json:"business_object_type,omitempty"`
	BusinessObjectID   *int64  `json:"business_object_id,omitempty"`
	BillingSubject     *string `json:"billing_subject,omitempty"`
	BillingScene       *string `json:"billing_scene,omitempty"`
	RecordingID        *int64  `json:"recording_id,omitempty"`
	ContentID          *int64  `json:"content_id,omitempty"`
	GenerationTaskID   *int64  `json:"generation_task_id,omitempty"`
	Provider           *string `json:"provider,omitempty"`
	ModelCode          *string `json:"model_code,omitempty"`
	Success            *bool   `json:"success,omitempty"`
	StartDate          *string `json:"start_date,omitempty"`
	EndDate            *string `json:"end_date,omitempty"`
	Page               int     `json:"page"`
	PageSize           int     `json:"page_size"`
}

// AIUsageRecordItem represents a single AI usage record.
type AIUsageRecordItem struct {
	ID                   int64   `json:"id"`
	RequestID            string  `json:"request_id"`
	TraceID              string  `json:"trace_id"`
	TenantID             int64   `json:"tenant_id"`
	TenantName           string  `json:"tenant_name,omitempty"`
	BusinessDomain       string  `json:"business_domain,omitempty"`
	BusinessObjectType   string  `json:"business_object_type,omitempty"`
	BusinessObjectID     int64   `json:"business_object_id,omitempty"`
	BillingSubject       string  `json:"billing_subject,omitempty"`
	BillingScene         string  `json:"billing_scene,omitempty"`
	BillingRuleVersion   string  `json:"billing_rule_version,omitempty"`
	RecordingID          int64   `json:"recording_id,omitempty"`
	ContentID            int64   `json:"content_id,omitempty"`
	TopicID              int64   `json:"topic_id,omitempty"`
	CustomerID           int64   `json:"customer_id,omitempty"`
	AnalysisRunID        int64   `json:"analysis_run_id,omitempty"`
	AnalysisStepRunID    int64   `json:"analysis_step_run_id,omitempty"`
	GenerationTaskID     int64   `json:"generation_task_id,omitempty"`
	FunctionType         string  `json:"function_type"`
	Module               string  `json:"module"`
	Provider             string  `json:"provider"`
	ModelCode            string  `json:"model_code"`
	Success              bool    `json:"success"`
	InputTokens          int64   `json:"input_tokens"`
	OutputTokens         int64   `json:"output_tokens"`
	TotalTokens          int64   `json:"total_tokens"`
	InputCost            float64 `json:"input_cost"`
	OutputCost           float64 `json:"output_cost"`
	TotalCost            float64 `json:"total_cost"`
	AudioDurationSeconds float64 `json:"audio_duration_seconds,omitempty"`
	LatencyMS            int64   `json:"latency_ms"`
	CreatedAt            string  `json:"created_at"`
}

// AIUsageGroupItem represents grouped AI usage data.
type AIUsageGroupItem struct {
	Key               string  `json:"key"`
	TenantID          *int64  `json:"tenant_id,omitempty"`
	TenantName        *string `json:"tenant_name,omitempty"`
	CallCount         int64   `json:"call_count"`
	TotalTokens       int64   `json:"total_tokens"`
	TotalAudioSeconds float64 `json:"total_audio_seconds"`
	TotalCostCNY      float64 `json:"total_cost_cny"`
	EstimatedPoints   int64   `json:"estimated_points"`
}

// AIUsageTrendItem represents daily usage trend.
type AIUsageTrendItem struct {
	Date              string  `json:"date"`
	CallCount         int64   `json:"call_count"`
	TotalTokens       int64   `json:"total_tokens"`
	TotalAudioSeconds float64 `json:"total_audio_seconds"`
	TotalCostCNY      float64 `json:"total_cost_cny"`
	EstimatedPoints   int64   `json:"estimated_points"`
}

// AIUsageSummaryResponse represents AI usage summary.
type AIUsageSummaryResponse struct {
	TotalCalls               int64              `json:"total_calls"`
	SuccessCalls             int64              `json:"success_calls"`
	FailedCalls              int64              `json:"failed_calls"`
	TotalTokens              int64              `json:"total_tokens"`
	TotalInputTokens         int64              `json:"total_input_tokens"`
	TotalOutputTokens        int64              `json:"total_output_tokens"`
	TotalAudioSeconds        float64            `json:"total_audio_seconds"`
	TotalCostCNY             float64            `json:"total_cost_cny"`
	EstimatedPoints          int64              `json:"estimated_points"`
	AvgCostPerCall           float64            `json:"avg_cost_per_call"`
	AvgCostPerBusinessObject float64            `json:"avg_cost_per_business_object"`
	ByTenant                 []AIUsageGroupItem `json:"by_tenant"`
	ByBusinessDomain         []AIUsageGroupItem `json:"by_business_domain"`
	ByBillingSubject         []AIUsageGroupItem `json:"by_billing_subject"`
	ByModel                  []AIUsageGroupItem `json:"by_model"`
	Trend                    []AIUsageTrendItem `json:"trend"`
}

// AIUsageObjectResponse represents business object usage detail.
type AIUsageObjectResponse struct {
	BusinessDomain     string              `json:"business_domain,omitempty"`
	BusinessObjectType string              `json:"business_object_type,omitempty"`
	BusinessObjectID   int64               `json:"business_object_id,omitempty"`
	BusinessObjectName string              `json:"business_object_name,omitempty"`
	TenantID           int64               `json:"tenant_id"`
	TenantName         string              `json:"tenant_name,omitempty"`
	BillingSubject     string              `json:"billing_subject,omitempty"`
	BillingScene       string              `json:"billing_scene,omitempty"`
	TotalCalls         int64               `json:"total_calls"`
	SuccessCalls       int64               `json:"success_calls"`
	FailedCalls        int64               `json:"failed_calls"`
	TotalTokens        int64               `json:"total_tokens"`
	TotalAudioSeconds  float64             `json:"total_audio_seconds"`
	TotalCostCNY       float64             `json:"total_cost_cny"`
	EstimatedPoints    int64               `json:"estimated_points"`
	FirstCallAt        string              `json:"first_call_at,omitempty"`
	LastCallAt         string              `json:"last_call_at,omitempty"`
	Records            []AIUsageRecordItem `json:"records"`
}

// LLMCallRecordResponse represents an LLM call record response
type LLMCallRecordResponse struct {
	ID                 int64    `json:"id"`
	RequestID          string   `json:"request_id"`
	TenantID           *int64   `json:"tenant_id,omitempty"`
	TenantName         *string  `json:"tenant_name,omitempty"`
	UserID             *int64   `json:"user_id,omitempty"`
	BusinessDomain     *string  `json:"business_domain,omitempty"`
	BusinessObjectType *string  `json:"business_object_type,omitempty"`
	BusinessObjectID   *int64   `json:"business_object_id,omitempty"`
	BillingSubject     *string  `json:"billing_subject,omitempty"`
	BillingScene       *string  `json:"billing_scene,omitempty"`
	BillingRuleVersion *string  `json:"billing_rule_version,omitempty"`
	RecordingID        *int64   `json:"recording_id,omitempty"`
	ContentID          *int64   `json:"content_id,omitempty"`
	TopicID            *int64   `json:"topic_id,omitempty"`
	CustomerID         *int64   `json:"customer_id,omitempty"`
	AnalysisRunID      *int64   `json:"analysis_run_id,omitempty"`
	AnalysisStepRunID  *int64   `json:"analysis_step_run_id,omitempty"`
	GenerationTaskID   *int64   `json:"generation_task_id,omitempty"`
	ModelName          string   `json:"model_name"`
	FunctionType       *string  `json:"function_type,omitempty"`
	Module             *string  `json:"module,omitempty"`
	TraceID            *string  `json:"trace_id,omitempty"`
	ModelCode          *string  `json:"model_code,omitempty"`
	Provider           string   `json:"provider"`
	PromptTokens       int      `json:"prompt_tokens"`
	CompletionTokens   int      `json:"completion_tokens"`
	TotalTokens        int      `json:"total_tokens"`
	Cost               float64  `json:"cost"`
	Duration           int      `json:"duration"`
	Status             string   `json:"status"`
	Success            *bool    `json:"success,omitempty"`
	InputCost          *float64 `json:"input_cost,omitempty"`
	OutputCost         *float64 `json:"output_cost,omitempty"`
	ErrorMessage       *string  `json:"error_message,omitempty"`
	Purpose            *string  `json:"purpose,omitempty"`
	RelatedID          *int64   `json:"related_id,omitempty"`
	RelatedType        *string  `json:"related_type,omitempty"`
	CreatedAt          string   `json:"created_at"`
}

// LLMCallRecordStatsResponse represents LLM call record statistics
type LLMCallRecordStatsResponse struct {
	TotalCalls        int64             `json:"total_calls"`
	SuccessCalls      int64             `json:"success_calls,omitempty"`
	FailedCalls       int64             `json:"failed_calls,omitempty"`
	SuccessCount      int64             `json:"success_count"`
	FailureCount      int64             `json:"failure_count"`
	SuccessRate       float64           `json:"success_rate"`
	TotalTokens       int64             `json:"total_tokens"`
	TotalInputTokens  int64             `json:"total_input_tokens"`
	TotalOutputTokens int64             `json:"total_output_tokens"`
	TotalCost         float64           `json:"total_cost"`
	AvgDuration       float64           `json:"avg_duration,omitempty"`
	AvgLatencyMS      float64           `json:"avg_latency_ms"`
	TopModels         []ModelUsageCount `json:"top_models,omitempty"`
	TopPurposes       []PurposeCount    `json:"top_purposes,omitempty"`
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
	TenantID           int64               `json:"tenant_id"`
	TenantName         string              `json:"tenant_name"`
	TotalCost          float64             `json:"total_cost"`
	TotalCalls         int64               `json:"total_calls"`
	TotalTokens        int64               `json:"total_tokens"`
	CostByFunctionType []CostBreakdownItem `json:"cost_by_function_type"`
	CostByModule       []CostBreakdownItem `json:"cost_by_module"`
	CostByModel        []CostBreakdownItem `json:"cost_by_model"`
	CostTrend          []CostTrendItem     `json:"cost_trend"`
}

// CostBreakdownItem represents cost breakdown item.
type CostBreakdownItem struct {
	Key         string  `json:"key"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

// CostTrendItem represents trend item.
type CostTrendItem struct {
	Date        string  `json:"date"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

// LLMCostSummaryResponse represents overall LLM cost summary
type LLMCostSummaryResponse struct {
	TotalCost          float64             `json:"total_cost"`
	TotalCalls         int64               `json:"total_calls"`
	TotalTokens        int64               `json:"total_tokens"`
	CostByTenant       []TenantCostSummary `json:"cost_by_tenant"`
	CostByFunctionType []CostBreakdownItem `json:"cost_by_function_type"`
	CostByModule       []CostBreakdownItem `json:"cost_by_module"`
	CostByModel        []CostBreakdownItem `json:"cost_by_model"`
}

// TenantCostSummary represents tenant cost summary
type TenantCostSummary struct {
	TenantID    int64   `json:"tenant_id"`
	TenantName  string  `json:"tenant_name"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
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
	IsValid       bool     `json:"is_valid"`
	Errors        []string `json:"errors,omitempty"`
	UsedFields    []string `json:"used_fields,omitempty"`
	MissingFields []string `json:"missing_fields,omitempty"`
}

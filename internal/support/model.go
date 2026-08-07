package support

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// Notification represents a notification
type Notification struct {
	ID          int64      `json:"id"`
	TenantID    *int64     `json:"tenant_id,omitempty"`
	UserID      int64      `json:"user_id"`
	UserType    string     `json:"user_type"` // "admin", "employee"
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	Type        string     `json:"type"` // "system", "task", "recording", "badge"
	RelatedID   *int64     `json:"related_id,omitempty"`
	RelatedType *string    `json:"related_type,omitempty"`
	IsRead      bool       `json:"is_read"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	ExtraData   JSONObject `json:"extra_data,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// DeviceToken represents a push notification device token
type DeviceToken struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	UserType    string    `json:"user_type"` // "admin", "employee"
	Token       string    `json:"token"`
	Platform    string    `json:"platform"` // "ios", "android"
	DeviceModel *string   `json:"device_model,omitempty"`
	AppVersion  *string   `json:"app_version,omitempty"`
	IsActive    bool      `json:"is_active"`
	LastUsedAt  time.Time `json:"last_used_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// OperationLog represents an operation log entry
type OperationLog struct {
	ID           int64     `json:"id"`
	TenantID     *int64    `json:"tenant_id,omitempty"`
	UserID       *int64    `json:"user_id,omitempty"`
	UserType     *string   `json:"user_type,omitempty"`
	Username     *string   `json:"username,omitempty"`
	Action       string    `json:"action"`
	Resource     string    `json:"resource"`
	ResourceID   *int64    `json:"resource_id,omitempty"`
	Method       string    `json:"method"` // HTTP method
	Path         string    `json:"path"`
	IPAddress    *string   `json:"ip_address,omitempty"`
	UserAgent    *string   `json:"user_agent,omitempty"`
	RequestBody  *string   `json:"request_body,omitempty"`
	ResponseCode int       `json:"response_code"`
	ErrorMessage *string   `json:"error_message,omitempty"`
	Duration     int       `json:"duration"` // milliseconds
	CreatedAt    time.Time `json:"created_at"`
}

// SystemActionLog represents an institution-side system action log entry.
type SystemActionLog struct {
	ID             int64      `json:"id"`
	TenantID       *int64     `json:"tenant_id,omitempty"`
	TenantName     *string    `json:"tenant_name,omitempty"`
	ActorID        *int64     `json:"actor_id,omitempty"`
	ActorName      *string    `json:"actor_name,omitempty"`
	ActorRoleCode  *string    `json:"actor_role_code,omitempty"`
	ActorRoleName  *string    `json:"actor_role_name,omitempty"`
	LogType        string     `json:"log_type"`
	ActionCode     string     `json:"action_code"`
	ActionName     string     `json:"action_name"`
	RoutePath      *string    `json:"route_path,omitempty"`
	Result         string     `json:"result"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	ObjectType     *string    `json:"object_type,omitempty"`
	ObjectID       *string    `json:"object_id,omitempty"`
	ObjectName     *string    `json:"object_name,omitempty"`
	RequestSummary JSONObject `json:"request_summary,omitempty"`
	BeforeSummary  JSONObject `json:"before_summary,omitempty"`
	AfterSummary   JSONObject `json:"after_summary,omitempty"`
	IPAddress      *string    `json:"ip_address,omitempty"`
	UserAgent      *string    `json:"user_agent,omitempty"`
	DeviceType     *string    `json:"device_type,omitempty"`
	TraceID        *string    `json:"trace_id,omitempty"`
	RequestID      *string    `json:"request_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// LLMModelConfig represents an LLM model configuration
type LLMModelConfig struct {
	ID               int64      `json:"id"`
	TenantID         int64      `json:"tenant_id"`
	ModelCode        string     `json:"model_code"`
	FunctionType     string     `json:"function_type"`
	ModelName        string     `json:"model_name"`
	Provider         string     `json:"provider"` // "openai", "anthropic", "azure", etc.
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
	CreatedBy        int64      `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

// LLMCallRecord represents an LLM API call record
type LLMCallRecord struct {
	ID               int64     `json:"id"`
	TenantID         *int64    `json:"tenant_id,omitempty"`
	UserID           *int64    `json:"user_id,omitempty"`
	ModelConfigID    int64     `json:"model_config_id"`
	ModelName        string    `json:"model_name"`
	Provider         string    `json:"provider"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	Cost             float64   `json:"cost"`     // in USD
	Duration         int       `json:"duration"` // milliseconds
	Status           string    `json:"status"`   // "success", "failed"
	ErrorMessage     *string   `json:"error_message,omitempty"`
	RequestPayload   *string   `json:"request_payload,omitempty"`
	ResponsePayload  *string   `json:"response_payload,omitempty"`
	Purpose          *string   `json:"purpose,omitempty"` // "transcribe", "analyze", "generate", etc.
	RelatedID        *int64    `json:"related_id,omitempty"`
	RelatedType      *string   `json:"related_type,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// MetadataField represents a metadata field definition
type MetadataField struct {
	FieldName    string  `json:"field_name"`
	FieldType    string  `json:"field_type"` // "string", "number", "boolean", "date", "array", "object"
	Description  string  `json:"description"`
	TableName    string  `json:"table_name"`
	IsRequired   bool    `json:"is_required"`
	DefaultValue *string `json:"default_value,omitempty"`
}

// JSONObject is a custom type for JSON objects stored in database
type JSONObject map[string]interface{}

// Scan implements the sql.Scanner interface
func (j *JSONObject) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}

	return json.Unmarshal(bytes, j)
}

// Value implements the driver.Valuer interface
func (j JSONObject) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

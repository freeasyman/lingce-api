package support

// SystemActionLogWriteRequest represents a system action log write request.
type SystemActionLogWriteRequest struct {
	TenantID      *int64    `json:"tenant_id,omitempty"`
	TenantName    *string   `json:"tenant_name,omitempty"`
	ActorID       *int64    `json:"actor_id,omitempty"`
	ActorName     *string   `json:"actor_name,omitempty"`
	ActorRoleCode *string   `json:"actor_role_code,omitempty"`
	ActorRoleName *string   `json:"actor_role_name,omitempty"`
	LogType       string    `json:"log_type"`
	ActionCode    string    `json:"action_code"`
	ActionName    string    `json:"action_name"`
	RoutePath     *string   `json:"route_path,omitempty"`
	Result        string    `json:"result,omitempty"`
	ErrorMessage  *string   `json:"error_message,omitempty"`
	ObjectType    *string   `json:"object_type,omitempty"`
	ObjectID      any       `json:"object_id,omitempty"`
	ObjectName    *string   `json:"object_name,omitempty"`
	RequestSummary JSONObject `json:"request_summary,omitempty"`
	BeforeSummary  JSONObject `json:"before_summary,omitempty"`
	AfterSummary   JSONObject `json:"after_summary,omitempty"`
	TraceID       *string   `json:"trace_id,omitempty"`
	RequestID     *string   `json:"request_id,omitempty"`
}

// SystemActionLogListRequest represents the request for listing system action logs.
type SystemActionLogListRequest struct {
	TenantID    *int64  `json:"tenant_id,omitempty"`
	ActorID     *int64  `json:"actor_id,omitempty"`
	Keyword     *string `json:"keyword,omitempty"`
	LogType     *string `json:"log_type,omitempty"`
	ActionCode  *string `json:"action_code,omitempty"`
	Result      *string `json:"result,omitempty"`
	IPAddress   *string `json:"ip,omitempty"`
	DeviceType  *string `json:"device_type,omitempty"`
	StartDate   *string `json:"start_date,omitempty"`
	EndDate     *string `json:"end_date,omitempty"`
	Page        int     `json:"page"`
	PageSize    int     `json:"page_size"`
}

// SystemActionLogResponse represents a system action log response.
type SystemActionLogResponse struct {
	ID            int64      `json:"id"`
	TenantID      *int64     `json:"tenant_id,omitempty"`
	TenantName    *string    `json:"tenant_name,omitempty"`
	ActorID       *int64     `json:"actor_id,omitempty"`
	ActorName     *string    `json:"actor_name,omitempty"`
	ActorRoleCode *string    `json:"actor_role_code,omitempty"`
	ActorRoleName *string    `json:"actor_role_name,omitempty"`
	LogType       string     `json:"log_type"`
	ActionCode    string     `json:"action_code"`
	ActionName    string     `json:"action_name"`
	RoutePath     *string    `json:"route_path,omitempty"`
	Result        string     `json:"result"`
	Summary       string     `json:"summary"`
	ErrorMessage  *string    `json:"error_message,omitempty"`
	IPAddress     *string    `json:"ip,omitempty"`
	UserAgent     *string    `json:"user_agent,omitempty"`
	DeviceType    *string    `json:"device_type,omitempty"`
	ObjectType    *string    `json:"object_type,omitempty"`
	ObjectID      *string    `json:"object_id,omitempty"`
	ObjectName    *string    `json:"object_name,omitempty"`
	RequestSummary JSONObject `json:"request_summary,omitempty"`
	BeforeSummary  JSONObject `json:"before_summary,omitempty"`
	AfterSummary   JSONObject `json:"after_summary,omitempty"`
	TraceID       *string    `json:"trace_id,omitempty"`
	RequestID     *string    `json:"request_id,omitempty"`
	CreatedAt     string     `json:"created_at"`
}

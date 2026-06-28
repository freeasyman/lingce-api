package customer

// Customer Management DTOs

// CustomerListRequest represents the request for listing customers
type CustomerListRequest struct {
	TenantID    *int64   `json:"tenant_id,omitempty"`
	TenantIDs   []int64  `json:"tenant_ids,omitempty"`
	Search      *string  `json:"search,omitempty"`
	Name        *string  `json:"name,omitempty"`
	Phone       *string  `json:"phone,omitempty"`
	Status      *string  `json:"status,omitempty"`
	Source      *string  `json:"source,omitempty"`
	AssignedTo  *int64   `json:"assigned_to,omitempty"`
	MinMomentum *int     `json:"min_momentum,omitempty"`
	MaxMomentum *int     `json:"max_momentum,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	StartDate   *string  `json:"start_date,omitempty"`
	EndDate     *string  `json:"end_date,omitempty"`
	Page        int      `json:"page"`
	PageSize    int      `json:"page_size"`
}

// CustomerStatsResponse represents customer statistics
type CustomerStatsResponse struct {
	TotalCustomers     int64         `json:"total_customers"`
	LeadCount          int64         `json:"lead_count"`
	ContactedCount     int64         `json:"contacted_count"`
	QualifiedCount     int64         `json:"qualified_count"`
	ConvertedCount     int64         `json:"converted_count"`
	LostCount          int64         `json:"lost_count"`
	ConversionRate     float64       `json:"conversion_rate"`
	AvgMomentum        float64       `json:"avg_momentum"`
	SourceDistribution []SourceCount `json:"source_distribution"`
	StatusDistribution []StatusCount `json:"status_distribution"`
}

type SourceCount struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

type StatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// CustomerResponse represents customer response
type CustomerResponse struct {
	ID                   int64      `json:"id"`
	TenantID             int64      `json:"tenant_id"`
	Name                 string     `json:"name"`
	Phone                *string    `json:"phone,omitempty"`
	Email                *string    `json:"email,omitempty"`
	Gender               *string    `json:"gender,omitempty"`
	Age                  *int       `json:"age,omitempty"`
	Source               *string    `json:"source,omitempty"`
	Status               string     `json:"status"`
	Momentum             int        `json:"momentum"`
	AssignedTo           *int64     `json:"assigned_to,omitempty"`
	AssignedToName       *string    `json:"assigned_to_name,omitempty"`
	AssignedAt           *string    `json:"assigned_at,omitempty"`
	ConvertedAt          *string    `json:"converted_at,omitempty"`
	FirstContactAt       *string    `json:"first_contact_at,omitempty"`
	LastContactedAt      *string    `json:"last_contacted_at,omitempty"`
	NextFollowUpAt       *string    `json:"next_follow_up_at,omitempty"`
	LifecycleStage       string     `json:"lifecycle_stage,omitempty"`
	ValueScore           int        `json:"value_score,omitempty"`
	FirstChannel         *string    `json:"first_channel,omitempty"`
	IdentityCount        int        `json:"identity_count,omitempty"`
	TotalInteractions    int        `json:"total_interactions,omitempty"`
	DealCount            int        `json:"deal_count,omitempty"`
	TotalConvertedAmount float64    `json:"total_converted_amount,omitempty"`
	LastInteractionAt    *string    `json:"last_interaction_at,omitempty"`
	LastConsultationItem *string    `json:"last_consultation_item,omitempty"`
	LastDealResult       *string    `json:"last_deal_result,omitempty"`
	LatestFollowUpStatus *string    `json:"latest_follow_up_status,omitempty"`
	PatientID            *int64     `json:"patient_id,omitempty"`
	LeadID               *int64     `json:"lead_id,omitempty"`
	Identities           []CustomerIdentityResponse    `json:"identities,omitempty"`
	RecentInteractions   []CustomerInteractionSummary  `json:"recent_interactions,omitempty"`
	Tags                 []string   `json:"tags,omitempty"`
	Notes                *string    `json:"notes,omitempty"`
	ExtraData            JSONObject `json:"extra_data,omitempty"`
	CreatedAt            string     `json:"created_at"`
	UpdatedAt            string     `json:"updated_at"`
}

type CustomerIdentityResponse struct {
	ID           int64   `json:"id"`
	ChannelType  string  `json:"channel_type"`
	ExternalID   *string `json:"external_id,omitempty"`
	ExternalName *string `json:"external_name,omitempty"`
	Status       string  `json:"status"`
}

type CustomerInteractionSummary struct {
	ID             int64   `json:"id"`
	InteractionType string  `json:"interaction_type"`
	Direction      *string `json:"direction,omitempty"`
	ContentSummary *string `json:"content_summary,omitempty"`
	StaffName      *string `json:"staff_name,omitempty"`
	OccurredAt     *string `json:"occurred_at,omitempty"`
	CreatedAt      *string `json:"created_at,omitempty"`
}

// CreateCustomerRequest represents the request for creating a customer
type CreateCustomerRequest struct {
	Name           string     `json:"name"`
	Phone          *string    `json:"phone,omitempty"`
	Email          *string    `json:"email,omitempty"`
	Gender         *string    `json:"gender,omitempty"`
	Age            *int       `json:"age,omitempty"`
	Source         *string    `json:"source,omitempty"`
	AssignedTo     *int64     `json:"assigned_to,omitempty"`
	NextFollowUpAt *string    `json:"next_follow_up_at,omitempty"`
	Tags           []string   `json:"tags,omitempty"`
	Notes          *string    `json:"notes,omitempty"`
	ExtraData      JSONObject `json:"extra_data,omitempty"`
}

// UpdateCustomerRequest represents the request for updating a customer
type UpdateCustomerRequest struct {
	Name           *string    `json:"name,omitempty"`
	Phone          *string    `json:"phone,omitempty"`
	Email          *string    `json:"email,omitempty"`
	Gender         *string    `json:"gender,omitempty"`
	Age            *int       `json:"age,omitempty"`
	Source         *string    `json:"source,omitempty"`
	Status         *string    `json:"status,omitempty"`
	Momentum       *int       `json:"momentum,omitempty"`
	AssignedTo     *int64     `json:"assigned_to,omitempty"`
	NextFollowUpAt *string    `json:"next_follow_up_at,omitempty"`
	Tags           []string   `json:"tags,omitempty"`
	Notes          *string    `json:"notes,omitempty"`
	ExtraData      JSONObject `json:"extra_data,omitempty"`
}

// AddIdentityRequest represents the request for adding customer identity
type AddIdentityRequest struct {
	Channel   string     `json:"channel"`
	ChannelID string     `json:"channel_id"`
	Nickname  *string    `json:"nickname,omitempty"`
	Avatar    *string    `json:"avatar,omitempty"`
	ExtraData JSONObject `json:"extra_data,omitempty"`
}

// InteractionListRequest represents the request for listing interactions
type InteractionListRequest struct {
	CustomerID int64   `json:"customer_id"`
	Type       *string `json:"type,omitempty"`
	Direction  *string `json:"direction,omitempty"`
	EmployeeID *int64  `json:"employee_id,omitempty"`
	StartDate  *string `json:"start_date,omitempty"`
	EndDate    *string `json:"end_date,omitempty"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

// InteractionResponse represents interaction response
type InteractionResponse struct {
	ID           int64   `json:"id"`
	CustomerID   int64   `json:"customer_id"`
	Type         string  `json:"type"`
	Direction    string  `json:"direction"`
	Content      *string `json:"content,omitempty"`
	Duration     *int    `json:"duration,omitempty"`
	RecordingID  *int64  `json:"recording_id,omitempty"`
	EmployeeID   int64   `json:"employee_id"`
	EmployeeName string  `json:"employee_name"`
	InteractedAt string  `json:"interacted_at"`
	CreatedAt    string  `json:"created_at"`
}

// CreateInteractionRequest represents the request for creating an interaction
type CreateInteractionRequest struct {
	Type        string  `json:"type"`
	Direction   string  `json:"direction"`
	Content     *string `json:"content,omitempty"`
	Duration    *int    `json:"duration,omitempty"`
	RecordingID *int64  `json:"recording_id,omitempty"`
}

// FollowUpListRequest represents the request for listing follow-ups
type FollowUpListRequest struct {
	CustomerID int64   `json:"customer_id"`
	Type       *string `json:"type,omitempty"`
	Status     *string `json:"status,omitempty"`
	EmployeeID *int64  `json:"employee_id,omitempty"`
	StartDate  *string `json:"start_date,omitempty"`
	EndDate    *string `json:"end_date,omitempty"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

// FollowUpResponse represents follow-up response
type FollowUpResponse struct {
	ID           int64   `json:"id"`
	CustomerID   int64   `json:"customer_id"`
	Type         string  `json:"type"`
	Status       string  `json:"status"`
	Content      string  `json:"content"`
	ScheduledAt  *string `json:"scheduled_at,omitempty"`
	CompletedAt  *string `json:"completed_at,omitempty"`
	EmployeeID   int64   `json:"employee_id"`
	EmployeeName string  `json:"employee_name"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// CreateFollowUpRequest represents the request for creating a follow-up
type CreateFollowUpRequest struct {
	Type        string  `json:"type"`
	Content     string  `json:"content"`
	ScheduledAt *string `json:"scheduled_at,omitempty"`
}

// DuplicateCheckRequest represents the request for duplicate check
type DuplicateCheckRequest struct {
	Phone *string `json:"phone,omitempty"`
	Email *string `json:"email,omitempty"`
}

// MergeCustomersRequest represents the request for merging customers
type MergeCustomersRequest struct {
	SourceIDs []int64 `json:"source_ids"`
	TargetID  int64   `json:"target_id"`
}

// Tag Management DTOs

// TagListRequest represents the request for listing tags
type TagListRequest struct {
	TenantID  *int64  `json:"tenant_id,omitempty"`
	TenantIDs []int64 `json:"tenant_ids,omitempty"`
	Name      *string `json:"name,omitempty"`
	Page      int     `json:"page"`
	PageSize  int     `json:"page_size"`
}

// TagResponse represents tag response
type TagResponse struct {
	ID            int64   `json:"id"`
	TenantID      int64   `json:"tenant_id"`
	Name          string  `json:"name"`
	Color         *string `json:"color,omitempty"`
	Description   *string `json:"description,omitempty"`
	CustomerCount int     `json:"customer_count"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// CreateTagRequest represents the request for creating a tag
type CreateTagRequest struct {
	Name        string  `json:"name"`
	Color       *string `json:"color,omitempty"`
	Description *string `json:"description,omitempty"`
}

// UpdateTagRequest represents the request for updating a tag
type UpdateTagRequest struct {
	Name        *string `json:"name,omitempty"`
	Color       *string `json:"color,omitempty"`
	Description *string `json:"description,omitempty"`
}

// BatchTagRequest represents the request for batch tagging
type BatchTagRequest struct {
	CustomerIDs []int64 `json:"customer_ids"`
	TagIDs      []int64 `json:"tag_ids"`
	Action      string  `json:"action"` // "add", "remove"
}

// Group Management DTOs

// GroupListRequest represents the request for listing groups
type GroupListRequest struct {
	TenantID  *int64  `json:"tenant_id,omitempty"`
	TenantIDs []int64 `json:"tenant_ids,omitempty"`
	Name      *string `json:"name,omitempty"`
	Type      *string `json:"type,omitempty"`
	Page      int     `json:"page"`
	PageSize  int     `json:"page_size"`
}

// GroupResponse represents group response
type GroupResponse struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Type        string     `json:"type"`
	Rules       JSONObject `json:"rules,omitempty"`
	MemberCount int        `json:"member_count"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

// CreateGroupRequest represents the request for creating a group
type CreateGroupRequest struct {
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Type        string     `json:"type"`
	Rules       JSONObject `json:"rules,omitempty"`
}

// UpdateGroupRequest represents the request for updating a group
type UpdateGroupRequest struct {
	Name        *string    `json:"name,omitempty"`
	Description *string    `json:"description,omitempty"`
	Rules       JSONObject `json:"rules,omitempty"`
}

// AddMembersRequest represents the request for adding members
type AddMembersRequest struct {
	CustomerIDs []int64 `json:"customer_ids"`
}

// RemoveMembersRequest represents the request for removing members
type RemoveMembersRequest struct {
	CustomerIDs []int64 `json:"customer_ids"`
}

// RulePreviewRequest represents the request for rule preview
type RulePreviewRequest struct {
	Rules JSONObject `json:"rules"`
}

// RulePreviewResponse represents rule preview response
type RulePreviewResponse struct {
	MatchCount int     `json:"match_count"`
	Customers  []int64 `json:"customers"`
}

// RuleValidateRequest represents the request for rule validation
type RuleValidateRequest struct {
	Rules JSONObject `json:"rules"`
}

// RuleValidateResponse represents rule validation response
type RuleValidateResponse struct {
	IsValid bool     `json:"is_valid"`
	Errors  []string `json:"errors"`
}

// RuleField represents a rule field
type RuleField struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // "string", "number", "date", "enum"
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description"`
}

// RuleOperator represents a rule operator
type RuleOperator struct {
	Name            string   `json:"name"`
	Label           string   `json:"label"`
	ApplicableTypes []string `json:"applicable_types"`
	Description     string   `json:"description"`
}

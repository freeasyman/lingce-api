package customer

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// Customer represents a customer
type Customer struct {
	ID                   int64      `json:"id"`
	TenantID             int64      `json:"tenant_id"`
	Name                 string     `json:"name"`
	Phone                *string    `json:"phone,omitempty"`
	Email                *string    `json:"email,omitempty"`
	Gender               *string    `json:"gender,omitempty"` // "male", "female", "other"
	Age                  *int       `json:"age,omitempty"`
	Source               *string    `json:"source,omitempty"` // "wechat", "phone", "referral", "website"
	Status               string     `json:"status"`           // "lead", "contacted", "qualified", "converted", "lost"
	Momentum             int        `json:"momentum"`         // 热度值 0-100
	AssignedTo           *int64     `json:"assigned_to,omitempty"`
	AssignedAt           *time.Time `json:"assigned_at,omitempty"`
	ConvertedAt          *time.Time `json:"converted_at,omitempty"`
	LastContactedAt      *time.Time `json:"last_contacted_at,omitempty"`
	NextFollowUpAt       *time.Time `json:"next_follow_up_at,omitempty"`
	LifecycleStage       string     `json:"lifecycle_stage,omitempty"`
	ValueScore           int        `json:"value_score,omitempty"`
	FirstChannel         *string    `json:"first_channel,omitempty"`
	IdentityCount        int        `json:"identity_count,omitempty"`
	TotalInteractions    int        `json:"total_interactions,omitempty"`
	DealCount            int        `json:"deal_count,omitempty"`
	TotalConvertedAmount float64    `json:"total_converted_amount,omitempty"`
	LastInteractionAt    *time.Time `json:"last_interaction_at,omitempty"`
	Tags                 []string   `json:"tags,omitempty"`
	Notes                *string    `json:"notes,omitempty"`
	ExtraData            JSONObject `json:"extra_data,omitempty"`
	CreatedBy            int64      `json:"created_by"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	DeletedAt            *time.Time `json:"deleted_at,omitempty"`
}

// CustomerIdentity represents a customer identity on different channels
type CustomerIdentity struct {
	ID         int64      `json:"id"`
	CustomerID int64      `json:"customer_id"`
	Channel    string     `json:"channel"` // "wechat", "phone", "email", "qq"
	ChannelID  string     `json:"channel_id"`
	Nickname   *string    `json:"nickname,omitempty"`
	Avatar     *string    `json:"avatar,omitempty"`
	ExtraData  JSONObject `json:"extra_data,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// CustomerInteraction represents a customer interaction
type CustomerInteraction struct {
	ID           int64     `json:"id"`
	CustomerID   int64     `json:"customer_id"`
	TenantID     int64     `json:"tenant_id"`
	Type         string    `json:"type"`      // "call", "message", "meeting", "email"
	Direction    string    `json:"direction"` // "inbound", "outbound"
	Content      *string   `json:"content,omitempty"`
	Duration     *int      `json:"duration,omitempty"` // seconds
	RecordingID  *int64    `json:"recording_id,omitempty"`
	EmployeeID   int64     `json:"employee_id"`
	InteractedAt time.Time `json:"interacted_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CustomerFollowUp represents a customer follow-up record
type CustomerFollowUp struct {
	ID          int64      `json:"id"`
	CustomerID  int64      `json:"customer_id"`
	TenantID    int64      `json:"tenant_id"`
	Type        string     `json:"type"`   // "call", "visit", "email", "other"
	Status      string     `json:"status"` // "planned", "completed", "cancelled"
	Content     string     `json:"content"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	EmployeeID  int64      `json:"employee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// CustomerTag represents a customer tag
type CustomerTag struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	Name        string     `json:"name"`
	Color       *string    `json:"color,omitempty"`
	Description *string    `json:"description,omitempty"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// CustomerGroup represents a customer group
type CustomerGroup struct {
	ID          int64      `json:"id"`
	TenantID    int64      `json:"tenant_id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Type        string     `json:"type"` // "static", "dynamic"
	Rules       JSONObject `json:"rules,omitempty"`
	MemberCount int        `json:"member_count"`
	CreatedBy   int64      `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// CustomerMembership represents customer membership information
type CustomerMembership struct {
	ID         int64      `json:"id"`
	CustomerID int64      `json:"customer_id"`
	TenantID   int64      `json:"tenant_id"`
	Level      string     `json:"level"` // "bronze", "silver", "gold", "platinum"
	Points     int        `json:"points"`
	StartDate  time.Time  `json:"start_date"`
	EndDate    *time.Time `json:"end_date,omitempty"`
	IsActive   bool       `json:"is_active"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// MomentumHistory represents customer momentum history
type MomentumHistory struct {
	Date     string `json:"date"`
	Momentum int    `json:"momentum"`
}

// Custom JSON type for database storage
type JSONObject map[string]interface{}

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

func (j JSONObject) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

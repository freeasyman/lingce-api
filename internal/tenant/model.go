package tenant

import "time"

// Tenant represents a tenant entity
type Tenant struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	Code              string     `json:"code"`
	AccountMode       string     `json:"account_mode,omitempty"`
	ContactName       string     `json:"contact_name,omitempty"`
	ContactPhone      string     `json:"contact_phone,omitempty"`
	ContactEmail      string     `json:"contact_email,omitempty"`
	Industry          string     `json:"industry,omitempty"`
	IsActive          bool       `json:"is_active"`
	ValidFrom         *time.Time `json:"valid_from"`
	ValidTo           *time.Time `json:"valid_to"`
	SubscriptionPlan  string     `json:"plan_name,omitempty"`
	SubscriptionState string     `json:"service_status,omitempty"`
	SubscriptionEndAt *time.Time `json:"expires_at,omitempty"`
	FeatureGroupID    *int64     `json:"feature_group_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	DeletedAt         *time.Time `json:"deleted_at"`
}

type MedicalSpecialty struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	ParentID  *int64 `json:"parent_id,omitempty"`
	Level     int    `json:"level"`
	SortOrder int    `json:"sort_order"`
}

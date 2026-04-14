package sysconfig

import "time"

// Tenant represents a tenant (same as organization.Tenant but in sysconfig context)
type Tenant struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	IsActive  bool       `json:"is_active"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// TenantSubscriptionPlan represents a subscription plan
type TenantSubscriptionPlan struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Code         string    `json:"code"`
	Description  *string   `json:"description,omitempty"`
	DurationDays int       `json:"duration_days"`
	Price        *float64  `json:"price,omitempty"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TenantSubscription represents a tenant's subscription
type TenantSubscription struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	PlanID       *int64     `json:"plan_id,omitempty"`
	Status       string     `json:"status"` // active, grace, expired, paused
	StartDate    time.Time  `json:"start_date"`
	EndDate      time.Time  `json:"end_date"`
	GraceEndDate *time.Time `json:"grace_end_date,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// TenantSubscriptionEvent represents a subscription event
type TenantSubscriptionEvent struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	EventType    string     `json:"event_type"` // renew, upgrade, pause, cancel, activate
	OldStatus    *string    `json:"old_status,omitempty"`
	NewStatus    string     `json:"new_status"`
	OldEndDate   *time.Time `json:"old_end_date,omitempty"`
	NewEndDate   *time.Time `json:"new_end_date,omitempty"`
	OperatorID   *int64     `json:"operator_id,omitempty"`
	OperatorType *string    `json:"operator_type,omitempty"`
	Notes        *string    `json:"notes,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TenantValidityChangeLog represents a validity change log
type TenantValidityChangeLog struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	OldValidFrom *time.Time `json:"old_valid_from,omitempty"`
	NewValidFrom *time.Time `json:"new_valid_from,omitempty"`
	OldValidTo   *time.Time `json:"old_valid_to,omitempty"`
	NewValidTo   *time.Time `json:"new_valid_to,omitempty"`
	OperatorID   *int64     `json:"operator_id,omitempty"`
	OperatorType *string    `json:"operator_type,omitempty"`
	Reason       *string    `json:"reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TenantFeatureGroup represents a feature group
type TenantFeatureGroup struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description *string   `json:"description,omitempty"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TenantFeatureGroupItem represents a feature in a group
type TenantFeatureGroupItem struct {
	ID          int64  `json:"id"`
	GroupID     int64  `json:"group_id"`
	ItemType    string `json:"item_type"`
	ItemCode    string `json:"item_code"`
	FeatureCode string `json:"feature_code"`
	IsEnabled   bool   `json:"is_enabled"`
}

// TenantFeatureOverride represents a feature override for a tenant
type TenantFeatureOverride struct {
	ID           int64     `json:"id"`
	TenantID     int64     `json:"tenant_id"`
	ItemType     string    `json:"item_type"`
	ItemCode     string    `json:"item_code"`
	OverrideMode string    `json:"override_mode"`
	FeatureCode  string    `json:"feature_code"`
	IsEnabled    bool      `json:"is_enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TenantFeatureChangeLog represents a feature change log
type TenantFeatureChangeLog struct {
	ID           int64     `json:"id"`
	TenantID     int64     `json:"tenant_id"`
	FeatureCode  string    `json:"feature_code"`
	OldValue     *bool     `json:"old_value,omitempty"`
	NewValue     bool      `json:"new_value"`
	OperatorID   *int64    `json:"operator_id,omitempty"`
	OperatorType *string   `json:"operator_type,omitempty"`
	Reason       *string   `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

package sysconfig

import "time"

// Tenant DTOs
type TenantListRequest struct {
	Name     string `json:"name,omitempty"`
	Code     string `json:"code,omitempty"`
	IsActive *bool  `json:"is_active,omitempty"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

type CreateTenantRequest struct {
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
}

type UpdateTenantRequest struct {
	Name      *string    `json:"name,omitempty"`
	Code      *string    `json:"code,omitempty"`
	IsActive  *bool      `json:"is_active,omitempty"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
}

type TenantResponse struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	IsActive  bool       `json:"is_active"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Subscription Plan DTOs
type CreateSubscriptionPlanRequest struct {
	Name             string   `json:"name"`
	Code             string   `json:"code"`
	Description      *string  `json:"description,omitempty"`
	DurationDays     int      `json:"duration_days"`
	GraceDaysDefault int      `json:"grace_days_default"`
	FeatureGroupID   *int64   `json:"feature_group_id,omitempty"`
	Price            *float64 `json:"price,omitempty"`
}

type UpdateSubscriptionPlanRequest struct {
	Name             *string  `json:"name,omitempty"`
	Description      *string  `json:"description,omitempty"`
	DurationDays     *int     `json:"duration_days,omitempty"`
	GraceDaysDefault *int     `json:"grace_days_default,omitempty"`
	FeatureGroupID   *int64   `json:"feature_group_id,omitempty"`
	Price            *float64 `json:"price,omitempty"`
	IsActive         *bool    `json:"is_active,omitempty"`
}

type SubscriptionPlanResponse struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Code             string    `json:"code"`
	Description      *string   `json:"description,omitempty"`
	DurationDays     int       `json:"duration_days"`
	GraceDaysDefault int       `json:"grace_days_default"`
	FeatureGroupID   *int64    `json:"feature_group_id,omitempty"`
	Price            *float64  `json:"price,omitempty"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Subscription DTOs
type SubscriptionResponse struct {
	ID           int64                     `json:"id"`
	TenantID     int64                     `json:"tenant_id"`
	Plan         *SubscriptionPlanResponse `json:"plan,omitempty"`
	Status       string                    `json:"status"`
	StartDate    string                    `json:"start_date"`
	EndDate      string                    `json:"end_date"`
	GraceEndDate *string                   `json:"grace_end_date,omitempty"`
	CreatedAt    string                    `json:"created_at"`
	UpdatedAt    string                    `json:"updated_at"`
}

type SubscriptionActionRequest struct {
	Action     string     `json:"action"` // renew, upgrade, pause, cancel, activate
	PlanID     *int64     `json:"plan_id,omitempty"`
	ExtendDays *int       `json:"extend_days,omitempty"`
	NewEndDate *time.Time `json:"new_end_date,omitempty"`
	Notes      *string    `json:"notes,omitempty"`
}

type SubscriptionEventResponse struct {
	ID           int64   `json:"id"`
	TenantID     int64   `json:"tenant_id"`
	EventType    string  `json:"event_type"`
	OldStatus    *string `json:"old_status,omitempty"`
	NewStatus    string  `json:"new_status"`
	OldEndDate   *string `json:"old_end_date,omitempty"`
	NewEndDate   *string `json:"new_end_date,omitempty"`
	OperatorID   *int64  `json:"operator_id,omitempty"`
	OperatorType *string `json:"operator_type,omitempty"`
	Notes        *string `json:"notes,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

type ValidityChangeLogResponse struct {
	ID           int64   `json:"id"`
	TenantID     int64   `json:"tenant_id"`
	OldValidFrom *string `json:"old_valid_from,omitempty"`
	NewValidFrom *string `json:"new_valid_from,omitempty"`
	OldValidTo   *string `json:"old_valid_to,omitempty"`
	NewValidTo   *string `json:"new_valid_to,omitempty"`
	OperatorID   *int64  `json:"operator_id,omitempty"`
	OperatorType *string `json:"operator_type,omitempty"`
	Reason       *string `json:"reason,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// Feature Group DTOs
type CreateFeatureGroupRequest struct {
	Name        string                     `json:"name"`
	Code        string                     `json:"code"`
	Description *string                    `json:"description,omitempty"`
	Items       []FeaturePolicyItem        `json:"items,omitempty"`
	Features    []FeatureGroupItemResponse `json:"features,omitempty"` // backward compatibility
}

type UpdateFeatureGroupRequest struct {
	Name        *string                     `json:"name,omitempty"`
	Description *string                     `json:"description,omitempty"`
	IsActive    *bool                       `json:"is_active,omitempty"`
	Items       *[]FeaturePolicyItem        `json:"items,omitempty"`
	Features    *[]FeatureGroupItemResponse `json:"features,omitempty"` // backward compatibility
}

type FeatureGroupResponse struct {
	ID          int64                      `json:"id"`
	Name        string                     `json:"name"`
	Code        string                     `json:"code"`
	Description *string                    `json:"description,omitempty"`
	IsActive    bool                       `json:"is_active"`
	Items       []FeaturePolicyItem        `json:"items,omitempty"`
	Features    []FeatureGroupItemResponse `json:"features,omitempty"` // backward compatibility
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
}

type FeatureGroupItemResponse struct {
	ID          int64  `json:"id"`
	FeatureCode string `json:"feature_code"`
	IsEnabled   bool   `json:"is_enabled"`
}

type AssignFeatureGroupRequest struct {
	GroupID *int64 `json:"group_id"`
}

type FeatureOverrideRequest struct {
	Items     []FeatureOverrideItem `json:"items,omitempty"`
	Overrides []FeatureOverrideItem `json:"overrides,omitempty"` // backward compatibility
}

type FeaturePolicyItem struct {
	ItemType string `json:"item_type"`
	ItemCode string `json:"item_code"`
}

type FeatureOverrideItem struct {
	ItemType     string `json:"item_type,omitempty"`
	ItemCode     string `json:"item_code,omitempty"`
	OverrideMode string `json:"override_mode,omitempty"`
	FeatureCode  string `json:"feature_code,omitempty"` // backward compatibility
	IsEnabled    bool   `json:"is_enabled,omitempty"`   // backward compatibility
}

type FeatureOverrideResponse struct {
	ID           int64  `json:"id"`
	ItemType     string `json:"item_type"`
	ItemCode     string `json:"item_code"`
	OverrideMode string `json:"override_mode"`
	FeatureCode  string `json:"feature_code,omitempty"` // backward compatibility
	IsEnabled    *bool  `json:"is_enabled,omitempty"`   // backward compatibility
}

type EffectiveFeaturePolicyResponse struct {
	TenantID        int64    `json:"tenant_id"`
	FeatureGroupID  *int64   `json:"feature_group_id,omitempty"`
	Unrestricted    bool     `json:"unrestricted"`
	AllowedMenus    []string `json:"allowed_menus"`
	AllowedFeatures []string `json:"allowed_features"`
}

type FeatureOptionItemResponse struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Category    *string `json:"category,omitempty"`
}

type MenuFeatureOptionItemResponse struct {
	ID         int64   `json:"id"`
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	ParentCode *string `json:"parent_code,omitempty"`
	ParentName *string `json:"parent_name,omitempty"`
	Path       *string `json:"path,omitempty"`
}

type TenantFeatureOptionsResponse struct {
	MenuItems    []MenuFeatureOptionItemResponse `json:"menu_items"`
	FeatureItems []FeatureOptionItemResponse     `json:"feature_items"`
}

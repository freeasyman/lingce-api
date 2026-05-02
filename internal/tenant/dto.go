package tenant

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type FlexibleInt64 struct {
	value *int64
}

func (f *FlexibleInt64) Ptr() *int64 {
	if f == nil {
		return nil
	}
	return f.value
}

func (f *FlexibleInt64) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		f.value = nil
		return nil
	}

	var num int64
	if err := json.Unmarshal(data, &num); err == nil {
		f.value = &num
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		f.value = nil
		return nil
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	f.value = &parsed
	return nil
}

// TenantListRequest represents tenant list query parameters
type TenantListRequest struct {
	Name     string `json:"name"`
	Code     string `json:"code"`
	IsActive *bool  `json:"is_active"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// CreateTenantRequest represents tenant creation request
type CreateTenantRequest struct {
	Name                     string        `json:"name"`
	Code                     string        `json:"code"`
	ContactName              *string       `json:"contact_name,omitempty"`
	ContactPhone             *string       `json:"contact_phone,omitempty"`
	ContactEmail             *string       `json:"contact_email,omitempty"`
	Industry                 *string       `json:"industry,omitempty"`
	ValidFrom                *time.Time    `json:"valid_from"`
	ValidTo                  *time.Time    `json:"valid_to"`
	SubscriptionPlanID       FlexibleInt64 `json:"subscription_plan_id,omitempty"`
	SubscriptionPlanName     *string       `json:"subscription_plan_name,omitempty"`
	SubscriptionDurationDays *int          `json:"subscription_duration_days,omitempty"`
	SubscriptionStartedOn    *string       `json:"subscription_started_on,omitempty"`
	SubscriptionGraceDays    *int          `json:"subscription_grace_days,omitempty"`
}

// UpdateTenantRequest represents tenant update request
type UpdateTenantRequest struct {
	Name                     *string       `json:"name"`
	Code                     *string       `json:"code"`
	ContactName              *string       `json:"contact_name,omitempty"`
	ContactPhone             *string       `json:"contact_phone,omitempty"`
	ContactEmail             *string       `json:"contact_email,omitempty"`
	Industry                 *string       `json:"industry,omitempty"`
	IsActive                 *bool         `json:"is_active"`
	ValidFrom                *time.Time    `json:"valid_from"`
	ValidTo                  *time.Time    `json:"valid_to"`
	SubscriptionPlanID       FlexibleInt64 `json:"subscription_plan_id,omitempty"`
	SubscriptionPlanName     *string       `json:"subscription_plan_name,omitempty"`
	SubscriptionDurationDays *int          `json:"subscription_duration_days,omitempty"`
	SubscriptionStartedOn    *string       `json:"subscription_started_on,omitempty"`
	SubscriptionGraceDays    *int          `json:"subscription_grace_days,omitempty"`
}

// SubscriptionActionRequest represents request payload for subscription actions.
type SubscriptionActionRequest struct {
	PlanID     *int64     `json:"plan_id,omitempty"`
	ExtendDays *int       `json:"extend_days,omitempty"`
	NewEndDate *time.Time `json:"new_end_date,omitempty"`
	Notes      *string    `json:"notes,omitempty"`
}

type AssignFeatureGroupRequest struct {
	GroupID *int64 `json:"group_id"`
}

type FeatureOverrideRequest struct {
	Items     []FeatureOverrideItem `json:"items,omitempty"`
	Overrides []FeatureOverrideItem `json:"overrides,omitempty"` // backward compatibility
}

type FeatureOverrideItem struct {
	ItemType     string `json:"item_type,omitempty"`
	ItemCode     string `json:"item_code,omitempty"`
	OverrideMode string `json:"override_mode,omitempty"`
	FeatureCode  string `json:"feature_code,omitempty"` // backward compatibility
	IsEnabled    bool   `json:"is_enabled,omitempty"`   // backward compatibility
}

type UpdateTenantProfileRequest struct {
	Name         *string `json:"name,omitempty"`
	OrgCode      *string `json:"org_code,omitempty"`
	Code         *string `json:"code,omitempty"`
	ContactName  *string `json:"contact_name,omitempty"`
	ContactPhone *string `json:"contact_phone,omitempty"`
	ContactEmail *string `json:"contact_email,omitempty"`
	Industry     *string `json:"industry,omitempty"`
}

type TenantProfileResponse struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	OrgCode       string     `json:"org_code"`
	ContactName   string     `json:"contact_name,omitempty"`
	ContactPhone  string     `json:"contact_phone,omitempty"`
	ContactEmail  string     `json:"contact_email,omitempty"`
	Industry      string     `json:"industry,omitempty"`
	ValidFrom     *time.Time `json:"valid_from,omitempty"`
	ValidTo       *time.Time `json:"valid_to,omitempty"`
	DaysRemaining int        `json:"days_remaining"`
	CreatedAt     time.Time  `json:"created_at"`
}

type MedicalSpecialtyResponse struct {
	ID        int64                       `json:"id"`
	Name      string                      `json:"name"`
	Code      string                      `json:"code"`
	ParentID  *int64                      `json:"parent_id,omitempty"`
	Level     int                         `json:"level"`
	SortOrder int                         `json:"sort_order"`
	Children  []*MedicalSpecialtyResponse `json:"children,omitempty"`
}

type InstitutionStatistics struct {
	TenantID           int64 `json:"tenant_id"`
	TotalEmployees     int   `json:"total_employees"`
	TotalDepartments   int   `json:"total_departments"`
	TotalBadgeDevices  int   `json:"total_badge_devices"`
	TotalRecordings    int   `json:"total_recordings"`
	RecordingsThisWeek int   `json:"recordings_this_week"`
}

package tenant

import "time"

// Tenant represents a tenant entity
type Tenant struct {
	ID                     int64      `json:"id"`
	Name                   string     `json:"name"`
	Code                   string     `json:"code"`
	AccountMode            string     `json:"account_mode,omitempty"`
	ContactName            string     `json:"contact_name,omitempty"`
	ContactPhone           string     `json:"contact_phone,omitempty"`
	ContactEmail           string     `json:"contact_email,omitempty"`
	Industry               string     `json:"industry,omitempty"`
	IsActive               bool       `json:"is_active"`
	ValidFrom              *time.Time `json:"valid_from"`
	ValidTo                *time.Time `json:"valid_to"`
	SubscriptionPlan       string     `json:"plan_name,omitempty"`
	SubscriptionState      string     `json:"service_status,omitempty"`
	SubscriptionEndAt      *time.Time `json:"expires_at,omitempty"`
	FeatureGroupID         *int64     `json:"feature_group_id,omitempty"`
	TrialSalesOwnerAdminID *int64     `json:"trial_sales_owner_admin_id,omitempty"`
	TrialSalesOwnerName    string     `json:"trial_sales_owner_name,omitempty"`
	TrialSource            string     `json:"trial_source,omitempty"`
	TrialNotes             string     `json:"trial_notes,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	DeletedAt              *time.Time `json:"deleted_at"`
}

type MedicalSpecialty struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	ParentID  *int64 `json:"parent_id,omitempty"`
	Level     int    `json:"level"`
	SortOrder int    `json:"sort_order"`
}

type TrialDemoRecordingAsset struct {
	ID                int64  `json:"id"`
	TemplateCode      string `json:"template_code"`
	AssetCode         string `json:"asset_code"`
	RoleCode          string `json:"role_code"`
	Title             string `json:"title"`
	SourceTenantID    int64  `json:"source_tenant_id"`
	SourceRecordingID int64  `json:"source_recording_id"`
	SourceCustomerID  *int64 `json:"source_customer_id,omitempty"`
	Version           string `json:"version"`
	IsActive          bool   `json:"is_active"`
	SortOrder         int    `json:"sort_order"`
}

type TenantTrialDemoRecording struct {
	TenantID           int64      `json:"tenant_id"`
	TemplateCode       string     `json:"template_code"`
	RoleCode           string     `json:"role_code"`
	AssetID            int64      `json:"asset_id"`
	RecordingID        int64      `json:"recording_id"`
	EmployeeID         int64      `json:"employee_id"`
	CustomerID         *int64     `json:"customer_id,omitempty"`
	Title              string     `json:"title"`
	ViewedAt           *time.Time `json:"viewed_at,omitempty"`
	ViewedByEmployeeID *int64     `json:"viewed_by_employee_id,omitempty"`
}

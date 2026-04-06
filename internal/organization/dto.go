package organization

import "time"

// CreateTenantRequest represents a request to create a tenant
type CreateTenantRequest struct {
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
}

// UpdateTenantRequest represents a request to update a tenant
type UpdateTenantRequest struct {
	Name      *string    `json:"name,omitempty"`
	Code      *string    `json:"code,omitempty"`
	IsActive  *bool      `json:"is_active,omitempty"`
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
}

// TenantResponse represents a tenant response
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

// TenantListRequest represents a request to list tenants
type TenantListRequest struct {
	Name     string `json:"name,omitempty"`
	Code     string `json:"code,omitempty"`
	IsActive *bool  `json:"is_active,omitempty"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

package tenant

import "time"

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
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	ValidFrom *time.Time `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to"`
}

// UpdateTenantRequest represents tenant update request
type UpdateTenantRequest struct {
	Name      *string    `json:"name"`
	Code      *string    `json:"code"`
	IsActive  *bool      `json:"is_active"`
	ValidFrom *time.Time `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to"`
}

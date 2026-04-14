package tenant

import "time"

// Tenant represents a tenant entity
type Tenant struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	IsActive  bool       `json:"is_active"`
	ValidFrom *time.Time `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type MedicalSpecialty struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	ParentID  *int64 `json:"parent_id,omitempty"`
	Level     int    `json:"level"`
	SortOrder int    `json:"sort_order"`
}

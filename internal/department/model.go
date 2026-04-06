package department

import "time"

// Department represents a department within a tenant
type Department struct {
	ID        int64      `json:"id"`
	TenantID  int64      `json:"tenant_id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	ParentID  *int64     `json:"parent_id,omitempty"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

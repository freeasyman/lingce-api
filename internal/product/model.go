package product

import "time"

type Product struct {
	ID           int64     `json:"id"`
	TenantID     int64     `json:"tenant_id"`
	TenantName   string    `json:"tenant_name,omitempty"`
	Industry     string    `json:"industry"`
	Name         string    `json:"name"`
	Aliases      []string  `json:"aliases"`
	Price        float64   `json:"price"`
	Applicable   string    `json:"applicable"`
	SellingPoint string    `json:"selling_point"`
	UpgradeTo    string    `json:"upgrade_to"`
	CombineWith  string    `json:"combine_with"`
	Status       string    `json:"status"`
	IsTemplate   bool      `json:"is_template"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

package product

import "time"

type Product struct {
	ID           int64    `json:"id"`
	TenantID     int64    `json:"tenant_id"`
	TenantName   string   `json:"tenant_name,omitempty"`
	Industry     string   `json:"industry"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	Price        float64  `json:"price"`
	Applicable   string   `json:"applicable"`
	SellingPoint string   `json:"selling_point"`
	UpgradeTo    string   `json:"upgrade_to"`
	CombineWith  string   `json:"combine_with"`
	Status       string   `json:"status"`
	IsTemplate   bool     `json:"is_template"`

	// 产品固有匹配属性：只描述产品自身，不包含场景、风险等客户判断。
	ApplicableKeywords []string `json:"applicable_keywords"`
	AgeMin             *int     `json:"age_min,omitempty"`
	AgeMax             *int     `json:"age_max,omitempty"`
	Gender             string   `json:"gender"`
	Contraindications  string   `json:"contraindications"`
	ReviewStatus       string   `json:"review_status"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

package product

import "time"

type ListRequest struct {
	TenantID     int64
	Industry     string
	Status       string
	ReviewStatus string
	Keyword      string
	IsTemplate   *bool
	Page         int
	PageSize     int
}

type CreateRequest struct {
	TenantID     int64    `json:"tenant_id"`
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

	ApplicableKeywords []string `json:"applicable_keywords,omitempty"`
	AgeMin             *int     `json:"age_min,omitempty"`
	AgeMax             *int     `json:"age_max,omitempty"`
	Gender             string   `json:"gender,omitempty"`
	Contraindications  string   `json:"contraindications,omitempty"`
	ReviewStatus       string   `json:"review_status,omitempty"`
}

type UpdateRequest struct {
	Industry     *string   `json:"industry,omitempty"`
	Name         *string   `json:"name,omitempty"`
	Aliases      *[]string `json:"aliases,omitempty"`
	Price        *float64  `json:"price,omitempty"`
	Applicable   *string   `json:"applicable,omitempty"`
	SellingPoint *string   `json:"selling_point,omitempty"`
	UpgradeTo    *string   `json:"upgrade_to,omitempty"`
	CombineWith  *string   `json:"combine_with,omitempty"`
	Status       *string   `json:"status,omitempty"`
	IsTemplate   *bool     `json:"is_template,omitempty"`

	ApplicableKeywords *[]string `json:"applicable_keywords,omitempty"`
	AgeMin             *int      `json:"age_min,omitempty"`
	AgeMax             *int      `json:"age_max,omitempty"`
	Gender             *string   `json:"gender,omitempty"`
	Contraindications  *string   `json:"contraindications,omitempty"`
	ReviewStatus       *string   `json:"review_status,omitempty"`
}

// CatalogItem 是 /products/catalog 的只读字段，顺序与赢客专家客户端契约一致。
type CatalogItem struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Aliases            []string  `json:"aliases"`
	Price              float64   `json:"price"`
	Applicable         string    `json:"applicable"`
	ApplicableKeywords []string  `json:"applicable_keywords"`
	ReviewStatus       string    `json:"review_status"`
	Status             string    `json:"status"`
	Version            int       `json:"version"`
	UpdatedAt          time.Time `json:"updated_at"`
}

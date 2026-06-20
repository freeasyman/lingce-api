package product

type ListRequest struct {
	TenantID   int64
	Industry   string
	Status     string
	Keyword    string
	IsTemplate *bool
	Page       int
	PageSize   int
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
}

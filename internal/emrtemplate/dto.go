package emrtemplate

type BindingStageConfig map[string]string

type RuleSummary struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type BindingItemResponse struct {
	ID              int64              `json:"id"`
	RuleID          string             `json:"rule_id"`
	RuleCode        string             `json:"rule_code"`
	RuleName        string             `json:"rule_name"`
	RuleDetail      string             `json:"rule_detail"`
	Enabled         bool               `json:"enabled"`
	RuleScope       string             `json:"rule_scope"`
	SortOrder       int                `json:"sort_order"`
	StageConfigJSON BindingStageConfig `json:"stage_config_json"`
	Rule            RuleSummary        `json:"rule"`
}

type TemplateListItemResponse struct {
	ID             int64  `json:"id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	ShortName      string `json:"short_name"`
	DepartmentCode string `json:"department_code"`
	DepartmentName string `json:"department_name"`
	Status         string `json:"status"`
	Description    string `json:"description"`
	VersionNo      int    `json:"version_no"`
	IsSystem       bool   `json:"is_system"`
	UpdatedAt      string `json:"updated_at"`
}

type TemplateDetailResponse struct {
	ID             int64                  `json:"id"`
	TenantID       int64                  `json:"tenant_id"`
	Code           string                 `json:"code"`
	Name           string                 `json:"name"`
	ShortName      string                 `json:"short_name"`
	DepartmentCode string                 `json:"department_code"`
	DepartmentName string                 `json:"department_name"`
	Status         string                 `json:"status"`
	Description    string                 `json:"description"`
	VersionNo      int                    `json:"version_no"`
	IsSystem       bool                   `json:"is_system"`
	SchemaJSON     map[string]any         `json:"schema_json"`
	Bindings       []*BindingItemResponse `json:"bindings"`
	UpdatedAt      string                 `json:"updated_at"`
}

type SaveTemplateRequest struct {
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	ShortName      string         `json:"short_name"`
	DepartmentCode string         `json:"department_code"`
	DepartmentName string         `json:"department_name"`
	Status         string         `json:"status"`
	Description    string         `json:"description"`
	SchemaJSON     map[string]any `json:"schema_json"`
}

type SaveBindingsRequest struct {
	Items []BindingItemUpsert `json:"items"`
}

type BindingItemUpsert struct {
	RuleID          string             `json:"rule_id"`
	RuleCode        string             `json:"rule_code"`
	RuleName        string             `json:"rule_name"`
	RuleDetail      string             `json:"rule_detail"`
	Enabled         bool               `json:"enabled"`
	RuleScope       string             `json:"rule_scope"`
	SortOrder       int                `json:"sort_order"`
	StageConfigJSON BindingStageConfig `json:"stage_config_json"`
}

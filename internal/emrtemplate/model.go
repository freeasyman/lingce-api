package emrtemplate

import "time"

type Template struct {
	ID        string    `json:"id"`
	TenantID  *int64    `json:"tenant_id,omitempty"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedBy int64     `json:"created_by"`
	UpdatedBy int64     `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TemplateVersion struct {
	ID              string     `json:"id"`
	TemplateID      string     `json:"template_id"`
	VersionNo       string     `json:"version_no"`
	Name            string     `json:"name"`
	DocumentType    string     `json:"document_type"`
	VisitType       string     `json:"visit_type"`
	DepartmentID    *int64     `json:"department_id,omitempty"`
	SpecialtyModule *string    `json:"specialty_module,omitempty"`
	PrintTitle      string     `json:"print_title"`
	Description     string     `json:"description"`
	Status          string     `json:"status"`
	CreatedBy       int64      `json:"created_by"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	PublishedBy     *int64     `json:"published_by,omitempty"`
	DisabledAt      *time.Time `json:"disabled_at,omitempty"`
	DisabledBy      *int64     `json:"disabled_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type Section struct {
	ID                string         `json:"id"`
	TemplateVersionID string         `json:"template_version_id"`
	Code              string         `json:"code"`
	Name              string         `json:"name"`
	ContentCategory   string         `json:"content_category"`
	InputType         string         `json:"input_type"`
	IsCommon          bool           `json:"is_common"`
	IsVisible         bool           `json:"is_visible"`
	DisplayOrder      int            `json:"display_order"`
	Description       string         `json:"description"`
	Options           map[string]any `json:"options,omitempty"`
	Structure         map[string]any `json:"structure,omitempty"`
}

type RequirementBinding struct {
	ID                      string `json:"id"`
	TemplateVersionID       string `json:"template_version_id"`
	QualityRequirementID    string `json:"quality_requirement_id"`
	ExecutionMode           string `json:"execution_mode"`
	DeadlineAction          string `json:"deadline_action"`
	DisplayOrder            int    `json:"display_order"`
	RequirementCode         string `json:"requirement_code"`
	RequirementName         string `json:"requirement_name"`
	RequirementRuleType     string `json:"requirement_rule_type"`
	RequirementQualityGroup string `json:"requirement_quality_group"`
}

type TemplateDetail struct {
	Template *Template             `json:"template"`
	Versions []*TemplateVersion    `json:"versions"`
	Sections []*Section            `json:"sections,omitempty"`
	Bindings []*RequirementBinding `json:"quality_requirements,omitempty"`
}

type SaveTemplateRequest struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type SaveVersionRequest struct {
	VersionNo       string  `json:"version_no"`
	Name            string  `json:"name"`
	DocumentType    string  `json:"document_type"`
	VisitType       string  `json:"visit_type"`
	DepartmentID    *int64  `json:"department_id,omitempty"`
	SpecialtyModule *string `json:"specialty_module,omitempty"`
	PrintTitle      string  `json:"print_title"`
	Description     string  `json:"description"`
}

type SaveSectionsRequest struct {
	Items []SectionInput `json:"items"`
}
type SectionInput struct {
	Code            string         `json:"code"`
	Name            string         `json:"name"`
	ContentCategory string         `json:"content_category"`
	InputType       string         `json:"input_type"`
	IsCommon        bool           `json:"is_common"`
	IsVisible       bool           `json:"is_visible"`
	DisplayOrder    int            `json:"display_order"`
	Description     string         `json:"description"`
	Options         map[string]any `json:"options"`
	Structure       map[string]any `json:"structure"`
}

type SaveBindingsRequest struct {
	Items []BindingInput `json:"items"`
}
type BindingInput struct {
	QualityRequirementID string `json:"quality_requirement_id"`
	ExecutionMode        string `json:"execution_mode"`
	DeadlineAction       string `json:"deadline_action"`
	DisplayOrder         int    `json:"display_order"`
}

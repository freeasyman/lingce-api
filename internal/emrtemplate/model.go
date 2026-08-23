package emrtemplate

import "time"

type Template struct {
	ID             int64
	TenantID       int64
	Code           string
	Name           string
	ShortName      string
	DepartmentCode string
	DepartmentName string
	Status         string
	Description    string
	VersionNo      int
	IsSystem       bool
	SchemaJSON     map[string]any
	CreatedBy      *int64
	UpdatedBy      *int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type TemplateRuleBinding struct {
	ID              int64
	TenantID        int64
	TemplateID      int64
	RuleID          string
	RuleCode        string
	RuleName        string
	RuleDetail      string
	Enabled         bool
	RuleScope       string
	StageConfigJSON map[string]string
	SortOrder       int
	CreatedBy       *int64
	UpdatedBy       *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

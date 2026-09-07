package emrquality

import "time"

type QualityRequirement struct {
	ID            string     `json:"id"`
	TenantID      *int64     `json:"tenant_id,omitempty"`
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	RuleType      string     `json:"rule_type"`
	QualityGroup  string     `json:"quality_group"`
	SourceName    string     `json:"source_name"`
	SourceVersion string     `json:"source_version"`
	EvaluatedFact string     `json:"evaluated_fact"`
	PassCondition string     `json:"pass_condition"`
	Precondition  string     `json:"precondition"`
	EvidenceBasis string     `json:"evidence_basis"`
	Status        string     `json:"status"`
	CreatedBy     int64      `json:"created_by"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	PublishedBy   *int64     `json:"published_by,omitempty"`
	DisabledAt    *time.Time `json:"disabled_at,omitempty"`
	DisabledBy    *int64     `json:"disabled_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

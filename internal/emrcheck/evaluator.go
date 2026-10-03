package emrcheck

import (
	"time"
)

type binding struct {
	QualityRequirementID string
	Code                 string
	Name                 string
	RuleType             string
	ExecutionMode        string
	DeadlineAction       string
	QualityGroup         string
	EvaluatedFact        string
	PassCondition        string
	Precondition         string
	EvidenceBasis        string
	SourceVersion        string
	FieldRule            *ExecutableRule
}

type qualityRuleInput struct {
	QualityRequirementID string           `json:"quality_requirement_id"`
	Code                 string           `json:"rule_code"`
	Name                 string           `json:"rule_name"`
	FieldCode            string           `json:"field_code,omitempty"`
	FieldName            string           `json:"field_name,omitempty"`
	FactCode             string           `json:"fact_code,omitempty"`
	RelatedFields        []string         `json:"related_fields,omitempty"`
	LLMInstruction       string           `json:"llm_instruction,omitempty"`
	SkipIf               []RuleDependency `json:"skip_if,omitempty"`
	RuleType             string           `json:"rule_type"`
	QualityGroup         string           `json:"quality_group"`
	EvaluatedFact        string           `json:"evaluated_fact"`
	PassCondition        string           `json:"pass_condition"`
	Precondition         string           `json:"precondition"`
	EvidenceBasis        string           `json:"evidence_basis"`
	SourceVersion        string           `json:"source_version"`
	ExecutionMode        string           `json:"execution_mode"`
	DeadlineAction       string           `json:"deadline_action"`
}

type checkContext struct {
	RecordID          string
	SnapshotID        string
	TemplateVersionID string
	VisitType         string
	Specialty         string
	Content           map[string]any
	ICD10Matches      map[string]ICD10DiagnosisMatch
	ICD10Candidates   map[string][]ICD10DiagnosisMatch
	PatientContext    map[string]any
	EncounterContext  map[string]any
	RecordContext     map[string]any
	Status            string
	CreatedAt         *time.Time
	StartedAt         *time.Time
	EndedAt           *time.Time
	SubmittedAt       *time.Time
	ArchivedAt        *time.Time
	LastSavedAt       *time.Time
	UpdatedAt         *time.Time
	ConfirmedBy       *int64
}

type ICD10DiagnosisMatch struct {
	Code string
	Name string
}

type evaluatedResult struct {
	Conclusion       string
	CheckStatus      string
	ExecutionStatus  string
	IncompleteReason string
	HandlingResult   string
	MeetsDeadline    *bool
	Evidence         map[string]any
	Explanation      string
	Suggested        string
	ActualMode       string
}

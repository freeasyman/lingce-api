package complianceguard

import "time"

const (
	SubjectCommunication    = "communication"
	SubjectMedicalRecord    = "medical_record"
	SubjectContent          = "content"
	StatusNotConnected      = "not_connected"
	StatusWaitingSource     = "waiting_source"
	StatusWaitingTranscript = "waiting_transcription"
	StatusWaitingSnapshot   = "waiting_snapshot"
	StatusReady             = "ready"
	StatusUnavailable       = "unavailable"
)

type SourceStatus struct {
	Subject         string `json:"subject"`
	Label           string `json:"label"`
	Status          string `json:"status"`
	TotalSources    int64  `json:"total_sources"`
	ReadySources    int64  `json:"ready_sources"`
	AnalyzedSources int64  `json:"analyzed_sources"`
	SourceType      string `json:"source_type"`
	Message         string `json:"message"`
}

type SourceStatusResponse struct {
	TenantID    int64           `json:"tenant_id"`
	TenantName  string          `json:"tenant_name"`
	GeneratedAt time.Time       `json:"generated_at"`
	Items       []*SourceStatus `json:"items"`
}

type SourceMaterial struct {
	TenantID      int64      `json:"tenant_id"`
	Subject       string     `json:"subject"`
	SourceType    string     `json:"source_type"`
	SourceID      string     `json:"source_id"`
	SourceVersion string     `json:"source_version"`
	Status        string     `json:"status"`
	Ready         bool       `json:"ready"`
	EmployeeID    *int64     `json:"employee_id,omitempty"`
	EncounterID   *int64     `json:"encounter_id,omitempty"`
	Title         string     `json:"title"`
	OccurredAt    *time.Time `json:"occurred_at,omitempty"`
}

type SourceSyncResponse struct {
	TenantID      int64     `json:"tenant_id"`
	SyncedSources int64     `json:"synced_sources"`
	SyncedAt      time.Time `json:"synced_at"`
}

type Finding struct {
	ID                string    `json:"id"`
	TenantID          int64     `json:"tenant_id"`
	Subject           string    `json:"subject"`
	SourceRefID       string    `json:"source_ref_id"`
	AnalysisRunID     string    `json:"analysis_run_id"`
	CandidateRuleCode string    `json:"candidate_rule_code"`
	RiskName          string    `json:"risk_name"`
	Priority          string    `json:"priority"`
	Status            string    `json:"status"`
	FactSummary       string    `json:"fact_summary"`
	BasisSlice        string    `json:"basis_slice"`
	CreatedAt         time.Time `json:"created_at"`
	EvidenceCount     int64     `json:"evidence_count"`
	SourceType        string    `json:"source_type"`
	SourceID          string    `json:"source_id"`
	EmployeeID        *int64    `json:"employee_id,omitempty"`
	EncounterID       *int64    `json:"encounter_id,omitempty"`
	EmployeeName      string    `json:"employee_name,omitempty"`
	DepartmentName    string    `json:"department_name,omitempty"`
}

type FindingEvidence struct {
	ID              string         `json:"id"`
	EvidenceType    string         `json:"evidence_type"`
	Location        map[string]any `json:"location"`
	FactText        string         `json:"fact_text"`
	ImmutableDigest string         `json:"immutable_digest"`
}

type FindingDetail struct {
	Finding
	Evidence         []*FindingEvidence `json:"evidence"`
	SourceVersion    string             `json:"source_version"`
	SourceStatus     string             `json:"source_status"`
	InputFingerprint string             `json:"input_fingerprint"`
	StrategyCode     string             `json:"strategy_code"`
	StrategyVersion  string             `json:"strategy_version"`
}

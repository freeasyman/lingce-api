package compliance

import (
	"encoding/json"
	"time"
)

type JSONMap map[string]any

type RuleExamples struct {
	Risky []string `json:"risky"`
	Safe  []string `json:"safe"`
}

type Rule struct {
	ID              string       `json:"id"`
	TenantID        int64        `json:"tenant_id"`
	Code            string       `json:"code"`
	Name            string       `json:"name"`
	Category        string       `json:"category"`
	Scope           string       `json:"scope"`
	Enabled         bool         `json:"enabled"`
	Severity        string       `json:"severity"`
	IsBuiltIn       bool         `json:"isBuiltIn"`
	TriggerType     string       `json:"triggerType"`
	Conditions      JSONMap      `json:"conditions"`
	Description     string       `json:"description"`
	LegalBasis      string       `json:"legalBasis"`
	SuggestedScript string       `json:"suggestedScript"`
	Examples        RuleExamples `json:"examples"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

type ComplianceEvent struct {
	ID             string          `json:"id"`
	TenantID       int64           `json:"tenant_id"`
	EncounterID    int64           `json:"encounter_id"`
	RuleCode       string          `json:"ruleCode,omitempty"`
	QuoteHash      string          `json:"quoteHash,omitempty"`
	Source         string          `json:"source"`
	Severity       string          `json:"severity"`
	Type           string          `json:"type"`
	Status         string          `json:"status"`
	Timestamp      time.Time       `json:"timestamp"`
	EmployeeName   string          `json:"employeeName"`
	EmployeeRole   string          `json:"employeeRole"`
	Department     string          `json:"department"`
	PatientName    string          `json:"patientName,omitempty"`
	PatientMeta    string          `json:"patientMeta,omitempty"`
	ContentTitle   string          `json:"contentTitle,omitempty"`
	ContentType    string          `json:"contentType,omitempty"`
	Quote          string          `json:"quote"`
	Summary        string          `json:"summary"`
	Advice         string          `json:"advice"`
	LegalBasis     string          `json:"legalBasis"`
	EvidenceAt     string          `json:"evidenceAt,omitempty"`
	Transcript     json.RawMessage `json:"transcript"`
	RelatedActions []string        `json:"relatedActions"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

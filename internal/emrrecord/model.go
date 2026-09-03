package emrrecord

import (
	"time"

	"github.com/freeasyman/lingce-api/internal/emrcheck"
)

type Record struct {
	ID                  string         `json:"id"`
	TenantID            int64          `json:"tenant_id"`
	EncounterID         *int64         `json:"encounter_id,omitempty"`
	PatientID           *int64         `json:"patient_id,omitempty"`
	PatientSnapshot     map[string]any `json:"patient_snapshot"`
	TemplateVersionID   string         `json:"template_version_id"`
	DocumentType        string         `json:"document_type"`
	VisitType           string         `json:"visit_type"`
	DepartmentID        *int64         `json:"department_id,omitempty"`
	DoctorID            int64          `json:"doctor_id"`
	SpecialtyModule     *string        `json:"specialty_module,omitempty"`
	StartedAt           time.Time      `json:"started_at"`
	EndedAt             *time.Time     `json:"ended_at,omitempty"`
	EncounterContext    map[string]any `json:"encounter_context"`
	SourceReferences    map[string]any `json:"source_references"`
	WorkingContent      map[string]any `json:"working_content"`
	WorkingDigest       string         `json:"working_digest"`
	CurrentSnapshotID   *string        `json:"current_snapshot_id,omitempty"`
	Status              string         `json:"status"`
	RevisionNo          int            `json:"revision_no"`
	ConfirmedBy         *int64         `json:"confirmed_by,omitempty"`
	ConfirmedAt         *time.Time     `json:"confirmed_at,omitempty"`
	ConfirmedSnapshotID *string        `json:"confirmed_snapshot_id,omitempty"`
	ArchivedSnapshotID  *string        `json:"archived_snapshot_id,omitempty"`
	SubmittedAt         *time.Time     `json:"submitted_at,omitempty"`
	ArchivedAt          *time.Time     `json:"archived_at,omitempty"`
	VoidedAt            *time.Time     `json:"voided_at,omitempty"`
	CreatedBy           int64          `json:"created_by"`
	CreatedAt           time.Time      `json:"created_at"`
	LastSavedAt         time.Time      `json:"last_saved_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type Snapshot struct {
	ID                string         `json:"id"`
	RecordID          string         `json:"record_id"`
	RevisionNo        int            `json:"revision_no"`
	SnapshotNo        int            `json:"snapshot_no"`
	TemplateVersionID string         `json:"template_version_id"`
	PatientSnapshot   map[string]any `json:"patient_snapshot"`
	DocumentContext   map[string]any `json:"document_context"`
	Content           map[string]any `json:"content"`
	ContentDigest     string         `json:"content_digest"`
	FormedBy          int64          `json:"formed_by"`
	FormedAt          time.Time      `json:"formed_at"`
}

type AICandidate struct {
	ID             string         `json:"id"`
	RecordID       string         `json:"record_id"`
	SectionCode    string         `json:"section_code"`
	Content        map[string]any `json:"content"`
	SourceEvidence map[string]any `json:"source_evidence"`
	Status         string         `json:"status"`
	GeneratedAt    time.Time      `json:"generated_at"`
	HandledAt      *time.Time     `json:"handled_at,omitempty"`
	HandledBy      *int64         `json:"handled_by,omitempty"`
}

type CreateRequest struct {
	EncounterID       *int64         `json:"encounter_id,omitempty"`
	PatientID         *int64         `json:"patient_id,omitempty"`
	PatientSnapshot   map[string]any `json:"patient_snapshot"`
	TemplateVersionID string         `json:"template_version_id"`
	DocumentType      string         `json:"document_type"`
	VisitType         string         `json:"visit_type"`
	DepartmentID      *int64         `json:"department_id,omitempty"`
	DoctorID          *int64         `json:"doctor_id,omitempty"`
	SpecialtyModule   *string        `json:"specialty_module,omitempty"`
	StartedAt         *time.Time     `json:"started_at,omitempty"`
	EndedAt           *time.Time     `json:"ended_at,omitempty"`
	EncounterContext  map[string]any `json:"encounter_context"`
	SourceReferences  map[string]any `json:"source_references"`
	Content           map[string]any `json:"content"`
}

type ContentRequest struct {
	Content map[string]any `json:"content"`
}

type ActionRequest struct {
	Content map[string]any `json:"content,omitempty"`
	Note    string         `json:"note,omitempty"`
}

type CreateAICandidateRequest struct {
	SectionCode    string         `json:"section_code"`
	Content        map[string]any `json:"content"`
	SourceEvidence map[string]any `json:"source_evidence"`
}

type GenerateAICandidatesRequest struct {
	TenantID      int64                      `json:"tenant_id"`
	RecordingID   int64                      `json:"recording_id"`
	GenerationKey string                     `json:"generation_key"`
	Candidates    []CreateAICandidateRequest `json:"candidates"`
}

type GenerateAICandidatesOutcome struct {
	Record     *Record        `json:"record"`
	Candidates []*AICandidate `json:"candidates"`
}

type GenerateRealtimeAICandidatesRequest struct {
	TenantID      int64                      `json:"tenant_id"`
	RecordID      string                     `json:"record_id"`
	EncounterID   int64                      `json:"encounter_id"`
	GenerationKey string                     `json:"generation_key"`
	Candidates    []CreateAICandidateRequest `json:"candidates"`
}

type HandleAICandidateRequest struct {
	Content map[string]any `json:"content,omitempty"`
	Note    string         `json:"note,omitempty"`
}

type WriteOutcome struct {
	Record         *Record
	Snapshot       *Snapshot
	CheckRun       *emrcheck.CheckRun `json:"check_run,omitempty"`
	BeforeContent  map[string]any
	BeforeStatus   string
	AfterStatus    string
	BeforeSnapshot *string
	CheckRunID     *string
}

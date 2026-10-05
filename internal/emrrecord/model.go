package emrrecord

import (
	"time"

	"github.com/freeasyman/lingce-api/internal/emrquality"
)

type Record struct {
	ID                  string         `json:"id"`
	TenantID            int64          `json:"tenant_id"`
	EncounterID         int64          `json:"encounter_id"`
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
	CurrentScoreStatus  string         `json:"current_score_status,omitempty"`
	CurrentTotalScore   *int           `json:"current_total_score,omitempty"`
	CurrentGrade        string         `json:"current_grade,omitempty"`
	CurrentScoreVersion string         `json:"current_score_version,omitempty"`
	CurrentQualityRunID *string        `json:"current_quality_run_id,omitempty"`
}

type Snapshot struct {
	ID                string         `json:"id"`
	RecordID          string         `json:"record_id"`
	EncounterID       int64          `json:"encounter_id"`
	RevisionNo        int            `json:"revision_no"`
	SnapshotNo        int            `json:"snapshot_no"`
	TemplateVersionID string         `json:"template_version_id"`
	PatientSnapshot   map[string]any `json:"patient_snapshot"`
	DocumentContext   map[string]any `json:"document_context"`
	Content           map[string]any `json:"content"`
	ContentDigest     string         `json:"content_digest"`
	FormedBy          int64          `json:"formed_by"`
	FormedAt          time.Time      `json:"formed_at"`
	ScoreStatus       string         `json:"score_status,omitempty"`
	TotalScore        *int           `json:"total_score,omitempty"`
	Grade             string         `json:"grade,omitempty"`
	ScoreVersion      string         `json:"score_version,omitempty"`
	QualityRunID      *string        `json:"quality_run_id,omitempty"`
}

type GenerateRequest struct {
	GenerationKey string         `json:"generation_key"`
	Input         map[string]any `json:"input"`
	SourceKind    string         `json:"source_kind,omitempty"`
	SourceID      string         `json:"source_id,omitempty"`
	PromptCode    string         `json:"prompt_code,omitempty"`
}

type CreateRequest struct {
	EncounterID       int64          `json:"encounter_id"`
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
	StandardInput     map[string]any `json:"standard_input,omitempty"`
}

type ContentRequest struct {
	Content         map[string]any `json:"content"`
	PatientSnapshot map[string]any `json:"patient_snapshot,omitempty"`
}

type ActionRequest struct {
	Content          map[string]any `json:"content,omitempty"`
	PatientSnapshot  map[string]any `json:"patient_snapshot,omitempty"`
	Note             string         `json:"note,omitempty"`
	AllowNonBlocking bool           `json:"allow_non_blocking,omitempty"`
	QualityRunID     string         `json:"quality_run_id,omitempty"`
}

type WriteOutcome struct {
	Record         *Record
	Snapshot       *Snapshot
	BeforeContent  map[string]any
	BeforeStatus   string
	AfterStatus    string
	BeforeSnapshot *string
	BeforePatient  map[string]any
	QualityReport  *emrquality.Report `json:"quality,omitempty"`
}

type GeneratedWorkingDraftRequest struct {
	EncounterID      int64
	CustomerID       *int64
	PatientSnapshot  map[string]any
	TemplateID       string
	DocumentType     string
	VisitType        string
	DepartmentID     *int64
	DoctorID         int64
	StartedAt        time.Time
	EndedAt          *time.Time
	EncounterContext map[string]any
	SourceReferences map[string]any
	Content          map[string]any
	CreatedBy        int64
}

type GeneratedWorkingDraftOutcome struct {
	Record       *Record
	Created      bool
	BeforeStatus string
	AfterStatus  string
}

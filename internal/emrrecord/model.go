package emrrecord

import "time"

type OutpatientRecord struct {
	ID                  int64                  `json:"id"`
	TenantID            int64                  `json:"tenant_id"`
	CustomerID          *int64                 `json:"customer_id,omitempty"`
	RecordingID         *int64                 `json:"recording_id,omitempty"`
	DoctorEmployeeID    *int64                 `json:"doctor_employee_id,omitempty"`
	DoctorEmployeeName  string                 `json:"doctor_employee_name,omitempty"`
	DepartmentID        *int64                 `json:"department_id,omitempty"`
	DepartmentName      string                 `json:"department_name,omitempty"`
	TemplateCode        string                 `json:"template_code"`
	TemplateName        string                 `json:"template_name"`
	Status              string                 `json:"status"`
	SourceType          string                 `json:"source_type"`
	SourceID            *int64                 `json:"source_id,omitempty"`
	EncounteredAt       *time.Time             `json:"encountered_at,omitempty"`
	ChiefComplaint      string                 `json:"chief_complaint,omitempty"`
	DiagnosisSummary    string                 `json:"diagnosis_summary,omitempty"`
	PatientName         string                 `json:"patient_name,omitempty"`
	PatientGender       string                 `json:"patient_gender,omitempty"`
	PatientAgeText      string                 `json:"patient_age_text,omitempty"`
	PatientPhone        string                 `json:"patient_phone,omitempty"`
	PatientSnapshotJSON map[string]interface{} `json:"patient_snapshot_json,omitempty"`
	DocumentJSON        map[string]interface{} `json:"document_json,omitempty"`
	MissingFields       []string               `json:"missing_fields,omitempty"`
	ImportedAt          *time.Time             `json:"imported_at,omitempty"`
	ArchivedAt          *time.Time             `json:"archived_at,omitempty"`
	CreatedBy           *int64                 `json:"created_by,omitempty"`
	UpdatedBy           *int64                 `json:"updated_by,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

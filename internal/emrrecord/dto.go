package emrrecord

type ListRecordsRequest struct {
	TenantID     int64
	EmployeeID   int64
	DepartmentID *int64
	Scope        string
	Keyword      string
	Status       string
	Page         int
	PageSize     int
}

type RecordListItem struct {
	ID              int64              `json:"id"`
	RecordDate      string             `json:"record_date"`
	StartedAt       string             `json:"started_at"`
	PatientID       string             `json:"patient_id"`
	PatientName     string             `json:"patient_name"`
	PatientMeta     string             `json:"patient_meta"`
	TemplateName    string             `json:"template_name"`
	ChiefComplaint  string             `json:"chief_complaint"`
	Diagnosis       string             `json:"diagnosis"`
	Doctor          string             `json:"doctor"`
	Department      string             `json:"department"`
	Status          string             `json:"status"`
	Risks           []string           `json:"risks"`
	UpdatedAt       string             `json:"updated_at"`
	ImportSource    string             `json:"import_source,omitempty"`
	PatientSnapshot PatientSnapshotDTO `json:"patient_snapshot"`
}

type PatientSnapshotDTO struct {
	Name           string `json:"name"`
	Gender         string `json:"gender"`
	BirthDate      string `json:"birth_date"`
	AgeText        string `json:"age_text"`
	Address        string `json:"address"`
	Phone          string `json:"phone"`
	Ethnicity      string `json:"ethnicity"`
	MaritalStatus  string `json:"marital_status"`
	Occupation     string `json:"occupation"`
	AllergyHistory string `json:"allergy_history"`
}

type ImportRecordingDraftsRequest struct {
	RecordingIDs []int64 `json:"recording_ids"`
}

type ImportRecordingDraftsResponse struct {
	ImportedCount int64   `json:"imported_count"`
	RecordIDs     []int64 `json:"record_ids"`
}

type SaveRecordRequest struct {
	DocumentJSON  map[string]any `json:"document_json"`
	ChangeReason  string         `json:"change_reason"`
}

type SubmitRecordRequest struct {
	DocumentJSON map[string]any `json:"document_json,omitempty"`
	ChangeReason string         `json:"change_reason"`
}

type ArchiveRecordRequest struct {
	ConfirmationNote string         `json:"confirmation_note"`
	DocumentJSON     map[string]any `json:"document_json,omitempty"`
}

type RecordWriteResponse struct {
	Record  *RecordDetailResponse `json:"record"`
	Version *RecordVersionDTO     `json:"version,omitempty"`
}

type RecordVersionDTO struct {
	VersionID     int64          `json:"version_id"`
	VersionNo     int            `json:"version_no"`
	VersionKind   string         `json:"version_kind"`
	Status        string         `json:"status"`
	ChangeSummary string         `json:"change_summary"`
	Reason        string         `json:"reason"`
	OperatorID    *int64         `json:"operator_id,omitempty"`
	OperatorName  string         `json:"operator_name,omitempty"`
	CreatedAt     string         `json:"created_at"`
	DocumentJSON  map[string]any `json:"document_json"`
	SnapshotJSON  map[string]any `json:"snapshot_json"`
}

type RecordAuditEventDTO struct {
	EventID      int64  `json:"event_id"`
	FieldKey     string `json:"field_key"`
	ActionType   string `json:"action_type"`
	BeforeValue  string `json:"before_value"`
	AfterValue   string `json:"after_value"`
	ChangeReason string `json:"change_reason"`
	OperatorID   *int64 `json:"operator_id,omitempty"`
	OperatorName string `json:"operator_name,omitempty"`
	CreatedAt    string `json:"created_at"`
}

type RecordDetailResponse struct {
	RecordID          int64              `json:"record_id"`
	TenantID          int64              `json:"tenant_id"`
	PatientSnapshot   PatientSnapshotDTO `json:"patient_snapshot"`
	DocumentJSON      map[string]any     `json:"document_json"`
	SourcePayloadJSON map[string]any     `json:"source_payload_json"`
	Doctor            string             `json:"doctor"`
	Department        string             `json:"department"`
	TemplateName      string             `json:"template_name"`
	ChiefComplaint    string             `json:"chief_complaint"`
	Diagnosis         string             `json:"diagnosis"`
	RecordDate        string             `json:"record_date"`
	StartedAt         string             `json:"started_at"`
	Status            string             `json:"status"`
	UpdatedAt         string             `json:"updated_at"`
}

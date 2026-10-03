package emrcheck

import "time"

type QualityManagementFilter struct {
	DateFrom     *time.Time
	DateTo       *time.Time
	DepartmentID *int64
	DoctorID     *int64
	Status       string
	Scope        string
	Limit        int
	Offset       int
}

type QualityManagementSummary struct {
	RecordTotal          int        `json:"record_total"`
	CheckedRecordTotal   int        `json:"checked_record_total"`
	ProblemRecordTotal   int        `json:"problem_record_total"`
	BlockingResultTotal  int        `json:"blocking_result_total"`
	FailedCheckTotal     int        `json:"failed_check_total"`
	RunningCheckTotal    int        `json:"running_check_total"`
	AttentionResultTotal int        `json:"attention_result_total"`
	LastUpdatedAt        *time.Time `json:"last_updated_at,omitempty"`
}

type QualityFindingGroup struct {
	GroupKey        string                        `json:"group_key"`
	FieldCode       string                        `json:"field_code"`
	FieldName       string                        `json:"field_name"`
	RuleCode        string                        `json:"rule_code"`
	Title           string                        `json:"title"`
	Severity        string                        `json:"severity"`
	RecordCount     int                           `json:"record_count"`
	ResultCount     int                           `json:"result_count"`
	BlockingCount   int                           `json:"blocking_count"`
	LatestCheckedAt *time.Time                    `json:"latest_checked_at,omitempty"`
	SampleRecords   []*QualityFindingRecordSample `json:"sample_records"`
}

type QualityFindingRecordSample struct {
	RecordID       string     `json:"record_id"`
	ResultID       string     `json:"result_id"`
	PatientName    string     `json:"patient_name"`
	DoctorID       int64      `json:"doctor_id"`
	DepartmentID   *int64     `json:"department_id,omitempty"`
	RecordStatus   string     `json:"record_status"`
	FieldCode      string     `json:"field_code"`
	FieldName      string     `json:"field_name"`
	Conclusion     string     `json:"conclusion"`
	HandlingResult string     `json:"handling_result"`
	Message        string     `json:"message"`
	CheckedAt      *time.Time `json:"checked_at,omitempty"`
}

type QualityFindingGroupList struct {
	Items []*QualityFindingGroup `json:"items"`
	Total int                    `json:"total"`
}

type QualityFindingRecordList struct {
	GroupKey string                        `json:"group_key"`
	Title    string                        `json:"title"`
	Items    []*QualityFindingRecordSample `json:"items"`
	Total    int                           `json:"total"`
}

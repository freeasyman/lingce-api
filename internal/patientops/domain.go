package patientops

// 一期领域枚举的唯一后端定义。数据库值和前端 domain.ts 必须保持一致。
const (
	SourceScreenshot    = "screenshot"
	SourceAudio         = "audio"
	SourceActivity      = "activity"
	SourceManual        = "manual"
	SourceBusinessEvent = "business_event"
	SourceSystemRule    = "system_rule"

	RecordScreenshot = "screenshot"
	RecordAudio      = "audio"
	RecordEmployee   = "employee"
	RecordPatient    = "patient"

	InsightConcern       = "concern"
	InsightDecisionMaker = "decision_maker"
	InsightQuestion      = "patient_question"
	InsightCommitment    = "commitment"
	InsightCollaboration = "collaboration"

	TaskStatusPendingConfirmation = "待确认"
	TaskStatusPending             = "待处理"
	TaskStatusProcessing          = "处理中"
	TaskStatusWaitingPatient      = "等待患者"
	TaskStatusWaitingCollab       = "等待内部协作"
	TaskStatusCompleted           = "已完成"
	TaskStatusRescheduled         = "已改期"
	TaskStatusCancelled           = "已取消"
	TaskStatusPaused              = "暂停联系"
)

var TaskTypes = []string{"接住新咨询", "患者问题处理", "咨询推进", "到院后促成", "家属 / 决策人确认", "员工承诺跟进", "专业协作", "投诉和异常", "人工新增", "活动触达"}
var TaskResults = []string{"已回复", "患者有回应", "已预约", "已购买", "需要家属商量", "需要医生 / 科室回答", "暂不考虑", "暂停联系", "改约下次联系", "明确拒绝"}

type CustomerLink struct {
	ID          int64   `json:"id,omitempty"`
	SourceType  string  `json:"source_type"`
	SourceID    int64   `json:"source_id"`
	CustomerID  *int64  `json:"customer_id,omitempty"`
	Status      string  `json:"status"`
	MatchReason string  `json:"match_reason,omitempty"`
	CorrectedBy *int64  `json:"corrected_by,omitempty"`
	CorrectedAt *string `json:"corrected_at,omitempty"`
}

type SourceOrigin struct {
	SourceType string `json:"source_type"`
	SourceID   int64  `json:"source_id"`
	Evidence   string `json:"evidence,omitempty"`
}

type ConversationSource struct {
	ID           int64  `json:"id"`
	Type         string `json:"type"`
	OriginalName string `json:"original_name,omitempty"`
	OriginalURL  string `json:"original_url,omitempty"`
	CapturedAt   string `json:"captured_at"`
	CreatedBy    *int64 `json:"created_by,omitempty"`
	CustomerID   *int64 `json:"customer_id,omitempty"`
	MatchStatus  string `json:"match_status"`
}

type CommunicationRecord struct {
	ID               int64         `json:"id"`
	CustomerID       *int64        `json:"customer_id,omitempty"`
	Type             string        `json:"type"`
	Title            string        `json:"title"`
	Content          string        `json:"content"`
	PatientUtterance string        `json:"patient_utterance,omitempty"`
	Origin           *SourceOrigin `json:"origin,omitempty"`
	Status           string        `json:"status"`
	CreatedAt        string        `json:"created_at"`
	LinkedTaskID     *int64        `json:"linked_task_id,omitempty"`
}

type Insight struct {
	ID          int64        `json:"id"`
	CustomerID  *int64       `json:"customer_id,omitempty"`
	Type        string       `json:"type"`
	Content     string       `json:"content"`
	Evidence    string       `json:"evidence"`
	Confidence  *float64     `json:"confidence,omitempty"`
	Origin      SourceOrigin `json:"origin"`
	Status      string       `json:"status"`
	ConfirmedBy *int64       `json:"confirmed_by,omitempty"`
	ConfirmedAt *string      `json:"confirmed_at,omitempty"`
}

type Task struct {
	ID                 int64        `json:"id"`
	Type               string       `json:"type"`
	Subtype            string       `json:"subtype,omitempty"`
	CustomerID         int64        `json:"customer_id"`
	ActivityID         *int64       `json:"activity_id,omitempty"`
	Origin             SourceOrigin `json:"origin"`
	Reason             string       `json:"reason"`
	OperationalGoal    string       `json:"operational_goal"`
	Action             string       `json:"action"`
	Content            string       `json:"content,omitempty"`
	ProductID          *int64       `json:"product_id,omitempty"`
	OwnerID            *int64       `json:"owner_id,omitempty"`
	DueAt              *string      `json:"due_at,omitempty"`
	Priority           string       `json:"priority"`
	ContactRestriction string       `json:"contact_restriction,omitempty"`
	Status             string       `json:"status"`
	Result             string       `json:"result,omitempty"`
	ResultNote         string       `json:"result_note,omitempty"`
	NextAction         string       `json:"next_action,omitempty"`
	NextDueAt          *string      `json:"next_due_at,omitempty"`
	DedupeKey          string       `json:"dedupe_key"`
	CreatedBy          *int64       `json:"created_by,omitempty"`
	CompletedAt        *string      `json:"completed_at,omitempty"`
}

package complianceguard

import (
	"encoding/json"
	"time"
)

type AnalyzeTextRequest struct {
	TenantID      int64  `json:"tenant_id"`
	SceneCode     string `json:"scene_code"`
	SourceType    string `json:"source_type"`
	SourceID      string `json:"source_id"`
	SourceVersion string `json:"source_version"`
	Text          string `json:"text"`
	SampleID      string `json:"sample_id,omitempty"`
}

// CommunicationCheckRequest 是 Worker 提交给合规卫士的正式沟通检查请求。
//
// Worker 只提交已经完成的录音素材和客观关联信息；提示词、规则集、
// 模型配置和结果入库均由合规卫士 API 负责。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
type CommunicationCheckRequest struct {
	TenantID          int64  `json:"tenant_id"`
	RecordingID       int64  `json:"recording_id"`
	EncounterID       int64  `json:"encounter_id"`
	EmployeeID        int64  `json:"employee_id"`
	CustomerID        int64  `json:"customer_id"`
	CustomerName      string `json:"customer_name"`
	EmployeeName      string `json:"employee_name"`
	DoctorName        string `json:"doctor_name"`
	BusinessScope     string `json:"business_scope"`
	RoleCode          string `json:"role_code"`
	RecordedAt        string `json:"recorded_at"`
	CleanedTranscript string `json:"cleaned_transcript"`
	// Empty values keep existing Worker deliveries compatible. A rerun key is
	// supplied once per intentional rerun and reused when that delivery retries.
	TriggerReason string `json:"trigger_reason,omitempty"`
	RerunKey      string `json:"rerun_key,omitempty"`
}

// CommunicationCheckResponse 是 API 对 Worker 的接收确认。
//
// accepted 只表示请求已通过同步校验并持久化，不表示模型已经完成。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
type CommunicationCheckResponse struct {
	Code           string `json:"code"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	RequestID      string `json:"request_id"`
	RunID          string `json:"run_id"`
	TenantID       int64  `json:"tenant_id"`
	RecordingID    int64  `json:"recording_id"`
	EncounterID    int64  `json:"encounter_id"`
	EmployeeID     int64  `json:"employee_id"`
	CustomerID     int64  `json:"customer_id"`
	RuleSetCode    string `json:"rule_set_code"`
	RuleSetVersion string `json:"rule_set_version"`
	PromptCode     string `json:"prompt_code"`
	PromptVersion  string `json:"prompt_version"`
	ModelConfigID  int64  `json:"model_config_id"`
	ModelCode      string `json:"model_code"`
	RawResponse    string `json:"raw_response"`
	Findings       []any  `json:"findings"`
}

type AnalyzeTextResponse struct {
	RunID          string    `json:"run_id"`
	Status         string    `json:"status"`
	PromptCode     string    `json:"prompt_code,omitempty"`
	PromptVersion  string    `json:"prompt_version,omitempty"`
	ModelCode      string    `json:"model_code,omitempty"`
	RuleSetCode    string    `json:"rule_set_code,omitempty"`
	RuleSetVersion string    `json:"rule_set_version,omitempty"`
	RawResponse    string    `json:"raw_response,omitempty"`
	Findings       []any     `json:"findings,omitempty"`
	Error          string    `json:"error,omitempty"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
}

// FindingListItem 是合规卫士列表页需要的最小业务数据。
//
// 分析运行 ID、模型配置和提示词编码属于技术记录，不放入列表主数据。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
type FindingListItem struct {
	ID               string     `json:"id"`
	TenantID         int64      `json:"tenant_id"`
	SourceType       string     `json:"source_type"`
	SourceID         string     `json:"source_id"`
	SourceVersion    string     `json:"source_version"`
	Subject          string     `json:"subject"`
	SceneCode        string     `json:"scene_code"`
	RoleCode         string     `json:"role_code"`
	EmployeeID       *int64     `json:"employee_id,omitempty"`
	EmployeeName     string     `json:"employee_name"`
	CustomerName     string     `json:"customer_name"`
	Department       string     `json:"department"`
	RuleCode         string     `json:"rule_code"`
	RuleName         string     `json:"rule_name"`
	RuleVersion      string     `json:"rule_version"`
	Fact             string     `json:"fact"`
	Summary          string     `json:"summary"`
	NeedsReview      bool       `json:"needs_review"`
	Status           string     `json:"status"`
	ReviewConclusion string     `json:"review_conclusion,omitempty"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// FindingDetail 是待核查和已核查共用的发现详情。
//
// evidence 和 missing_facts 保持 JSON 数组结构，确保原始证据不被接口层改写。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
type FindingDetail struct {
	FindingListItem
	Reason           string          `json:"reason"`
	Evidence         json.RawMessage `json:"evidence"`
	MissingFacts     json.RawMessage `json:"missing_facts"`
	ReviewConclusion string          `json:"review_conclusion"`
	ReviewReason     string          `json:"review_reason"`
	ReviewedBy       *int64          `json:"reviewed_by,omitempty"`
	ReviewedAt       *time.Time      `json:"reviewed_at,omitempty"`
	Rule             FindingRule     `json:"rule"`
	Source           FindingSource   `json:"source"`
}

// ReviewFindingRequest 是人工核查发现时提交的结论和说明。
//
// review_conclusion 只允许 confirmed、not_confirmed、unable_to_determine；
// 这三个值分别对应成立、不成立、无法判断。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
type ReviewFindingRequest struct {
	ReviewConclusion string `json:"review_conclusion"`
	ReviewReason     string `json:"review_reason"`
}

// ReviewFindingResponse 返回核查后的最小状态，详情页据此更新显示。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
type ReviewFindingResponse struct {
	ID               string    `json:"id"`
	TenantID         int64     `json:"tenant_id"`
	Status           string    `json:"status"`
	ReviewConclusion string    `json:"review_conclusion"`
	ReviewReason     string    `json:"review_reason"`
	ReviewedBy       int64     `json:"reviewed_by"`
	ReviewedAt       time.Time `json:"reviewed_at"`
}

type FindingRule struct {
	Code             string `json:"code"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	JudgmentStandard string `json:"judgment_standard"`
}

type FindingSource struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	Version      string `json:"version"`
	Subject      string `json:"subject"`
	SceneCode    string `json:"scene_code"`
	RoleCode     string `json:"role_code"`
	EmployeeID   *int64 `json:"employee_id,omitempty"`
	EmployeeName string `json:"employee_name"`
	Department   string `json:"department"`
}

type CheckItemListItem struct {
	ID               string                   `json:"id"`
	TenantID         int64                    `json:"tenant_id"`
	SourceType       string                   `json:"source_type"`
	SourceID         string                   `json:"source_id"`
	SourceVersion    string                   `json:"source_version"`
	SceneCode        string                   `json:"scene_code"`
	RoleCode         string                   `json:"role_code"`
	EmployeeID       *int64                   `json:"employee_id,omitempty"`
	ThemeKey         string                   `json:"theme_key"`
	Question         string                   `json:"question"`
	RiskHypothesis   string                   `json:"risk_hypothesis"`
	PrimaryRiskType  string                   `json:"primary_risk_type"`
	RelatedRiskTypes json.RawMessage          `json:"related_risk_types"`
	Evidence         json.RawMessage          `json:"evidence"`
	CheckBasis       json.RawMessage          `json:"check_basis"`
	SuggestedActions json.RawMessage          `json:"suggested_actions"`
	Priority         string                   `json:"priority"`
	Status           string                   `json:"status"`
	Conclusion       string                   `json:"conclusion,omitempty"`
	ProcessReason    string                   `json:"process_reason,omitempty"`
	ProcessActions   json.RawMessage          `json:"process_actions,omitempty"`
	ProcessNote      string                   `json:"process_note,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
	ProcessRecords   []CheckItemProcessRecord `json:"process_records,omitempty"`
}
type CheckItemProcessRequest struct {
	Conclusion     string   `json:"conclusion"`
	Reason         string   `json:"reason"`
	Actions        []string `json:"actions"`
	Note           string   `json:"note"`
	IdempotencyKey string   `json:"idempotency_key"`
}

type CheckItemProcessRecord struct {
	ID         string          `json:"id"`
	ActorID    int64           `json:"actor_id"`
	Conclusion string          `json:"conclusion"`
	Reason     string          `json:"reason"`
	Actions    json.RawMessage `json:"actions"`
	Note       string          `json:"note"`
	CreatedAt  time.Time       `json:"created_at"`
}

// OverviewBucket 是全院情况中一个聚合维度的数量。
//
// 统计只来自 compliance_findings；没有产生发现的执行记录不进入概览。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
type OverviewBucket struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// ComplianceOverview 是全院情况页面使用的最小统计模型。
//
// 不提供“合规率”、评分或趋势等没有确定口径的指标，避免 MVP 阶段
// 把全院情况扩展成复杂报表。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
type ComplianceOverview struct {
	PendingCount    int64               `json:"pending_count"`
	ReviewedCount   int64               `json:"reviewed_count"`
	ConfirmedCount  int64               `json:"confirmed_count"`
	ByScene         []OverviewBucket    `json:"by_scene"`
	ByDepartment    []OverviewBucket    `json:"by_department"`
	ByEmployee      []OverviewBucket    `json:"by_employee"`
	ByRule          []OverviewBucket    `json:"by_rule"`
	RecentPending   []CheckItemListItem `json:"recent_pending"`
	RecentReviewed  []CheckItemListItem `json:"recent_reviewed"`
	RecentConfirmed []CheckItemListItem `json:"recent_confirmed"`
}

// ComplianceRule 是检查标准页面使用的规则定义。
//
// definition 保留规则的完整扩展字段；常用字段单独返回，方便列表筛选、
// 编辑和后续查询。版本字段属于规则身份的一部分，不能被静默覆盖。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
type ComplianceRule struct {
	ID               string          `json:"id"`
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	Category         string          `json:"category"`
	Scope            string          `json:"scope"`
	Enabled          bool            `json:"enabled"`
	IsBuiltIn        bool            `json:"is_built_in"`
	Severity         string          `json:"severity"`
	TriggerType      string          `json:"trigger_type"`
	Conditions       json.RawMessage `json:"conditions"`
	Description      string          `json:"description"`
	LegalBasis       string          `json:"legal_basis"`
	SuggestedScript  string          `json:"suggested_script"`
	Examples         json.RawMessage `json:"examples"`
	Version          string          `json:"version"`
	Status           string          `json:"status"`
	SceneCodes       json.RawMessage `json:"scene_codes"`
	RoleCodes        json.RawMessage `json:"role_codes"`
	JudgmentStandard string          `json:"judgment_standard"`
	Definition       json.RawMessage `json:"definition"`
	InUse            bool            `json:"in_use"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// ComplianceRuleRequest 是新增规则、修改未使用规则和新建规则版本的共同输入。
//
// version 为空时由服务端生成；scene_codes 和 role_codes 为空时根据 scope
// 使用沟通规则的默认范围，避免前端必须理解数据库内部结构。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-14
type ComplianceRuleRequest struct {
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	Category         string          `json:"category"`
	Scope            string          `json:"scope"`
	Enabled          *bool           `json:"enabled,omitempty"`
	Severity         string          `json:"severity"`
	TriggerType      string          `json:"trigger_type"`
	Conditions       json.RawMessage `json:"conditions"`
	Description      string          `json:"description"`
	LegalBasis       string          `json:"legal_basis"`
	SuggestedScript  string          `json:"suggested_script"`
	Examples         json.RawMessage `json:"examples"`
	Version          string          `json:"version,omitempty"`
	SceneCodes       json.RawMessage `json:"scene_codes,omitempty"`
	RoleCodes        json.RawMessage `json:"role_codes,omitempty"`
	JudgmentStandard string          `json:"judgment_standard,omitempty"`
	Definition       json.RawMessage `json:"definition,omitempty"`
}

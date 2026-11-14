package emrquality

import "time"

type Action string

const (
	ActionSave     Action = "save"
	ActionComplete Action = "complete"
	ActionArchive  Action = "archive"
)

type Context struct {
	TenantID     int64
	RecordID     string
	SnapshotID   *string
	Action       Action
	DocumentType string
	VisitType    string
	TemplateCode string
	Patient      map[string]any
	Content      map[string]any
	Encounter    map[string]any
	DepartmentID *int64
	StartedAt    time.Time
	DoctorID     int64
	CreatedBy    int64
	History      []map[string]any `json:"history,omitempty"`
	// ProgramOnly 仅执行纯代码规则（不调用 LLM），供 HIS 免费版等零 AI 成本场景使用。
	ProgramOnly bool
}

type Evidence struct {
	Source  string `json:"source"`
	Path    string `json:"path"`
	Excerpt string `json:"excerpt"`
}

type RuleResult struct {
	RuleCode      string     `json:"rule_code"`
	SectionCode   string     `json:"section_code"`
	Result        string     `json:"result"`
	ExecutionKind string     `json:"execution_kind"`
	Evidence      []Evidence `json:"evidence"`
	Reason        string     `json:"reason"`
	Deduction     int        `json:"deduction"`
	RawDeduction  int        `json:"raw_deduction,omitempty"`
	ScoringGroup  string     `json:"scoring_group,omitempty"`
	Message       string     `json:"message"`
	Blocking      bool       `json:"blocking"`
	ManualReview  bool       `json:"manual_review"`
	DefectKeys    []string   `json:"defect_keys,omitempty"`
}

type SectionScore struct {
	Code      string       `json:"code"`
	Name      string       `json:"name"`
	Standard  int          `json:"standard"`
	Score     int          `json:"score"`
	Deduction int          `json:"deduction"`
	Findings  []RuleResult `json:"findings"`
	Pending   bool         `json:"pending"`
}

type Report struct {
	RunID           string         `json:"run_id,omitempty"`
	InputDigest     string         `json:"-"`
	TenantID        int64          `json:"-"`
	RecordID        string         `json:"record_id"`
	SnapshotID      *string        `json:"snapshot_id,omitempty"`
	Action          Action         `json:"action"`
	RuleSetVersion  string         `json:"rule_set_version"`
	Results         []RuleResult   `json:"results"`
	Sections        []SectionScore `json:"sections"`
	TotalScore      int            `json:"total_score"`
	MajorScore      int            `json:"major_score"`
	Grade           string         `json:"grade"`
	BlockingCount   int            `json:"blocking_count"`
	PendingCount    int            `json:"pending_count"`
	PendingSections []string       `json:"pending_sections"`
	ScoreReady      bool           `json:"score_ready"`
	OverallResult   string         `json:"overall_result"`
	FailureReason   string         `json:"failure_reason,omitempty"`
	ModelAudit      map[string]any `json:"-"`
	StartedAt       time.Time      `json:"-"`
	CompletedAt     time.Time      `json:"-"`
	// ProgramOnly 标记本次 run 是否只执行纯代码规则。HIS 保存/完成触发的
	// 程序质检需要在 save 动作后也写入当前评分；EMR 的 save 仍保持只在
	// complete/archive 时更新当前评分。
	ProgramOnly bool `json:"-"`
}

func (r *Report) HasBlocking() bool { return r != nil && r.BlockingCount > 0 }

func (r *Report) HasNonBlockingProblems() bool {
	if r == nil {
		return false
	}
	for _, result := range r.Results {
		if result.Result == "problem" && !result.Blocking {
			return true
		}
	}
	return false
}

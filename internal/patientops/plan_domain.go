package patientops

import "github.com/freeasyman/lingce-api/internal/customercontact"

// 经营计划 V1 领域对象。场景、目标、名单属于计划；风险属于“客户 × 产品”的名单与任务。

type Plan struct {
	ID             int64          `json:"id"`
	TenantID       int64          `json:"tenant_id"`
	Name           string         `json:"name"`
	PlanType       string         `json:"plan_type"` // status | sales
	Scenario       string         `json:"scenario"`
	PeriodStart    string         `json:"period_start,omitempty"`
	PeriodEnd      string         `json:"period_end,omitempty"`
	TargetCount    *int           `json:"target_count,omitempty"`
	TargetAmount   *float64       `json:"target_amount,omitempty"`
	ProductIDs     []int64        `json:"product_ids"`
	ProductNames   []string       `json:"product_names"`
	FilterConfig   map[string]any `json:"filter_config"`
	Status         string         `json:"status"` // draft | published | closed
	CreatedBy      *int64         `json:"created_by,omitempty"`
	PublishedAt    string         `json:"published_at,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	MemberCount    int64          `json:"member_count"`
	CompletedCount int64          `json:"completed_count"`
}

type PlanMember struct {
	ID                     int64                     `json:"id"`
	PlanID                 int64                     `json:"plan_id"`
	CustomerID             int64                     `json:"customer_id"`
	CustomerName           string                    `json:"customer_name"`
	Phone                  string                    `json:"phone,omitempty"`
	Contacts               []customercontact.Contact `json:"contacts,omitempty"`
	RecommendedProductID   *int64                    `json:"recommended_product_id,omitempty"`
	RecommendedProductName string                    `json:"recommended_product_name,omitempty"`
	MatchScore             *int                      `json:"match_score,omitempty"`
	Reason                 string                    `json:"reason"`
	ReasonSource           string                    `json:"reason_source"`
	RiskMedical            string                    `json:"risk_medical"`
	RiskCommercial         string                    `json:"risk_commercial"`
	TaskID                 *int64                    `json:"task_id,omitempty"`
	Status                 string                    `json:"status"`
	CreatedAt              string                    `json:"created_at"`
}

type PlanProductStat struct {
	ProductID   int64   `json:"product_id"`
	ProductName string  `json:"product_name"`
	TaskCount   int64   `json:"task_count"`
	DealAmount  float64 `json:"deal_amount"`
}

type PlanStats struct {
	TaskTotal        int64             `json:"task_total"`
	TaskDone         int64             `json:"task_done"`
	TaskReplied      int64             `json:"task_replied"`
	TaskBooked       int64             `json:"task_booked"`
	TaskDeals        int64             `json:"task_deals"`
	DealAmount       float64           `json:"deal_amount"`
	ProductBreakdown []PlanProductStat `json:"product_breakdown"`
}

// PlanReportItem 是报表页需要的计划 + 任务统计（不含成员明细）。
type PlanReportItem struct {
	Plan  *Plan     `json:"plan"`
	Stats PlanStats `json:"stats"`
}

type PlanDetail struct {
	Plan    *Plan        `json:"plan"`
	Members []PlanMember `json:"members"`
	Stats   PlanStats    `json:"stats"`
}

type PlanCandidate struct {
	// 年龄是产品适用判断，不等同于客户身份确认。
	AdmissionStatus        string                    `json:"admission_status"` // ready | needs_confirmation | needs_age
	AdmissionReason        string                    `json:"admission_reason,omitempty"`
	ProductVersion         int                       `json:"product_version,omitempty"`
	ProductAgeMin          *int                      `json:"product_age_min,omitempty"`
	ProductAgeMax          *int                      `json:"product_age_max,omitempty"`
	ProductAgeRequired     bool                      `json:"product_age_required"`
	ProductGender          string                    `json:"product_gender,omitempty"`
	GenderHint             string                    `json:"gender_hint,omitempty"`
	CustomerID             int64                     `json:"customer_id"`
	CustomerName           string                    `json:"customer_name"`
	Phone                  string                    `json:"phone,omitempty"`
	Contacts               []customercontact.Contact `json:"contacts,omitempty"`
	Platform               string                    `json:"platform,omitempty"`
	Nickname               string                    `json:"nickname,omitempty"`
	Age                    int                       `json:"age,omitempty"`
	Gender                 string                    `json:"gender,omitempty"`
	Scenario               string                    `json:"scenario"`
	LastVisitAt            string                    `json:"last_visit_at,omitempty"`
	LastVisitType          string                    `json:"last_visit_type,omitempty"`
	LastActivityAt         string                    `json:"last_activity_at,omitempty"`
	Interest               string                    `json:"interest,omitempty"`
	Concern                string                    `json:"concern,omitempty"`
	ClinicalText           string                    `json:"clinical_text,omitempty"`
	Salutation             string                    `json:"salutation,omitempty"`
	CustomerType           string                    `json:"customer_type,omitempty"`
	DecisionMaker          string                    `json:"decision_maker,omitempty"`
	Source                 string                    `json:"source,omitempty"`
	ConditionSummary       string                    `json:"condition_summary,omitempty"`
	VisitHistory           []PlanVisitFact           `json:"visit_history,omitempty"`
	RecommendedProductID   *int64                    `json:"recommended_product_id,omitempty"`
	RecommendedProductName string                    `json:"recommended_product_name,omitempty"`
	ProductPrice           float64                   `json:"product_price,omitempty"`
	ProductApplicable      string                    `json:"product_applicable,omitempty"`
	ProductUpgradeTo       string                    `json:"product_upgrade_to,omitempty"`
	ProductCombineWith     string                    `json:"product_combine_with,omitempty"`
	MatchScore             *int                      `json:"match_score,omitempty"`
	Reason                 string                    `json:"reason"`
	ReasonSource           string                    `json:"reason_source"`
	RiskMedical            string                    `json:"risk_medical"`
	RiskCommercial         string                    `json:"risk_commercial"`
	ExpectedResult         string                    `json:"expected_result"`
	TaskType               string                    `json:"task_type"`
	TaskSubtype            string                    `json:"task_subtype"`
	AssignedTo             *int64                    `json:"assigned_to,omitempty"`

	// 以下字段由大模型按客户生成；为空时回退到模板内容。
	Goal    string `json:"goal,omitempty"`
	Action  string `json:"action,omitempty"`
	Content string `json:"content,omitempty"`

	// ContentGenerated 标记该会员的方案内容由大模型真实生成成功；发布/加员落库后
	// 按会员扣 person 1 的计费依据，回退模板的会员为 false（不扣量）。
	ContentGenerated bool `json:"-"`
}

type PlanExcluded struct {
	CustomerID   int64  `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	Reason       string `json:"reason"`
}

type PlanCoverage struct {
	MatchedCount int     `json:"matched_count"`
	MinAmount    float64 `json:"min_amount"`
	MaxAmount    float64 `json:"max_amount"`
	TargetAmount float64 `json:"target_amount"`
	MinCoverage  float64 `json:"min_coverage"`
	MaxCoverage  float64 `json:"max_coverage"`
}

type PlanPreview struct {
	PlanID        int64           `json:"plan_id"`
	PlanType      string          `json:"plan_type"`
	Scenario      string          `json:"scenario"`
	EligibleCount int             `json:"eligible_count"`
	PendingCount  int             `json:"pending_count"`
	ExcludedCount int             `json:"excluded_count"`
	Candidates    []PlanCandidate `json:"candidates"`
	Excluded      []PlanExcluded  `json:"excluded"`
	Coverage      *PlanCoverage   `json:"coverage,omitempty"`
}

type CreatePlanRequest struct {
	Name         string         `json:"name"`
	PlanType     string         `json:"plan_type"`
	Scenario     string         `json:"scenario"`
	PeriodStart  string         `json:"period_start"`
	PeriodEnd    string         `json:"period_end"`
	TargetCount  *int           `json:"target_count"`
	TargetAmount *float64       `json:"target_amount"`
	ProductIDs   []int64        `json:"product_ids"`
	FilterConfig map[string]any `json:"filter_config"`
}

type UpdatePlanRequest struct {
	Name         *string         `json:"name,omitempty"`
	PlanType     *string         `json:"plan_type,omitempty"`
	Scenario     *string         `json:"scenario,omitempty"`
	PeriodStart  *string         `json:"period_start,omitempty"`
	PeriodEnd    *string         `json:"period_end,omitempty"`
	TargetCount  *int            `json:"target_count,omitempty"`
	TargetAmount *float64        `json:"target_amount,omitempty"`
	ProductIDs   *[]int64        `json:"product_ids,omitempty"`
	FilterConfig *map[string]any `json:"filter_config,omitempty"`
}

type PublishPlanRequest struct {
	CustomerIDs              []int64                       `json:"customer_ids"`
	SuitabilityConfirmations []PlanSuitabilityConfirmation `json:"suitability_confirmations,omitempty"`
}

type AddPlanMembersRequest struct {
	CustomerIDs              []int64                       `json:"customer_ids"`
	SuitabilityConfirmations []PlanSuitabilityConfirmation `json:"suitability_confirmations,omitempty"`
}

type PlanSuitabilityConfirmation struct {
	CustomerID     int64  `json:"customer_id"`
	ProductID      int64  `json:"product_id"`
	ProductVersion int    `json:"product_version"`
	Reason         string `json:"reason"`
}

// planCustomerFact 是智能筛选与产品匹配所需的客户事实快照。
type planCustomerFact struct {
	CustomerID      int64
	Name            string
	Phone           string
	Gender          string
	Age             int
	IdentityStatus  string
	Tags            string
	Status          string
	Source          string
	LifecycleStage  string
	AssignedTo      *int64
	Contacts        []customercontact.Contact
	OpenTasks       int
	LastVisitAt     string
	LastVisitType   string
	VisitCount      int
	LastContactedAt string
	LastSourceAt    string
	Platform        string
	Nickname        string
	Interest        string
	Concern         string
	Question        string
	DecisionMaker   string
	Diagnosis       string
	ChiefComplaint  string
	Department      string
	ClinicalText    string
	InOtherPlan     bool
}

type PlanVisitFact struct {
	VisitAt        string `json:"visit_at,omitempty"`
	VisitType      string `json:"visit_type,omitempty"`
	Department     string `json:"department,omitempty"`
	Doctor         string `json:"doctor,omitempty"`
	Diagnosis      string `json:"diagnosis,omitempty"`
	ChiefComplaint string `json:"chief_complaint,omitempty"`
	TreatmentPlan  string `json:"treatment_plan,omitempty"`
	Fee            string `json:"fee,omitempty"`
}

type planProduct struct {
	ID                 int64
	Name               string
	Aliases            []string
	Price              float64
	Applicable         string
	SellingPoint       string
	UpgradeTo          string
	CombineWith        string
	ApplicableKeywords []string
	AgeMin             *int
	AgeMax             *int
	AgeRequired        bool
	Version            int
	Gender             string
	Contraindications  string
}

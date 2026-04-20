package recording

import "time"

// CreateRecordingRequest represents a request to create a medical recording
type CreateRecordingRequest struct {
	TenantID           int64      `json:"tenant_id"`
	EmployeeID         int64      `json:"employee_id"`
	PatientName        string     `json:"patient_name"`
	PatientAge         *int       `json:"patient_age,omitempty"`
	PatientGender      *string    `json:"patient_gender,omitempty"`
	PatientPhone       *string    `json:"patient_phone,omitempty"`
	RecordingURL       string     `json:"recording_url"`
	RecordingDuration  *int       `json:"recording_duration,omitempty"`
	RecordingStartedAt *time.Time `json:"recording_started_at,omitempty"`
	RecordingEndedAt   *time.Time `json:"recording_ended_at,omitempty"`
}

// UpdateRecordingRequest represents a request to update a medical recording
type UpdateRecordingRequest struct {
	PatientName       *string          `json:"patient_name,omitempty"`
	PatientAge        *int             `json:"patient_age,omitempty"`
	PatientGender     *string          `json:"patient_gender,omitempty"`
	PatientPhone      *string          `json:"patient_phone,omitempty"`
	CustomerID        *int64           `json:"customer_id,omitempty"`
	TranscriptText    *string          `json:"transcript_text,omitempty"`
	DoctorSummary     *string          `json:"doctor_summary,omitempty"`
	TherapistSummary  *string          `json:"therapist_summary,omitempty"`
	ConsultantSummary *string          `json:"consultant_summary,omitempty"`
	Status            *RecordingStatus `json:"status,omitempty"`
	ProcessingError   *string          `json:"processing_error,omitempty"`
	ProcessedAt       *time.Time       `json:"processed_at,omitempty"`
}

// RecordingResponse represents a medical recording response
type RecordingResponse struct {
	ID                    int64                    `json:"id"`
	TenantID              int64                    `json:"tenant_id"`
	TenantName            string                   `json:"tenant_name,omitempty"`
	EmployeeID            int64                    `json:"employee_id"`
	EmployeeName          string                   `json:"employee_name,omitempty"`
	CustomerID            *int64                   `json:"customer_id,omitempty"`
	CustomerName          *string                  `json:"customer_name,omitempty"`
	PatientName           string                   `json:"patient_name"`
	PatientAge            *int                     `json:"patient_age,omitempty"`
	PatientGender         *string                  `json:"patient_gender,omitempty"`
	PatientPhone          *string                  `json:"patient_phone,omitempty"`
	RecordingURL          string                   `json:"recording_url"`
	RecordingDuration     *int                     `json:"recording_duration,omitempty"`
	TranscriptText        *string                  `json:"transcript_text,omitempty"`
	DoctorSummary         *string                  `json:"doctor_summary,omitempty"`
	TherapistSummary      *string                  `json:"therapist_summary,omitempty"`
	ConsultantSummary     *string                  `json:"consultant_summary,omitempty"`
	SceneType             *string                  `json:"scene_type,omitempty"`
	VisitOutcome          *string                  `json:"visit_outcome,omitempty"`
	SubjectiveSummary     *string                  `json:"subjective_summary,omitempty"`
	QualityScore          *float64                 `json:"quality_score,omitempty"`
	SegueScore            *float64                 `json:"segue_score,omitempty"`
	CriticalGap           *bool                    `json:"critical_gap,omitempty"`
	AnalysisResult        map[string]interface{}   `json:"analysis_result,omitempty"`
	AnalysisSummary       map[string]interface{}   `json:"analysis_summary,omitempty"`
	AnalysisDisplay       map[string]interface{}   `json:"analysis_display,omitempty"`
	AnalysisStatus        *string                  `json:"analysis_status"`
	Duration              *int                     `json:"duration"`
	SeguePercent          *float64                 `json:"segue_percent"`
	VisitOutcomeStatus    *string                  `json:"visit_outcome_status"`
	DecisionStatus        *string                  `json:"decision_status"`
	SoapSubjectiveSummary *string                  `json:"soap_subjective_summary"`
	RecordedAt            *string                  `json:"recorded_at"`
	ChiefComplaint        *string                  `json:"chief_complaint"`
	Diagnosis             *string                  `json:"diagnosis"`
	CurrentState          *string                  `json:"current_state"`
	ContentSeedsCount     int                      `json:"content_seeds_count"`
	ContentSeedsTypes     []string                 `json:"content_seeds_types"`
	RouteReviewRequired   *bool                    `json:"route_review_required"`
	RouteReviewReason     *string                  `json:"route_review_reason"`
	RouteAutoDecision     *string                  `json:"route_auto_decision"`
	RouteReviewStatus     *string                  `json:"route_review_status"`
	EMRStatus             *string                  `json:"emr_status"`
	StatusSummary         *string                  `json:"status_summary,omitempty"`
	DealOutcome           map[string]interface{}   `json:"deal_outcome,omitempty"`
	SuggestedTask         map[string]interface{}   `json:"suggested_task,omitempty"`
	ConsultationRecord    map[string]interface{}   `json:"consultation_record,omitempty"`
	Report                *string                  `json:"report,omitempty"`
	StructuredTranscript  []map[string]interface{} `json:"structured_transcript,omitempty"`
	TimelineTranscript    []map[string]interface{} `json:"timeline_transcript,omitempty"`
	ContentSeeds          []map[string]interface{} `json:"content_seeds,omitempty"`
	RouteReview           map[string]interface{}   `json:"route_review,omitempty"`
	EMRDraft              map[string]interface{}   `json:"emr_draft,omitempty"`
	Status                string                   `json:"status"`
	ProcessingError       *string                  `json:"processing_error,omitempty"`
	RecordingStartedAt    *string                  `json:"recording_started_at,omitempty"`
	RecordingEndedAt      *string                  `json:"recording_ended_at,omitempty"`
	ProcessedAt           *string                  `json:"processed_at,omitempty"`
	CreatedAt             string                   `json:"created_at"`
	UpdatedAt             string                   `json:"updated_at"`
}

// RecordingListRequest represents a request to list medical recordings
type RecordingListRequest struct {
	TenantID     int64            `json:"tenant_id"`
	TenantIDs    []int64          `json:"tenant_ids,omitempty"`
	Scope        *RecordingScope  `json:"recording_scope,omitempty"`
	EmployeeID   *int64           `json:"employee_id,omitempty"`
	PatientName  *string          `json:"patient_name,omitempty"`
	Status       *RecordingStatus `json:"status,omitempty"`
	Scene        *RecordingScene  `json:"scene,omitempty"`
	Source       *RecordingSource `json:"source,omitempty"`
	SceneType    *string          `json:"scene_type,omitempty"`
	VisitOutcome *string          `json:"visit_outcome,omitempty"`
	IncludeShort *bool            `json:"include_short,omitempty"`
	SegueMin     *float64         `json:"segue_min,omitempty"`
	SegueMax     *float64         `json:"segue_max,omitempty"`
	StartDate    *time.Time       `json:"start_date,omitempty"`
	EndDate      *time.Time       `json:"end_date,omitempty"`
	Keyword      *string          `json:"keyword,omitempty"`
	Page         int              `json:"page"`
	PageSize     int              `json:"page_size"`
}

// Recording Statistics DTOs

// RecordingStatsOverviewResponse represents the overview statistics
type RecordingStatsOverviewResponse struct {
	TotalRecordings     int64   `json:"total_recordings"`
	TotalDuration       int64   `json:"total_duration"` // in seconds
	CompletedRecordings int64   `json:"completed_recordings"`
	PendingRecordings   int64   `json:"pending_recordings"`
	FailedRecordings    int64   `json:"failed_recordings"`
	AvgDuration         float64 `json:"avg_duration"`
	TodayRecordings     int64   `json:"today_recordings"`
}

// RecordingStatsBySceneResponse represents statistics by scene
type RecordingStatsBySceneResponse struct {
	Scene      string  `json:"scene"`
	Count      int64   `json:"count"`
	Duration   int64   `json:"duration"`
	Percentage float64 `json:"percentage"`
}

// RecordingStatsBySourceResponse represents statistics by source
type RecordingStatsBySourceResponse struct {
	Source     string  `json:"source"`
	Count      int64   `json:"count"`
	Duration   int64   `json:"duration"`
	Percentage float64 `json:"percentage"`
}

// RecordingStatsByTenantResponse represents statistics by tenant
type RecordingStatsByTenantResponse struct {
	TenantID   int64  `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	Count      int64  `json:"count"`
	Duration   int64  `json:"duration"`
}

// DurationDistributionResponse represents duration distribution
type DurationDistributionResponse struct {
	Range      string  `json:"range"` // e.g., "0-5min", "5-10min"
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
}

// DailyStatsResponse represents daily statistics
type DailyStatsResponse struct {
	Date     string `json:"date"`
	Count    int64  `json:"count"`
	Duration int64  `json:"duration"`
}

// Recording Operations DTOs

// TriggerTranscribeRequest represents the request for triggering transcription
type TriggerTranscribeRequest struct {
	Force bool `json:"force,omitempty"` // Force re-transcribe even if already done
}

// TriggerAnalyzeRequest represents the request for triggering analysis
type TriggerAnalyzeRequest struct {
	Force bool `json:"force,omitempty"` // Force re-analyze even if already done
}

// TriggerCleanRequest represents the request for triggering text cleaning
type TriggerCleanRequest struct {
	Force bool `json:"force,omitempty"` // Force re-clean even if already done
}

// AnalysisFeedbackRequest represents feedback on analysis results
type AnalysisFeedbackRequest struct {
	Rating  int     `json:"rating"` // 1-5
	Comment *string `json:"comment,omitempty"`
}

// BatchTranscribeRequest represents the request for batch transcription
type BatchTranscribeRequest struct {
	RecordingIDs []int64 `json:"recording_ids"`
}

// BatchDeleteRequest represents the request for batch deletion
type BatchDeleteRequest struct {
	RecordingIDs []int64 `json:"recording_ids"`
}

// ConfirmActionRequest represents the request for confirming follow-up action
type ConfirmActionRequest struct {
	Action string  `json:"action"` // e.g., "call", "message", "visit"
	Notes  *string `json:"notes,omitempty"`
}

// GenerateOpeningScriptRequest represents the request for generating opening script
type GenerateOpeningScriptRequest struct {
	Context *string `json:"context,omitempty"`
}

// GenerateOperationsPlanRequest represents the request for generating operations plan
type GenerateOperationsPlanRequest struct {
	Context *string `json:"context,omitempty"`
}

// PlayURLResponse represents the play URL response
type PlayURLResponse struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

// LearningRecommendationResponse represents learning recommendation
type LearningRecommendationResponse struct {
	Recommendations []RecommendationItem `json:"recommendations"`
}

// RecommendationItem represents a recommendation item
type RecommendationItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"` // "high", "medium", "low"
}

// Medical Recording specific DTOs

// QualityControlDashboardResponse represents the quality control dashboard
type QualityControlDashboardResponse struct {
	TotalRecordings  int64        `json:"total_recordings"`
	QualifiedCount   int64        `json:"qualified_count"`
	UnqualifiedCount int64        `json:"unqualified_count"`
	AvgScore         float64      `json:"avg_score"`
	TopIssues        []IssueCount `json:"top_issues"`
}

// IssueCount represents an issue and its count
type IssueCount struct {
	Issue string `json:"issue"`
	Count int64  `json:"count"`
}

// DoctorAbilityRankingResponse represents doctor ability ranking
type DoctorAbilityRankingResponse struct {
	EmployeeID     int64   `json:"employee_id"`
	EmployeeName   string  `json:"employee_name"`
	RecordingCount int64   `json:"recording_count"`
	AvgScore       float64 `json:"avg_score"`
	Rank           int     `json:"rank"`
}

// DoctorAbilityDetailResponse represents detailed doctor ability
type DoctorAbilityDetailResponse struct {
	EmployeeID           int64    `json:"employee_id"`
	EmployeeName         string   `json:"employee_name"`
	CommunicationScore   float64  `json:"communication_score"`
	ProfessionalismScore float64  `json:"professionalism_score"`
	EmpathyScore         float64  `json:"empathy_score"`
	EfficiencyScore      float64  `json:"efficiency_score"`
	RecentTrend          string   `json:"recent_trend"` // "improving", "stable", "declining"
	Strengths            []string `json:"strengths"`
	Weaknesses           []string `json:"weaknesses"`
}

// CommunicationAnalysisResponse represents communication analysis
type CommunicationAnalysisResponse struct {
	TotalRecordings       int64                          `json:"total_recordings"`
	AvgCommunicationScore float64                        `json:"avg_communication_score"`
	TopCommunicators      []DoctorAbilityRankingResponse `json:"top_communicators"`
	CommonIssues          []IssueCount                   `json:"common_issues"`
}

// WeeklyMeetingMaterialResponse represents weekly meeting material
type WeeklyMeetingMaterialResponse struct {
	WeekStart        string             `json:"week_start"`
	WeekEnd          string             `json:"week_end"`
	Highlights       []string           `json:"highlights"`
	BestPractices    []BestPracticeItem `json:"best_practices"`
	ImprovementAreas []string           `json:"improvement_areas"`
}

// BestPracticeItem represents a best practice item
type BestPracticeItem struct {
	RecordingID  int64  `json:"recording_id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	EmployeeName string `json:"employee_name"`
}

// WeeklySummaryResponse represents weekly summary
type WeeklySummaryResponse struct {
	WeekStart       string                         `json:"week_start"`
	WeekEnd         string                         `json:"week_end"`
	TotalRecordings int64                          `json:"total_recordings"`
	AvgScore        float64                        `json:"avg_score"`
	TopPerformers   []DoctorAbilityRankingResponse `json:"top_performers"`
	KeyInsights     []string                       `json:"key_insights"`
}

// TeamTrendsResponse represents team trends
type TeamTrendsResponse struct {
	Period string           `json:"period"` // "daily", "weekly", "monthly"
	Data   []TrendDataPoint `json:"data"`
}

// TrendDataPoint represents a data point in trend
type TrendDataPoint struct {
	Date           string  `json:"date"`
	AvgScore       float64 `json:"avg_score"`
	RecordingCount int64   `json:"recording_count"`
}

// AddBestPracticeRequest represents the request for adding best practice
type AddBestPracticeRequest struct {
	Title       string   `json:"title"`
	Description *string  `json:"description,omitempty"`
	Category    *string  `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// FollowUpGenerationModeResponse represents follow-up task generation mode
type FollowUpGenerationModeResponse struct {
	TenantID  int64      `json:"tenant_id"`
	Mode      string     `json:"mode"` // "auto", "manual", "hybrid"
	AutoRules JSONObject `json:"auto_rules,omitempty"`
}

// UpdateFollowUpGenerationModeRequest represents the request for updating generation mode
type UpdateFollowUpGenerationModeRequest struct {
	Mode      string     `json:"mode"`
	AutoRules JSONObject `json:"auto_rules,omitempty"`
}

// ConfirmFollowUpTasksRequest represents the request for confirming follow-up tasks
type ConfirmFollowUpTasksRequest struct {
	TaskIDs []int64 `json:"task_ids"`
}

// InstitutionRuleConfigListRequest represents the request for listing rule configs
type InstitutionRuleConfigListRequest struct {
	RuleType *string `json:"rule_type,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

// CreateInstitutionRuleConfigRequest represents the request for creating rule config
type CreateInstitutionRuleConfigRequest struct {
	RuleType   string     `json:"rule_type"`
	RuleConfig JSONObject `json:"rule_config"`
	IsActive   bool       `json:"is_active"`
}

// UpdateInstitutionRuleConfigRequest represents the request for updating rule config
type UpdateInstitutionRuleConfigRequest struct {
	RuleConfig *JSONObject `json:"rule_config,omitempty"`
	IsActive   *bool       `json:"is_active,omitempty"`
}

// RecordingAnalysisSettingsResponse represents recording analysis settings
type RecordingAnalysisSettingsResponse struct {
	TenantID           int64      `json:"tenant_id"`
	EnableAutoAnalysis bool       `json:"enable_auto_analysis"`
	AnalysisPrompts    []string   `json:"analysis_prompts"`
	QualityThresholds  JSONObject `json:"quality_thresholds"`
}

// Recording Task DTOs

// RecordingTaskStatsResponse represents task statistics
type RecordingTaskStatsResponse struct {
	TotalTasks     int64 `json:"total_tasks"`
	PendingTasks   int64 `json:"pending_tasks"`
	AssignedTasks  int64 `json:"assigned_tasks"`
	CompletedTasks int64 `json:"completed_tasks"`
	CancelledTasks int64 `json:"cancelled_tasks"`
	OverdueTasks   int64 `json:"overdue_tasks"`
}

// DailyBriefingResponse represents daily briefing
type DailyBriefingResponse struct {
	Date              string `json:"date"`
	TodayTasks        int64  `json:"today_tasks"`
	CompletedTasks    int64  `json:"completed_tasks"`
	PendingTasks      int64  `json:"pending_tasks"`
	HighPriorityTasks int64  `json:"high_priority_tasks"`
	Summary           string `json:"summary"`
}

// TaskListRequest represents the request for listing tasks
type TaskListRequest struct {
	TenantID    *int64      `json:"tenant_id,omitempty"`
	TenantIDs   []int64     `json:"tenant_ids,omitempty"`
	RecordingID *int64      `json:"recording_id,omitempty"`
	AssignedTo  *int64      `json:"assigned_to,omitempty"`
	Status      *TaskStatus `json:"status,omitempty"`
	TaskType    *TaskType   `json:"task_type,omitempty"`
	StartDate   *time.Time  `json:"start_date,omitempty"`
	EndDate     *time.Time  `json:"end_date,omitempty"`
	Page        int         `json:"page"`
	PageSize    int         `json:"page_size"`
}

// TaskResponse represents a task response
type TaskResponse struct {
	ID                    int64   `json:"id"`
	TenantID              int64   `json:"tenant_id"`
	RecordingID           int64   `json:"recording_id"`
	TaskType              string  `json:"task_type"`
	Title                 string  `json:"title"`
	Description           *string `json:"description,omitempty"`
	CustomerName          *string `json:"customer_name,omitempty"`
	Priority              *string `json:"priority,omitempty"`
	Script                *string `json:"script,omitempty"`
	ContactReason         *string `json:"contact_reason,omitempty"`
	SourceType            *string `json:"source_type,omitempty"`
	SourceTypeLabel       *string `json:"source_type_label,omitempty"`
	SourceDetail          *string `json:"source_detail,omitempty"`
	SourceDetailLabel     *string `json:"source_detail_label,omitempty"`
	AssignedTo            *int64  `json:"assigned_to,omitempty"`
	AssignedToName        *string `json:"assigned_to_name,omitempty"`
	AssignedBy            *int64  `json:"assigned_by,omitempty"`
	RecordingRoleCategory *string `json:"recording_role_category,omitempty"`
	RecordingRoleLabel    *string `json:"recording_role_label,omitempty"`
	RecordingOwnerName    *string `json:"recording_owner_name,omitempty"`
	Status                string  `json:"status"`
	DueDate               *string `json:"due_date,omitempty"`
	DueAt                 *string `json:"due_at,omitempty"`
	CompletedAt           *string `json:"completed_at,omitempty"`
	CompletedBy           *int64  `json:"completed_by,omitempty"`
	CancelledAt           *string `json:"cancelled_at,omitempty"`
	CancelReason          *string `json:"cancel_reason,omitempty"`
	CreatedAt             string  `json:"created_at"`
	UpdatedAt             string  `json:"updated_at"`
}

// BatchAssignTasksRequest represents the request for batch assigning tasks
type BatchAssignTasksRequest struct {
	TaskIDs    []int64 `json:"task_ids"`
	AssignedTo int64   `json:"assigned_to"`
}

// CompleteTaskRequest represents the request for completing a task
type CompleteTaskRequest struct {
	Notes *string `json:"notes,omitempty"`
}

// CancelTaskRequest represents the request for cancelling a task
type CancelTaskRequest struct {
	Reason string `json:"reason"`
}

// EmployeePartnershipListRequest represents the request for listing partnerships
type EmployeePartnershipListRequest struct {
	EmployeeID   *int64  `json:"employee_id,omitempty"`
	PartnerID    *int64  `json:"partner_id,omitempty"`
	Relationship *string `json:"relationship,omitempty"`
	Page         int     `json:"page"`
	PageSize     int     `json:"page_size"`
}

// CreateEmployeePartnershipRequest represents the request for creating partnership
type CreateEmployeePartnershipRequest struct {
	EmployeeID   int64  `json:"employee_id"`
	PartnerID    int64  `json:"partner_id"`
	Relationship string `json:"relationship"`
}

// Recording Dashboard DTOs

// DailyReportResponse represents daily report
type DailyReportResponse struct {
	Date            string                         `json:"date"`
	TotalRecordings int64                          `json:"total_recordings"`
	TotalDuration   int64                          `json:"total_duration"`
	AvgScore        float64                        `json:"avg_score"`
	TopPerformers   []DoctorAbilityRankingResponse `json:"top_performers"`
	KeyMetrics      JSONObject                     `json:"key_metrics"`
	Revenue         *DashboardRevenue              `json:"revenue,omitempty"`
	Pipeline        *DashboardPipeline             `json:"pipeline,omitempty"`
	EmployeeRanking []DashboardEmployeeRankingItem `json:"employee_ranking,omitempty"`
	AmountTrend     []DashboardAmountTrendItem     `json:"amount_trend,omitempty"`
	Insight         *string                        `json:"insight,omitempty"`
}

// DiagnosisResponse represents diagnosis
type DiagnosisResponse struct {
	OverallHealth   string           `json:"overall_health"` // "excellent", "good", "fair", "poor"
	Issues          []IssueCount     `json:"issues"`
	Recommendations []string         `json:"recommendations"`
	Trends          []TrendDataPoint `json:"trends"`
	DataQuality     *DashboardDataQuality           `json:"data_quality,omitempty"`
	Funnel          []DashboardFunnelStage          `json:"funnel,omitempty"`
	ConcernDist     []DashboardConcernDistribution  `json:"concern_distribution,omitempty"`
	EmployeeDetails []DashboardEmployeeDiagnosisRow `json:"employee_diagnosis,omitempty"`
}

// UpdateTargetRequest represents the request for updating target
type UpdateTargetRequest struct {
	Month                string   `json:"month"` // YYYY-MM
	Target               float64  `json:"target"`
	TenantID             *int64   `json:"tenant_id,omitempty"`
	MonthlyRevenueTarget *float64 `json:"monthly_revenue_target,omitempty"`
}

// FunnelDetailResponse represents funnel detail
type FunnelDetailResponse struct {
	Stage      string  `json:"stage"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
	DropRate   float64 `json:"drop_rate"`
}

type DashboardRevenue struct {
	TotalAmount    float64  `json:"total_amount"`
	DealCount      int64    `json:"deal_count"`
	DealRate       float64  `json:"deal_rate"`
	AvgDealAmount  float64  `json:"avg_deal_amount"`
	ConfirmedCount int64    `json:"confirmed_count"`
	TotalRecordings int64   `json:"total_recordings"`
	ActiveEmployees int64   `json:"active_employees"`
	Target         *float64 `json:"target,omitempty"`
	TargetProgress *float64 `json:"target_progress,omitempty"`
	MomAmount      *float64 `json:"mom_amount,omitempty"`
	MomDealRate    *float64 `json:"mom_deal_rate,omitempty"`
	MomAvgAmount   *float64 `json:"mom_avg_amount,omitempty"`
}

type DashboardPipeline struct {
	FollowingCount         int64    `json:"following_count"`
	Active7DCount          int64    `json:"active_7d_count"`
	OverdueCount           int64    `json:"overdue_count"`
	AvgDealAmount          float64  `json:"avg_deal_amount"`
	HistoricalRecoveryRate *float64 `json:"historical_recovery_rate,omitempty"`
}

type DashboardEmployeeRankingItem struct {
	EmployeeID        int64    `json:"employee_id"`
	Name              string   `json:"name"`
	DealAmount        float64  `json:"deal_amount"`
	DealRate          float64  `json:"deal_rate"`
	Consultations     int64    `json:"consultations"`
	AvgDealAmount     float64  `json:"avg_deal_amount"`
	FollowingCount    int64    `json:"following_count"`
	OverdueCount      int64    `json:"overdue_count"`
	AbilityScore      *float64 `json:"ability_score,omitempty"`
	AbilitySampleCount int64   `json:"ability_sample_count"`
}

type DashboardAmountTrendItem struct {
	Date     string  `json:"date"`
	Amount   float64 `json:"amount"`
	DealRate float64 `json:"deal_rate"`
}

type DashboardDataQuality struct {
	RecordingCount      int64   `json:"recording_count"`
	AnalysisSuccessRate float64 `json:"analysis_success_rate"`
}

type DashboardFunnelStage struct {
	Key    string   `json:"key"`
	Label  string   `json:"label"`
	Count  int64    `json:"count"`
	Amount *float64 `json:"amount,omitempty"`
	Rate   *float64 `json:"rate,omitempty"`
}

type DashboardConcernDistribution struct {
	Reason     string  `json:"reason"`
	Label      string  `json:"label"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
}

type DashboardEmployeeDiagnosisRow struct {
	EmployeeID     int64   `json:"employee_id"`
	Name           string  `json:"name"`
	Consultations  int64   `json:"consultations"`
	DealCount      int64   `json:"deal_count"`
	DealRate       float64 `json:"deal_rate"`
	DealAmount     float64 `json:"deal_amount"`
	AvgDealAmount  float64 `json:"avg_deal_amount"`
	FollowingCount int64   `json:"following_count"`
	OverdueCount   int64   `json:"overdue_count"`
}

type DashboardFunnelDetailByEmployee struct {
	EmployeeID   int64  `json:"employee_id"`
	Name         string `json:"name"`
	Count        int64  `json:"count"`
	OverdueCount int64  `json:"overdue_count"`
}

type DashboardFunnelDetailResponse struct {
	StageKey   string                          `json:"stage_key"`
	StageLabel string                          `json:"stage_label"`
	TotalCount int64                           `json:"total_count"`
	ByEmployee []DashboardFunnelDetailByEmployee `json:"by_employee"`
}

// Recording Prompt DTOs

// RecordingPromptListRequest represents the request for listing prompts
type RecordingPromptListRequest struct {
	Code     *string `json:"code,omitempty"`
	Name     *string `json:"name,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

// CreateRecordingPromptRequest represents the request for creating prompt
type CreateRecordingPromptRequest struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	PromptText  string   `json:"prompt_text"`
	Variables   []string `json:"variables,omitempty"`
	IsActive    bool     `json:"is_active"`
}

// UpdateRecordingPromptRequest represents the request for updating prompt
type UpdateRecordingPromptRequest struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	PromptText  *string  `json:"prompt_text,omitempty"`
	Variables   []string `json:"variables,omitempty"`
	IsActive    *bool    `json:"is_active,omitempty"`
}

// TestPromptRequest represents the request for testing prompt
type TestPromptRequest struct {
	Variables JSONObject `json:"variables"`
}

// TestPromptResponse represents the response for testing prompt
type TestPromptResponse struct {
	RenderedPrompt string `json:"rendered_prompt"`
	TestResult     string `json:"test_result"`
}

// RecordingPromptTenantConfigListRequest represents the request for listing tenant configs
type RecordingPromptTenantConfigListRequest struct {
	PromptCode *string `json:"prompt_code,omitempty"`
	IsActive   *bool   `json:"is_active,omitempty"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

// CreateRecordingPromptTenantConfigRequest represents the request for creating tenant config
type CreateRecordingPromptTenantConfigRequest struct {
	PromptCode string `json:"prompt_code"`
	PromptText string `json:"prompt_text"`
	IsActive   bool   `json:"is_active"`
}

// UpdateRecordingPromptTenantConfigRequest represents the request for updating tenant config
type UpdateRecordingPromptTenantConfigRequest struct {
	PromptText *string `json:"prompt_text,omitempty"`
	IsActive   *bool   `json:"is_active,omitempty"`
}

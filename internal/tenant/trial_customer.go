package tenant

import "time"

type TrialCustomerListRequest struct {
	Keyword          string `json:"keyword"`
	OwnerAdminID     *int64 `json:"owner_admin_id,omitempty"`
	Stage            string `json:"stage,omitempty"`
	ActivationStatus string `json:"activation_status,omitempty"`
	HasRealRecording *bool  `json:"has_real_recording,omitempty"`
	Page             int    `json:"page"`
	PageSize         int    `json:"page_size"`
}

type TrialCustomerAssignment struct {
	TenantID               int64      `json:"tenant_id"`
	SalesOwnerAdminID      *int64     `json:"sales_owner_admin_id,omitempty"`
	SalesOwnerNameSnapshot string     `json:"sales_owner_name_snapshot,omitempty"`
	AssignedAt             *time.Time `json:"assigned_at,omitempty"`
	AssignedBy             *int64     `json:"assigned_by,omitempty"`
	UpdatedAt              *time.Time `json:"updated_at,omitempty"`
}

type TrialCustomerMetrics struct {
	TenantID               int64      `json:"tenant_id"`
	TrialStartedAt         *time.Time `json:"trial_started_at,omitempty"`
	TrialExpiresAt         *time.Time `json:"trial_expires_at,omitempty"`
	FirstLoginAt           *time.Time `json:"first_login_at,omitempty"`
	LastLoginAt            *time.Time `json:"last_login_at,omitempty"`
	LoginCount             int        `json:"login_count"`
	DoctorDemoViewedAt     *time.Time `json:"doctor_demo_viewed_at,omitempty"`
	ConsultantDemoViewedAt *time.Time `json:"consultant_demo_viewed_at,omitempty"`
	FirstUploadAt          *time.Time `json:"first_upload_at,omitempty"`
	LastUploadAt           *time.Time `json:"last_upload_at,omitempty"`
	UploadCount            int        `json:"upload_count"`
	AnalysisCount          int        `json:"analysis_count"`
	DoctorUploadCount      int        `json:"doctor_upload_count"`
	ConsultantUploadCount  int        `json:"consultant_upload_count"`
	GeneratedCustomerCount int        `json:"generated_customer_count"`
	GeneratedTaskCount     int        `json:"generated_task_count"`
	GeneratedContentCount  int        `json:"generated_content_count"`
	WechatContentCount     int        `json:"wechat_content_count"`
	XiaohongshuContentCount int       `json:"xiaohongshu_content_count"`
	VideoScriptContentCount int       `json:"video_script_content_count"`
	LastActivityAt         *time.Time `json:"last_activity_at,omitempty"`
	CurrentStage           string     `json:"current_stage,omitempty"`
	PriorityLevel          string     `json:"priority_level,omitempty"`
	BlockingReason         string     `json:"blocking_reason,omitempty"`
	NextActionHint         string     `json:"next_action_hint,omitempty"`
	UpdatedAt              *time.Time `json:"updated_at,omitempty"`
	TrialMaxRecordings     int        `json:"trial_max_recordings"`
	TrialRemainingUsage    int        `json:"trial_remaining_usage"`
}

type TrialCustomerListItem struct {
	TenantID                 int64      `json:"tenant_id"`
	TenantName               string     `json:"tenant_name"`
	TenantCode               string     `json:"tenant_code"`
	ContactName              string     `json:"contact_name,omitempty"`
	ContactPhone             string     `json:"contact_phone,omitempty"`
	AccountMode              string     `json:"account_mode"`
	CurrentStage             string     `json:"current_stage"`
	ActivationStatus         string     `json:"activation_status"`
	SalesOwnerAdminID        *int64     `json:"sales_owner_admin_id,omitempty"`
	SalesOwnerName           string     `json:"sales_owner_name,omitempty"`
	TrialStartedAt           *time.Time `json:"trial_started_at,omitempty"`
	TrialExpiresAt           *time.Time `json:"trial_expires_at,omitempty"`
	FirstLoginAt             *time.Time `json:"first_login_at,omitempty"`
	LastLoginAt              *time.Time `json:"last_login_at,omitempty"`
	FirstUploadAt            *time.Time `json:"first_upload_at,omitempty"`
	LastUploadAt             *time.Time `json:"last_upload_at,omitempty"`
	LoginCount               int        `json:"login_count"`
	AnalysisCount            int        `json:"analysis_count"`
	UploadCount              int        `json:"upload_count"`
	DoctorDemoViewed         bool       `json:"doctor_demo_viewed"`
	ConsultantDemoViewed     bool       `json:"consultant_demo_viewed"`
	RealRecordingUploadCount int        `json:"real_recording_upload_count"`
	GeneratedCustomerCount   int        `json:"generated_customer_count"`
	GeneratedTaskCount       int        `json:"generated_task_count"`
	PriorityLevel            string     `json:"priority_level,omitempty"`
	BlockingReason           string     `json:"blocking_reason,omitempty"`
	NextActionHint           string     `json:"next_action_hint,omitempty"`
	LatestFollowUpSummary    string     `json:"latest_follow_up_summary,omitempty"`
	LatestFollowUpAt         *time.Time `json:"latest_follow_up_at,omitempty"`
}

type TrialCustomerDetail struct {
	Item             TrialCustomerListItem         `json:"item"`
	Metrics          TrialCustomerMetrics          `json:"metrics"`
	Assignment       TrialCustomerAssignment       `json:"assignment"`
	Milestones       []TrialCustomerMilestone      `json:"milestones"`
	RoleUsage        []TrialCustomerRoleUsage      `json:"role_usage"`
	FollowUps        []TrialCustomerFollowUp       `json:"follow_ups"`
	AIRecommendation TrialCustomerAIRecommendation `json:"ai_recommendation"`
}

type TrialCustomerMilestone struct {
	Code       string     `json:"code"`
	Label      string     `json:"label"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
}

type TrialCustomerRoleUsage struct {
	RoleCode        string `json:"role_code"`
	DemoViewed      bool   `json:"demo_viewed"`
	RealUploadCount int    `json:"real_upload_count"`
	InterestLevel   string `json:"interest_level,omitempty"`
	StatusSummary   string `json:"status_summary,omitempty"`
}

type TrialCustomerFollowUp struct {
	ID                int64      `json:"id"`
	TenantID          int64      `json:"tenant_id"`
	SalesOwnerAdminID *int64     `json:"sales_owner_admin_id,omitempty"`
	FollowUpType      string     `json:"follow_up_type"`
	Summary           string     `json:"summary"`
	Result            string     `json:"result"`
	NextFollowUpAt    *time.Time `json:"next_follow_up_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	CreatedBy         *int64     `json:"created_by,omitempty"`
}

type TrialCustomerAIRecommendation struct {
	Summary         string `json:"summary"`
	BlockingReason  string `json:"blocking_reason,omitempty"`
	RecommendedStep string `json:"recommended_step,omitempty"`
	RecommendedTalk string `json:"recommended_talk,omitempty"`
	RecommendedWhen string `json:"recommended_when,omitempty"`
}

type TrialCustomerListResponse struct {
	Items    []*TrialCustomerListItem `json:"items"`
	Total    int64                    `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}

type TrialCustomerFunnelStage struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type TrialCustomerOwnerFunnel struct {
	OwnerAdminID   *int64 `json:"owner_admin_id,omitempty"`
	OwnerName      string `json:"owner_name"`
	TrialCount     int    `json:"trial_count"`
	ActivatedCount int    `json:"activated_count"`
	UploadedCount  int    `json:"uploaded_count"`
	ConvertedCount int    `json:"converted_count"`
}

type TrialCustomerFunnelResponse struct {
	Stages          []TrialCustomerFunnelStage `json:"stages"`
	OwnerBreakdown  []TrialCustomerOwnerFunnel `json:"owner_breakdown"`
	BlockingReasons []TrialCustomerFunnelStage `json:"blocking_reasons"`
}

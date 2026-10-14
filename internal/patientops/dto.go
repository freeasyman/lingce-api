package patientops

type ScreenshotAnalyzeRequest struct {
	TenantID     int64  `json:"tenant_id"`
	EmployeeID   int64  `json:"employee_id"`
	CustomerID   *int64 `json:"customer_id,omitempty"`
	OriginalName string `json:"original_name"`
	ImageData    string `json:"image_data"`
}

type CreateProspectRequest struct {
	Name     string `json:"name"`
	Account  string `json:"account,omitempty"`
	Platform string `json:"platform,omitempty"`
}

type ScreenshotAnalysis struct {
	Platform            string   `json:"platform"`
	Account             string   `json:"account"`
	Nickname            string   `json:"nickname"`
	PatientUtterances   []string `json:"patient_utterances"`
	PatientQuestion     string   `json:"patient_question"`
	Concern             string   `json:"concern"`
	DecisionMaker       string   `json:"decision_maker"`
	ProductInterest     []string `json:"product_interest"`
	SuggestedIcebreaker string   `json:"suggested_icebreaker"`
	SuggestedReply      string   `json:"suggested_reply"`
	NextAction          string   `json:"next_action"`
	NextContactAt       string   `json:"next_contact_at"`
	Evidence            []string `json:"evidence"`
}

type CustomerCandidate struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Nickname string `json:"nickname"`
	Phone    string `json:"phone"`
	Reason   string `json:"reason"`
}

type ScreenshotAnalyzeResponse struct {
	SourceID    int64               `json:"source_id"`
	Analysis    ScreenshotAnalysis  `json:"analysis"`
	Candidates  []CustomerCandidate `json:"candidates"`
	MatchStatus string              `json:"match_status"`
}

type ConversationSourceResponse struct {
	ID           int64              `json:"id"`
	SourceType   string             `json:"source_type"`
	OriginalName string             `json:"original_name"`
	MediaData    string             `json:"media_data,omitempty"`
	CapturedAt   string             `json:"captured_at"`
	CustomerID   *int64             `json:"customer_id,omitempty"`
	MatchStatus  string             `json:"match_status"`
	Recognition  ScreenshotAnalysis `json:"recognition"`
}

type ActionRequest struct {
	SourceID   int64  `json:"source_id"`
	Action     string `json:"action"`
	Content    string `json:"content"`
	RecordType string `json:"record_type,omitempty"`
	TaskType   string `json:"task_type,omitempty"`
	Goal       string `json:"goal,omitempty"`
}

type ActionResponse struct {
	RecordID int64  `json:"record_id,omitempty"`
	DraftID  int64  `json:"draft_id,omitempty"`
	Status   string `json:"status"`
}

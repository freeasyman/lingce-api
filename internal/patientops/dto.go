package patientops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// StringList accepts the two shapes returned by vision models: an array of
// strings or a single string. The API exposes one stable array shape.
type StringList []string

func (s StringList) MarshalJSON() ([]byte, error) { return json.Marshal([]string(s)) }

func (s *StringList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		*s = nil
		return nil
	}
	var values []string
	if data[0] == '[' {
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		*s = values
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("expected string or array of strings: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		*s = nil
	} else {
		*s = []string{value}
	}
	return nil
}

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
	ConversationSummary string     `json:"conversation_summary"`
	Platform            string     `json:"platform"`
	Account             string     `json:"account"`
	Nickname            string     `json:"nickname"`
	PatientUtterances   StringList `json:"patient_utterances"`
	PatientQuestion     string     `json:"patient_question"`
	Concern             string     `json:"concern"`
	DecisionMaker       string     `json:"decision_maker"`
	ProductInterest     StringList `json:"product_interest"`
	SuggestedIcebreaker string     `json:"suggested_icebreaker"`
	SuggestedReply      string     `json:"suggested_reply"`
	NextAction          string     `json:"next_action"`
	NextContactAt       string     `json:"next_contact_at"`
	Evidence            StringList `json:"evidence"`
}

func (a *ScreenshotAnalysis) Normalize() {
	a.PatientUtterances = cleanStringList(a.PatientUtterances)
	a.ProductInterest = cleanStringList(a.ProductInterest)
	a.Evidence = cleanStringList(a.Evidence)
}

func cleanStringList(values StringList) StringList {
	result := make(StringList, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// Compile-time assertion documents that StringList remains assignable to the
// existing []string call sites through its defined slice type.

type CustomerCandidate struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Nickname string `json:"nickname"`
	Phone    string `json:"phone"`
	Reason   string `json:"reason"`
}

type ScreenshotAnalyzeResponse struct {
	SourceID       int64               `json:"source_id"`
	Analysis       ScreenshotAnalysis  `json:"analysis"`
	Candidates     []CustomerCandidate `json:"candidates"`
	MatchStatus    string              `json:"match_status"`
	AnalysisStatus string              `json:"analysis_status,omitempty"`
	AnalysisError  string              `json:"analysis_error,omitempty"`
	Reused         bool                `json:"reused,omitempty"`
}

type ScreenshotStatusResponse struct {
	SourceID       int64               `json:"source_id"`
	Analysis       *ScreenshotAnalysis `json:"analysis,omitempty"`
	Candidates     []CustomerCandidate `json:"candidates,omitempty"`
	MatchStatus    string              `json:"match_status"`
	AnalysisStatus string              `json:"analysis_status"`
	AnalysisError  string              `json:"analysis_error,omitempty"`
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

type GenerateActionRequest struct {
	SourceID int64  `json:"source_id"`
	Action   string `json:"action"`
}

type GenerateActionResponse struct {
	Action  string `json:"action"`
	Content string `json:"content"`
}

type ActionResponse struct {
	RecordID int64  `json:"record_id,omitempty"`
	DraftID  int64  `json:"draft_id,omitempty"`
	Status   string `json:"status"`
}

type OperationRecordResponse struct {
	ID         int64  `json:"id"`
	SourceID   *int64 `json:"source_id,omitempty"`
	ActionType string `json:"action_type"`
	RecordType string `json:"record_type"`
	Content    string `json:"content"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

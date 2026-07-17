package opportunityalert

import "time"

type ListRequest struct {
	Status   string
	Page     int
	PageSize int
}

type ActionRequest struct {
	Note   string `json:"note,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type AlertResponse struct {
	ID                int64       `json:"id"`
	TenantID          int64       `json:"tenant_id"`
	RecordingID       int64       `json:"recording_id"`
	EmployeeID        int64       `json:"employee_id"`
	CustomerID        *int64      `json:"customer_id,omitempty"`
	CustomerName      string      `json:"customer_name"`
	AlertType         string      `json:"alert_type"`
	Title             string      `json:"title"`
	Summary           string      `json:"summary"`
	Reason            string      `json:"reason"`
	CustomerObjection string      `json:"customer_objection"`
	Evidence          string      `json:"evidence"`
	SuggestedAction   string      `json:"suggested_action"`
	SuggestedScript   string      `json:"suggested_script"`
	Priority          string      `json:"priority"`
	Status            AlertStatus `json:"status"`
	ViewedAt          *string     `json:"viewed_at,omitempty"`
	HandledAt         *string     `json:"handled_at,omitempty"`
	IgnoredAt         *string     `json:"ignored_at,omitempty"`
	RecipientType     *string     `json:"recipient_type,omitempty"`
	RecipientReadAt   *string     `json:"recipient_read_at,omitempty"`
	CreatedAt         string      `json:"created_at"`
	UpdatedAt         string      `json:"updated_at"`
}

type CreateAlertInput struct {
	TenantID          int64
	RecordingID       int64
	EmployeeID        int64
	CustomerID        *int64
	CustomerName      string
	AlertType         string
	Title             string
	Summary           string
	Reason            string
	CustomerObjection string
	Evidence          string
	SuggestedAction   string
	SuggestedScript   string
	Priority          string
	DedupeKey         string
	RawPayload        JSONObject
}

func ToResponse(item *Alert) *AlertResponse {
	if item == nil {
		return nil
	}
	resp := &AlertResponse{
		ID:                item.ID,
		TenantID:          item.TenantID,
		RecordingID:       item.RecordingID,
		EmployeeID:        item.EmployeeID,
		CustomerID:        item.CustomerID,
		CustomerName:      item.CustomerName,
		AlertType:         item.AlertType,
		Title:             item.Title,
		Summary:           item.Summary,
		Reason:            item.Reason,
		CustomerObjection: item.CustomerObjection,
		Evidence:          item.Evidence,
		SuggestedAction:   item.SuggestedAction,
		SuggestedScript:   item.SuggestedScript,
		Priority:          item.Priority,
		Status:            item.Status,
		CreatedAt:         formatTime(item.CreatedAt),
		UpdatedAt:         formatTime(item.UpdatedAt),
	}
	if item.ViewedAt != nil {
		v := formatTime(*item.ViewedAt)
		resp.ViewedAt = &v
	}
	if item.HandledAt != nil {
		v := formatTime(*item.HandledAt)
		resp.HandledAt = &v
	}
	if item.IgnoredAt != nil {
		v := formatTime(*item.IgnoredAt)
		resp.IgnoredAt = &v
	}
	if item.RecipientType != nil {
		v := string(*item.RecipientType)
		resp.RecipientType = &v
	}
	if item.RecipientReadAt != nil {
		v := formatTime(*item.RecipientReadAt)
		resp.RecipientReadAt = &v
	}
	return resp
}

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}


type CCRuleResponse struct {
	ID             int64  `json:"id"`
	TenantID       int64  `json:"tenant_id"`
	EmployeeID     int64  `json:"employee_id"`
	EmployeeName   string `json:"employee_name"`
	CCEmployeeID   int64  `json:"cc_employee_id"`
	CCEmployeeName string `json:"cc_employee_name"`
	IsActive       bool   `json:"is_active"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type CreateCCRuleRequest struct {
	TenantID      int64 `json:"tenant_id"`
	EmployeeID    int64 `json:"employee_id"`
	CCEmployeeID  int64 `json:"cc_employee_id"`
}

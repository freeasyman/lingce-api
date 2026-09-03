package opportunityalert

import "time"

import "context"

type ListRequest struct {
	Status   string
	Page     int
	PageSize int
}

type AdminListRequest struct {
	TenantID *int64
	Status   string
	Keyword  string
	// 投递结果筛选：""/"all" 不过滤，"delivered" 全部送达，"undelivered" 有人未送达
	DeliveryResult string
	Page           int
	PageSize       int
}

type RecentDeliveriesRequest struct {
	TenantID *int64
	Limit    int
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
	DeliveredCount    int         `json:"delivered_count"`
	TotalRecipients   int         `json:"total_recipients"`
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

type MessageSendRequest struct {
	MessageScene string
	DedupeKey    string
	EmployeeIDs  []int64
	Title        string
	Content      string
	TargetURL    string
	ButtonText   string
	BizDate      *string
	Extra        any
}

type MessageSender interface {
	IsEnabled() bool
	SendInternalMessage(ctx context.Context, req MessageSendRequest) error
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
		DeliveredCount:    item.DeliveredCount,
		TotalRecipients:   item.TotalRecipients,
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
	TenantID     int64 `json:"tenant_id"`
	EmployeeID   int64 `json:"employee_id"`
	CCEmployeeID int64 `json:"cc_employee_id"`
}

type ResendAlertRequest struct {
	EmployeeIDs []int64 `json:"employee_ids,omitempty"`
}

type AlertRecipientResponse struct {
	AlertID           int64   `json:"alert_id"`
	TenantID          int64   `json:"tenant_id"`
	EmployeeID        int64   `json:"employee_id"`
	EmployeeName      string  `json:"employee_name"`
	EmployeePhone     string  `json:"employee_phone,omitempty"`
	RecipientType     string  `json:"recipient_type"`
	DeliveryStatus    string  `json:"delivery_status,omitempty"`
	WeComMessageLogID *int64  `json:"wecom_message_log_id,omitempty"`
	WeComUserID       string  `json:"wecom_user_id,omitempty"`
	SentAt            *string `json:"sent_at,omitempty"`
	ReadAt            *string `json:"read_at,omitempty"`
	UpdatedAt         string  `json:"updated_at"`
}

type AlertDeliveryLogResponse struct {
	ID              int64      `json:"id"`
	TenantID        int64      `json:"tenant_id"`
	EmployeeID      int64      `json:"employee_id"`
	EmployeeName    string     `json:"employee_name"`
	WeComUserID     string     `json:"wecom_user_id,omitempty"`
	MessageScene    string     `json:"message_scene"`
	DedupeKey       string     `json:"dedupe_key"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	TargetURL       string     `json:"target_url,omitempty"`
	Status          string     `json:"status"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	RequestPayload  JSONObject `json:"request_payload"`
	ResponsePayload JSONObject `json:"response_payload"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
}

type RecentDeliveryResponse struct {
	AlertID        int64   `json:"alert_id"`
	TenantID       int64   `json:"tenant_id"`
	RecordingID    int64   `json:"recording_id"`
	EmployeeID     int64   `json:"employee_id"`
	EmployeeName   string  `json:"employee_name"`
	CustomerName   string  `json:"customer_name"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	RecipientType  string  `json:"recipient_type"`
	DeliveryStatus string  `json:"delivery_status"`
	WeComUserID    string  `json:"wecom_user_id,omitempty"`
	SentAt         *string `json:"sent_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

func ToRecipientResponse(item *AlertRecipient) *AlertRecipientResponse {
	if item == nil {
		return nil
	}
	resp := &AlertRecipientResponse{
		AlertID:           item.AlertID,
		TenantID:          item.TenantID,
		EmployeeID:        item.EmployeeID,
		EmployeeName:      item.EmployeeName,
		EmployeePhone:     item.EmployeePhone,
		RecipientType:     item.RecipientType,
		DeliveryStatus:    item.DeliveryStatus,
		WeComMessageLogID: item.WeComMessageLogID,
		WeComUserID:       item.WeComUserID,
		UpdatedAt:         formatTime(item.UpdatedAt),
	}
	if item.SentAt != nil {
		v := formatTime(*item.SentAt)
		resp.SentAt = &v
	}
	if item.ReadAt != nil {
		v := formatTime(*item.ReadAt)
		resp.ReadAt = &v
	}
	return resp
}

func ToDeliveryLogResponse(item *AlertDeliveryLog) *AlertDeliveryLogResponse {
	if item == nil {
		return nil
	}
	return &AlertDeliveryLogResponse{
		ID:              item.ID,
		TenantID:        item.TenantID,
		EmployeeID:      item.EmployeeID,
		EmployeeName:    item.EmployeeName,
		WeComUserID:     item.WeComUserID,
		MessageScene:    item.MessageScene,
		DedupeKey:       item.DedupeKey,
		Title:           item.Title,
		Content:         item.Content,
		TargetURL:       item.TargetURL,
		Status:          item.Status,
		ErrorMessage:    item.ErrorMessage,
		RequestPayload:  item.RequestPayload,
		ResponsePayload: item.ResponsePayload,
		CreatedAt:       formatTime(item.CreatedAt),
		UpdatedAt:       formatTime(item.UpdatedAt),
	}
}

func ToRecentDeliveryResponse(item *RecentDelivery) *RecentDeliveryResponse {
	if item == nil {
		return nil
	}
	resp := &RecentDeliveryResponse{
		AlertID:        item.AlertID,
		TenantID:       item.TenantID,
		RecordingID:    item.RecordingID,
		EmployeeID:     item.EmployeeID,
		EmployeeName:   item.EmployeeName,
		CustomerName:   item.CustomerName,
		Title:          item.Title,
		Status:         item.Status,
		RecipientType:  item.RecipientType,
		DeliveryStatus: item.DeliveryStatus,
		WeComUserID:    item.WeComUserID,
		CreatedAt:      formatTime(item.CreatedAt),
	}
	if item.SentAt != nil {
		v := formatTime(*item.SentAt)
		resp.SentAt = &v
	}
	return resp
}

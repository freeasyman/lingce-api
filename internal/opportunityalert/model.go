package opportunityalert

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

type JSONObject map[string]interface{}

func (j JSONObject) Value() (driver.Value, error) {
	if j == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(j)
}

func (j *JSONObject) Scan(value interface{}) error {
	if value == nil {
		*j = JSONObject{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		*j = JSONObject{}
		return nil
	}
	if len(bytes) == 0 {
		*j = JSONObject{}
		return nil
	}
	return json.Unmarshal(bytes, j)
}

type AlertStatus string

const (
	AlertStatusPending AlertStatus = "pending"
	AlertStatusViewed  AlertStatus = "viewed"
	AlertStatusHandled AlertStatus = "handled"
	AlertStatusIgnored AlertStatus = "ignored"
)

type RecipientType string

const (
	RecipientTypeOwner RecipientType = "owner"
	RecipientTypeCC    RecipientType = "cc"
)

type Alert struct {
	ID                int64
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
	Status            AlertStatus
	ViewedAt          *time.Time
	HandledAt         *time.Time
	IgnoredAt         *time.Time
	DedupeKey         string
	RawPayload        JSONObject
	CreatedAt         time.Time
	UpdatedAt         time.Time
	RecipientType     *RecipientType
	RecipientReadAt   *time.Time
}

type AlertRecipient struct {
	AlertID            int64
	TenantID           int64
	EmployeeID         int64
	EmployeeName       string
	EmployeePhone      string
	RecipientType      string
	DeliveryStatus     string
	WeComMessageLogID  *int64
	WeComUserID        string
	SentAt             *time.Time
	ReadAt             *time.Time
	UpdatedAt          time.Time
}

type AlertDeliveryLog struct {
	ID              int64
	TenantID        int64
	EmployeeID      int64
	EmployeeName    string
	WeComUserID     string
	MessageScene    string
	DedupeKey       string
	Title           string
	Content         string
	TargetURL       string
	Status          string
	ErrorMessage    string
	RequestPayload  JSONObject
	ResponsePayload JSONObject
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type RecordingAlertSource struct {
	ID              int64
	TenantID        int64
	EmployeeID      int64
	CustomerID      *int64
	CustomerName    string
	BusinessScope   string
	AnalysisResult  JSONObject
	AnalysisDisplay JSONObject
}

type CCRule struct {
	ID             int64
	TenantID       int64
	EmployeeID     int64
	EmployeeName   string
	CCEmployeeID   int64
	CCEmployeeName string
	IsActive       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

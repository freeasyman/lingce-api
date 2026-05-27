package wecom

import "encoding/xml"

type EncryptedCallbackEnvelope struct {
	XMLName xml.Name `xml:"xml"`
	ToUser  string   `xml:"ToUserName"`
	AgentID string   `xml:"AgentID"`
	Encrypt string   `xml:"Encrypt"`
}

type CallbackEvent struct {
	XMLName       xml.Name `xml:"xml"`
	SuiteID       string   `xml:"SuiteId"`
	InfoType      string   `xml:"InfoType"`
	TimeStamp     string   `xml:"TimeStamp"`
	SuiteTicket   string   `xml:"SuiteTicket"`
	AuthCorpID    string   `xml:"AuthCorpId"`
	AuthCode      string   `xml:"AuthCode"`
	PermanentCode string   `xml:"PermanentCode"`
}

type SuiteTicketRecord struct {
	SuiteID     string
	SuiteTicket string
}

type CorpInstallRecord struct {
	CorpID        string
	CorpName      string
	PermanentCode string
	AgentID       int64
	Status        string
}

type UserBindingRecord struct {
	CorpID      string
	WeComUserID string
	EmployeeID  int64
	TenantID    int64
	Source      string
}

type OAuthUserProfile struct {
	CorpID      string `json:"corp_id"`
	WeComUserID string `json:"wecom_user_id"`
	OpenUserID  string `json:"open_user_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Mobile      string `json:"mobile,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
}

type EmployeeBindingRecord struct {
	CorpID        string
	CorpName      string
	WeComUserID   string
	EmployeeID    int64
	TenantID      int64
	PermanentCode string
	AgentID       int64
}

type MessageLogRecord struct {
	ID              int64
	CorpID          string
	TenantID        int64
	EmployeeID      int64
	WeComUserID     string
	MessageScene    string
	DedupeKey       string
	Title           string
	Content         string
	TargetURL       string
	Status          string
	ErrorMessage    string
	RequestPayload  string
	ResponsePayload string
	BizDate         *string
}

package wecom

import "encoding/xml"
import "time"

type EncryptedCallbackEnvelope struct {
	XMLName xml.Name `xml:"xml"`
	ToUser  string   `xml:"ToUserName"`
	AgentID string   `xml:"AgentID"`
	Encrypt string   `xml:"Encrypt"`
}

type CallbackEvent struct {
	XMLName       xml.Name `xml:"xml"`
	SuiteID       string   `xml:"SuiteId,omitempty"`
	InfoType      string   `xml:"InfoType,omitempty"`
	TimeStamp     string   `xml:"TimeStamp,omitempty"`
	SuiteTicket   string   `xml:"SuiteTicket,omitempty"`
	AuthCorpID    string   `xml:"AuthCorpId,omitempty"`
	AuthCode      string   `xml:"AuthCode,omitempty"`
	PermanentCode string   `xml:"PermanentCode,omitempty"`
}

type UserBindingRecord struct {
	ID          int64
	CorpID      string
	WeComUserID string
	EmployeeID  int64
	TenantID    int64
	Source      string
}

type BindingStatusRecord struct {
	BindingID      *int64
	TenantID       int64
	TenantName     string
	EmployeeID     int64
	EmployeeName   string
	EmployeePhone  string
	CorpID         string
	CorpName       string
	WeComUserID    string
	Source         string
	AppEnabled     bool
	IsBound        bool
	BoundAt        *time.Time
	BindingUpdated *time.Time
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
	CorpID      string
	CorpName    string
	WeComUserID string
	EmployeeID  int64
	TenantID    int64
	AgentID     int64
}

type TenantWeComAppRecord struct {
	ID                   int64
	TenantID             int64
	CorpID               string
	CorpName             string
	AgentID              int64
	SecretCiphertext     string
	Token                string
	EncodingAESKey       string
	HomeURL              string
	TrustedDomain        string
	JSAPIDomain          string
	Enabled              bool
	ConfigConfirmed      bool
	AccessToken          string
	AccessTokenExpiredAt *time.Time
	LastSyncAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
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

type DirectoryMemberRecord struct {
	ID                int64
	TenantID          int64
	CorpID            string
	WeComUserID       string
	Name              string
	Mobile            string
	DepartmentIDsJSON string
	WeComStatus       int
	MatchStatus       string
	MatchedEmployeeID *int64
	LastSyncedAt      time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type DirectoryMemberListRecord struct {
	ID                  int64
	TenantID            int64
	TenantName          string
	CorpID              string
	CorpName            string
	WeComUserID         string
	Name                string
	Mobile              string
	MatchStatus         string
	MatchedEmployeeID   *int64
	MatchedEmployeeName string
	BindingEmployeeID   *int64
	BindingSource       string
	LastSyncedAt        time.Time
}

type DirectorySyncSummary struct {
	TenantID       int64
	CorpID         string
	SyncedCount    int
	PreboundCount  int
	MatchedCount   int
	ConflictCount  int
	UnmatchedCount int
}

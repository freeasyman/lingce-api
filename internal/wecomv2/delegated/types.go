package delegated

import "encoding/xml"

type OAuthLoginRequest struct {
	Code   string `json:"code"`
	CorpID string `json:"corp_id,omitempty"`
}

type OAuthUserProfile struct {
	CorpID      string `json:"corp_id"`
	WeComUserID string `json:"wecom_user_id"`
	OpenUserID  string `json:"open_user_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Mobile      string `json:"mobile,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
}

type OAuthLoginResponse struct {
	Status    string            `json:"status"`
	Reason    string            `json:"reason,omitempty"`
	AutoBound bool              `json:"auto_bound,omitempty"`
	Auth      any               `json:"auth,omitempty"`
	Profile   *OAuthUserProfile `json:"profile,omitempty"`
}

type BindRequest struct {
	CorpID      string `json:"corp_id"`
	WeComUserID string `json:"wecom_user_id"`
}

type CorpInstallResponse struct {
	ID             string  `json:"id"`
	ProviderApp    string  `json:"provider_app"`
	TenantID       int64   `json:"tenant_id"`
	CorpID         string  `json:"corp_id"`
	CorpName       string  `json:"corp_name,omitempty"`
	AgentID        int64   `json:"agent_id"`
	Status         string  `json:"status"`
	HasPermanent   bool    `json:"has_permanent_code"`
	LaunchURL      string  `json:"launch_url,omitempty"`
	TrustedDomain  string  `json:"trusted_domain,omitempty"`
	CallbackURL    string  `json:"callback_url,omitempty"`
	Token          string  `json:"token,omitempty"`
	EncodingAESKey string  `json:"encoding_aes_key,omitempty"`
	UpdatedAt      *string `json:"updated_at,omitempty"`
	CancelledAt    *string `json:"cancelled_at,omitempty"`
}

type CorpInstallDetailResponse struct {
	Install      *CorpInstallResponse `json:"install"`
	RecentEvents []*EventLogResponse  `json:"recent_events"`
}

type DelegatedAppOverviewResponse struct {
	ProviderApp        string              `json:"provider_app"`
	TemplateConnected  bool                `json:"template_connected"`
	LastSuiteTicketAt  *string             `json:"last_suite_ticket_at,omitempty"`
	ActiveInstallCount int64               `json:"active_install_count"`
	RecentEvents       []*EventLogResponse `json:"recent_events"`
}

type EventLogResponse struct {
	ID         string  `json:"id"`
	CorpID     string  `json:"corp_id,omitempty"`
	InfoType   string  `json:"info_type"`
	RawPayload string  `json:"raw_payload"`
	CreatedAt  *string `json:"created_at,omitempty"`
}

type CorpInstallBindRequest struct {
	ProviderApp string `json:"provider_app"`
	CorpID      string `json:"corp_id"`
	TenantID    int64  `json:"tenant_id"`
}

type InternalSendMessageRequest struct {
	MessageScene string  `json:"message_scene"`
	DedupeKey    string  `json:"dedupe_key"`
	EmployeeIDs  []int64 `json:"employee_ids"`
	Title        string  `json:"title"`
	Content      string  `json:"content"`
	TargetURL    string  `json:"target_url,omitempty"`
	ButtonText   string  `json:"button_text,omitempty"`
	BizDate      *string `json:"biz_date,omitempty"`
	Extra        any     `json:"extra,omitempty"`
}

type InternalSendMessageResponse struct {
	Requested int `json:"requested"`
	Sent      int `json:"sent"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

type encryptedCallbackEnvelope struct {
	XMLName xml.Name `xml:"xml"`
	Encrypt string   `xml:"Encrypt"`
}

type callbackEvent struct {
	XMLName     xml.Name `xml:"xml"`
	SuiteID     string   `xml:"SuiteId,omitempty"`
	InfoType    string   `xml:"InfoType,omitempty"`
	SuiteTicket string   `xml:"SuiteTicket,omitempty"`
	AuthCorpID  string   `xml:"AuthCorpId,omitempty"`
	AuthCode    string   `xml:"AuthCode,omitempty"`
}

type corpInstallRecord struct {
	ID            int64
	ProviderApp   string
	TenantID      int64
	CorpID        string
	CorpName      string
	PermanentCode string
	AgentID       int64
	Status        string
	UpdatedAt     *string
	CancelledAt   *string
}

type userBindingRecord struct {
	ID          int64
	ProviderApp string
	CorpID      string
	WeComUserID string
	EmployeeID  int64
	TenantID    int64
	Source      string
}

type employeeBindingRecord struct {
	ProviderApp string
	CorpID      string
	CorpName    string
	WeComUserID string
	EmployeeID  int64
	TenantID    int64
	AgentID     int64
}

type messageLogRecord struct {
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

type eventLogRecord struct {
	ID         int64
	CorpID     string
	InfoType   string
	RawPayload string
	CreatedAt  *string
}

type userInfo3rdResponse struct {
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
	CorpID     string `json:"CorpId"`
	UserID     string `json:"UserId"`
	OpenUserID string `json:"OpenUserId"`
	UserTicket string `json:"user_ticket"`
}

type userDetailResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	UserID  string `json:"userid"`
	Name    string `json:"name"`
	Mobile  string `json:"mobile"`
	Avatar  string `json:"avatar"`
}

package wecom

import internalauth "github.com/freeasyman/lingce-api/internal/auth"

type OAuthLoginRequest struct {
	Code   string `json:"code"`
	CorpID string `json:"corp_id,omitempty"`
}

type OAuthLoginResponse struct {
	Status    string                      `json:"status"`
	AutoBound bool                        `json:"auto_bound,omitempty"`
	Auth      *internalauth.LoginResponse `json:"auth,omitempty"`
	Profile   *OAuthUserProfile           `json:"profile,omitempty"`
}

type BindRequest struct {
	CorpID      string `json:"corp_id"`
	WeComUserID string `json:"wecom_user_id"`
}

type AdminBindingRequest struct {
	TenantID    int64  `json:"tenant_id"`
	EmployeeID  int64  `json:"employee_id"`
	CorpID      string `json:"corp_id,omitempty"`
	WeComUserID string `json:"wecom_user_id"`
}

type BindingStatusResponse struct {
	BindingID      *int64  `json:"binding_id,omitempty"`
	TenantID       int64   `json:"tenant_id"`
	TenantName     string  `json:"tenant_name,omitempty"`
	EmployeeID     int64   `json:"employee_id"`
	EmployeeName   string  `json:"employee_name"`
	EmployeePhone  string  `json:"employee_phone"`
	CorpID         string  `json:"corp_id,omitempty"`
	CorpName       string  `json:"corp_name,omitempty"`
	WeComUserID    string  `json:"wecom_user_id,omitempty"`
	Source         string  `json:"source,omitempty"`
	AppEnabled     bool    `json:"app_enabled"`
	IsBound        bool    `json:"is_bound"`
	BoundAt        *string `json:"bound_at,omitempty"`
	BindingUpdated *string `json:"binding_updated_at,omitempty"`
}

type DirectoryMemberListParams struct {
	TenantID *int64
	Keyword  string
	Status   string
	Page     int
	PageSize int
}

type DirectoryMemberResponse struct {
	ID                  int64   `json:"id"`
	TenantID            int64   `json:"tenant_id"`
	TenantName          string  `json:"tenant_name,omitempty"`
	CorpID              string  `json:"corp_id"`
	CorpName            string  `json:"corp_name,omitempty"`
	WeComUserID         string  `json:"wecom_user_id"`
	Name                string  `json:"name"`
	Mobile              string  `json:"mobile,omitempty"`
	MatchStatus         string  `json:"match_status"`
	MatchedEmployeeID   *int64  `json:"matched_employee_id,omitempty"`
	MatchedEmployeeName string  `json:"matched_employee_name,omitempty"`
	BindingEmployeeID   *int64  `json:"binding_employee_id,omitempty"`
	BindingSource       string  `json:"binding_source,omitempty"`
	LastSyncedAt        *string `json:"last_synced_at,omitempty"`
}

type DirectorySyncRequest struct {
	TenantID int64 `json:"tenant_id"`
}

type DirectorySyncResponse struct {
	TenantID       int64  `json:"tenant_id"`
	CorpID         string `json:"corp_id"`
	SyncedCount    int    `json:"synced_count"`
	PreboundCount  int    `json:"prebound_count"`
	MatchedCount   int    `json:"matched_count"`
	ConflictCount  int    `json:"conflict_count"`
	UnmatchedCount int    `json:"unmatched_count"`
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

type TenantWeComAppRequest struct {
	TenantID        int64  `json:"tenant_id"`
	CorpID          string `json:"corp_id"`
	CorpName        string `json:"corp_name,omitempty"`
	AgentID         int64  `json:"agent_id"`
	Secret          string `json:"secret,omitempty"`
	Token           string `json:"token,omitempty"`
	EncodingAESKey  string `json:"encoding_aes_key,omitempty"`
	HomeURL         string `json:"home_url,omitempty"`
	TrustedDomain   string `json:"trusted_domain,omitempty"`
	JSAPIDomain     string `json:"jsapi_domain,omitempty"`
	Enabled         bool   `json:"enabled"`
	ConfigConfirmed bool   `json:"config_confirmed"`
}

type TenantWeComAppResponse struct {
	ID                   int64   `json:"id"`
	TenantID             int64   `json:"tenant_id"`
	TenantName           string  `json:"tenant_name,omitempty"`
	CorpID               string  `json:"corp_id"`
	CorpName             string  `json:"corp_name,omitempty"`
	AgentID              int64   `json:"agent_id"`
	SecretMasked         string  `json:"secret_masked,omitempty"`
	HasSecret            bool    `json:"has_secret"`
	HasToken             bool    `json:"has_token"`
	HasEncodingAESKey    bool    `json:"has_encoding_aes_key"`
	Token                string  `json:"token,omitempty"`
	EncodingAESKey       string  `json:"encoding_aes_key,omitempty"`
	HomeURL              string  `json:"home_url,omitempty"`
	TrustedDomain        string  `json:"trusted_domain,omitempty"`
	JSAPIDomain          string  `json:"jsapi_domain,omitempty"`
	Enabled              bool    `json:"enabled"`
	ConfigConfirmed      bool    `json:"config_confirmed"`
	AccessTokenExpiredAt *string `json:"access_token_expired_at,omitempty"`
	LastSyncAt           *string `json:"last_sync_at,omitempty"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

type InstallURLResponse struct {
	InstallURL  string `json:"install_url"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	AuthType    int    `json:"auth_type"`
}

type InstallCallbackResult struct {
	TenantID int64  `json:"tenant_id,omitempty"`
	CorpID   string `json:"corp_id"`
	CorpName string `json:"corp_name,omitempty"`
}

type CorpInstallResponse struct {
	TenantID     int64   `json:"tenant_id"`
	CorpID       string  `json:"corp_id"`
	CorpName     string  `json:"corp_name,omitempty"`
	AgentID      int64   `json:"agent_id"`
	Status       string  `json:"status"`
	HasPermanent bool    `json:"has_permanent_code"`
	UpdatedAt    *string `json:"updated_at,omitempty"`
	CancelledAt  *string `json:"cancelled_at,omitempty"`
}

type EventLogSummaryResponse struct {
	CorpID    string  `json:"corp_id,omitempty"`
	InfoType  string  `json:"info_type"`
	CreatedAt *string `json:"created_at,omitempty"`
}

type PartnerModeStatusResponse struct {
	Mode               string                     `json:"mode"`
	ProviderApp        string                     `json:"provider_app"`
	SuiteID            string                     `json:"suite_id"`
	Configured         bool                       `json:"configured"`
	HasSuiteTicket     bool                       `json:"has_suite_ticket"`
	LastSuiteTicketAt  *string                    `json:"last_suite_ticket_at,omitempty"`
	ActiveInstallCount int                        `json:"active_install_count"`
	RecentEvents       []*EventLogSummaryResponse `json:"recent_events"`
}

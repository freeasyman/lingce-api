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
	TenantID       int64  `json:"tenant_id"`
	CorpID         string `json:"corp_id"`
	CorpName       string `json:"corp_name,omitempty"`
	AgentID        int64  `json:"agent_id"`
	Secret         string `json:"secret,omitempty"`
	Token          string `json:"token,omitempty"`
	EncodingAESKey string `json:"encoding_aes_key,omitempty"`
	HomeURL        string `json:"home_url,omitempty"`
	TrustedDomain  string `json:"trusted_domain,omitempty"`
	JSAPIDomain    string `json:"jsapi_domain,omitempty"`
	Enabled        bool   `json:"enabled"`
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
	HomeURL              string  `json:"home_url,omitempty"`
	TrustedDomain        string  `json:"trusted_domain,omitempty"`
	JSAPIDomain          string  `json:"jsapi_domain,omitempty"`
	Enabled              bool    `json:"enabled"`
	AccessTokenExpiredAt *string `json:"access_token_expired_at,omitempty"`
	LastSyncAt           *string `json:"last_sync_at,omitempty"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
}

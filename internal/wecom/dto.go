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

type InstallURLResponse struct {
	InstallURL  string `json:"install_url"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	AuthType    int    `json:"auth_type"`
}

type InstallCallbackResult struct {
	CorpID   string `json:"corp_id"`
	CorpName string `json:"corp_name,omitempty"`
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

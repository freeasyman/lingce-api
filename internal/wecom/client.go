package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL     string
	suiteID     string
	suiteSecret string
	httpClient  *http.Client
}

type apiErrorResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type suiteTokenResponse struct {
	apiErrorResponse
	SuiteAccessToken string `json:"suite_access_token"`
	ExpiresIn        int64  `json:"expires_in"`
}

type preAuthCodeResponse struct {
	apiErrorResponse
	PreAuthCode string `json:"pre_auth_code"`
	ExpiresIn   int64  `json:"expires_in"`
}

type permanentCodeResponse struct {
	apiErrorResponse
	AuthCorpInfo struct {
		CorpID string `json:"corpid"`
	} `json:"auth_corp_info"`
	PermanentCode string `json:"permanent_code"`
}

type authInfoResponse struct {
	apiErrorResponse
	AuthCorpInfo struct {
		CorpID   string `json:"corpid"`
		CorpName string `json:"corp_name"`
	} `json:"auth_corp_info"`
	AuthInfo struct {
		Agent []struct {
			AgentID int64 `json:"agentid"`
		} `json:"agent"`
	} `json:"auth_info"`
}

type corpTokenResponse struct {
	apiErrorResponse
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

type userInfo3rdResponse struct {
	apiErrorResponse
	CorpID     string `json:"CorpId"`
	UserID     string `json:"UserId"`
	OpenUserID string `json:"OpenUserId"`
	UserTicket string `json:"user_ticket"`
}

type userDetailResponse struct {
	apiErrorResponse
	UserID string `json:"userid"`
	Name   string `json:"name"`
	Mobile string `json:"mobile"`
	Avatar string `json:"avatar"`
}

type sendMessageResponse struct {
	apiErrorResponse
	InvalidUser string `json:"invaliduser"`
}

func NewClient(baseURL, suiteID, suiteSecret string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://qyapi.weixin.qq.com"
	}
	return &Client{
		baseURL:     baseURL,
		suiteID:     suiteID,
		suiteSecret: suiteSecret,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) GetSuiteAccessToken(ctx context.Context, suiteTicket string) (string, int64, error) {
	var resp suiteTokenResponse
	err := c.postJSON(ctx, "/cgi-bin/service/get_suite_token", map[string]string{
		"suite_id":     c.suiteID,
		"suite_secret": c.suiteSecret,
		"suite_ticket": suiteTicket,
	}, &resp)
	if err != nil {
		return "", 0, err
	}
	return resp.SuiteAccessToken, resp.ExpiresIn, nil
}

func (c *Client) GetPreAuthCode(ctx context.Context, suiteAccessToken string) (string, int64, error) {
	var resp preAuthCodeResponse
	path := "/cgi-bin/service/get_pre_auth_code?suite_access_token=" + url.QueryEscape(suiteAccessToken)
	err := c.getJSON(ctx, path, &resp)
	if err != nil {
		return "", 0, err
	}
	return resp.PreAuthCode, resp.ExpiresIn, nil
}

func (c *Client) SetSessionInfo(ctx context.Context, suiteAccessToken, preAuthCode string, authType int) error {
	path := "/cgi-bin/service/set_session_info?suite_access_token=" + url.QueryEscape(suiteAccessToken)
	payload := map[string]any{
		"pre_auth_code": preAuthCode,
		"session_info": map[string]any{
			"auth_type": authType,
		},
	}
	var resp apiErrorResponse
	return c.postJSON(ctx, path, payload, &resp)
}

func (c *Client) GetPermanentCode(ctx context.Context, suiteAccessToken, authCode string) (*permanentCodeResponse, error) {
	var resp permanentCodeResponse
	path := "/cgi-bin/service/get_permanent_code?suite_access_token=" + url.QueryEscape(suiteAccessToken)
	err := c.postJSON(ctx, path, map[string]string{"auth_code": authCode}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetAuthInfo(ctx context.Context, suiteAccessToken, corpID, permanentCode string) (*authInfoResponse, error) {
	var resp authInfoResponse
	path := "/cgi-bin/service/get_auth_info?suite_access_token=" + url.QueryEscape(suiteAccessToken)
	err := c.postJSON(ctx, path, map[string]string{
		"auth_corpid":    corpID,
		"permanent_code": permanentCode,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetCorpToken(ctx context.Context, suiteAccessToken, corpID, permanentCode string) (string, int64, error) {
	var resp corpTokenResponse
	path := "/cgi-bin/service/get_corp_token?suite_access_token=" + url.QueryEscape(suiteAccessToken)
	err := c.postJSON(ctx, path, map[string]string{
		"auth_corpid":    corpID,
		"permanent_code": permanentCode,
	}, &resp)
	if err != nil {
		return "", 0, err
	}
	return resp.AccessToken, resp.ExpiresIn, nil
}

func (c *Client) GetUserInfo3rd(ctx context.Context, suiteAccessToken, code string) (*userInfo3rdResponse, error) {
	var resp userInfo3rdResponse
	path := "/cgi-bin/service/getuserinfo3rd?suite_access_token=" + url.QueryEscape(suiteAccessToken)
	err := c.postJSON(ctx, path, map[string]string{"code": code}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetUserDetail(ctx context.Context, corpAccessToken, userID string) (*userDetailResponse, error) {
	var resp userDetailResponse
	path := "/cgi-bin/user/get?access_token=" + url.QueryEscape(corpAccessToken) + "&userid=" + url.QueryEscape(userID)
	err := c.getJSON(ctx, path, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) SendTextCardMessage(ctx context.Context, corpAccessToken string, agentID int64, toUser, title, description, targetURL, buttonText string) (*sendMessageResponse, error) {
	var resp sendMessageResponse
	path := "/cgi-bin/message/send?access_token=" + url.QueryEscape(corpAccessToken)
	payload := map[string]any{
		"touser":  toUser,
		"msgtype": "textcard",
		"agentid": agentID,
		"textcard": map[string]any{
			"title":       title,
			"description": description,
			"url":         targetURL,
			"btntxt":      buttonText,
		},
		"safe": 0,
	}
	if strings.TrimSpace(buttonText) == "" {
		delete(payload["textcard"].(map[string]any), "btntxt")
	}
	if err := c.postJSON(ctx, path, payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode wecom response: %w", err)
	}
	if apiErr := checkAPIError(out); apiErr != nil {
		return apiErr
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode wecom response: %w", err)
	}
	if apiErr := checkAPIError(out); apiErr != nil {
		return apiErr
	}
	return nil
}

func checkAPIError(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var apiErr apiErrorResponse
	if err := json.Unmarshal(b, &apiErr); err != nil {
		return nil
	}
	if apiErr.ErrCode != 0 {
		return fmt.Errorf("wecom api error: %d %s", apiErr.ErrCode, apiErr.ErrMsg)
	}
	return nil
}

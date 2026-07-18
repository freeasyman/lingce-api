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
	baseURL    string
	httpClient *http.Client
}

type apiErrorResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
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

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://qyapi.weixin.qq.com"
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) GetCorpAccessToken(ctx context.Context, corpID, corpSecret string) (string, int64, error) {
	var resp corpTokenResponse
	err := c.postJSON(ctx, "/cgi-bin/gettoken", map[string]string{
		"corpid":     corpID,
		"corpsecret": corpSecret,
	}, &resp)
	if err != nil {
		return "", 0, err
	}
	return resp.AccessToken, resp.ExpiresIn, nil
}

func (c *Client) GetCorpUserInfo(ctx context.Context, corpAccessToken, code string) (*userInfo3rdResponse, error) {
	var resp userInfo3rdResponse
	path := "/cgi-bin/auth/getuserinfo?access_token=" + url.QueryEscape(corpAccessToken) + "&code=" + url.QueryEscape(code)
	err := c.getJSON(ctx, path, &resp)
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

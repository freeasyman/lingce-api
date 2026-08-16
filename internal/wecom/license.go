package wecom

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	LicenseAccountTypeBase = 1
)

type ProviderTokenResponse struct {
	apiErrorResponse
	ProviderAccessToken string `json:"provider_access_token"`
	ExpiresIn           int64  `json:"expires_in"`
}

type LicenseOrder struct {
	OrderID     string `json:"order_id"`
	OrderType   int    `json:"order_type"`
	OrderStatus int    `json:"order_status"`
	CreateTime  int64  `json:"create_time"`
	PayTime     int64  `json:"pay_time"`
}

type LicenseOrderAccount struct {
	ActiveCode string `json:"active_code"`
	UserID     string `json:"userid"`
	Type       int    `json:"type"`
}

type LicenseActiveInfo struct {
	ActiveCode string `json:"-"`
	Type       int    `json:"type"`
	UserID     string `json:"userid"`
	ActiveTime int64  `json:"active_time"`
	ExpireTime int64  `json:"expire_time"`
}

type LicenseActivationResult struct {
	AlreadyActive bool                `json:"already_active"`
	ActiveStatus  int                 `json:"active_status"`
	Expired       bool                `json:"expired"`
	ActiveInfo    []LicenseActiveInfo `json:"active_info_list,omitempty"`
	OrderID       string              `json:"order_id,omitempty"`
}

type LicenseClient struct {
	client         *Client
	providerCorpID string
	providerSecret string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func NewLicenseClient(baseURL, providerCorpID, providerSecret string) *LicenseClient {
	return &LicenseClient{
		client:         NewClient(baseURL),
		providerCorpID: strings.TrimSpace(providerCorpID),
		providerSecret: strings.TrimSpace(providerSecret),
	}
}

func (c *LicenseClient) IsEnabled() bool {
	return c != nil && c.client != nil && c.providerCorpID != "" && c.providerSecret != ""
}

func (c *LicenseClient) getProviderAccessToken(ctx context.Context) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("wecom provider credentials are not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Add(2*time.Minute).Before(c.expiresAt) {
		return c.token, nil
	}
	var resp ProviderTokenResponse
	if err := c.client.postJSON(ctx, "/cgi-bin/service/get_provider_token", map[string]string{
		"corpid":          c.providerCorpID,
		"provider_secret": c.providerSecret,
	}, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.ProviderAccessToken) == "" {
		return "", fmt.Errorf("wecom returned empty provider access token")
	}
	expiresIn := resp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
	}
	c.token = resp.ProviderAccessToken
	c.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return c.token, nil
}

func (c *LicenseClient) GetActiveInfoByUser(ctx context.Context, corpID, userID string) (int, []LicenseActiveInfo, error) {
	token, err := c.getProviderAccessToken(ctx)
	if err != nil {
		return 0, nil, err
	}
	var resp struct {
		apiErrorResponse
		ActiveStatus   int                 `json:"active_status"`
		ActiveInfoList []LicenseActiveInfo `json:"active_info_list"`
	}
	path := "/cgi-bin/license/get_active_info_by_user?provider_access_token=" + url.QueryEscape(token)
	if err := c.client.postJSON(ctx, path, map[string]string{
		"corpid": strings.TrimSpace(corpID),
		"userid": strings.TrimSpace(userID),
	}, &resp); err != nil {
		return 0, nil, err
	}
	return resp.ActiveStatus, resp.ActiveInfoList, nil
}

func (c *LicenseClient) ActivateAccount(ctx context.Context, corpID, userID, activeCode string) error {
	token, err := c.getProviderAccessToken(ctx)
	if err != nil {
		return err
	}
	var resp apiErrorResponse
	path := "/cgi-bin/license/active_account?provider_access_token=" + url.QueryEscape(token)
	return c.client.postJSON(ctx, path, map[string]string{
		"corpid":      strings.TrimSpace(corpID),
		"userid":      strings.TrimSpace(userID),
		"active_code": strings.TrimSpace(activeCode),
	}, &resp)
}

func (c *LicenseClient) ListOrders(ctx context.Context, startTime, endTime int64, cursor string, limit int) ([]LicenseOrder, string, bool, error) {
	token, err := c.getProviderAccessToken(ctx)
	if err != nil {
		return nil, "", false, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	payload := map[string]any{
		"start_time": startTime,
		"end_time":   endTime,
		"limit":      limit,
	}
	if strings.TrimSpace(cursor) != "" {
		payload["cursor"] = strings.TrimSpace(cursor)
	}
	var resp struct {
		apiErrorResponse
		OrderList  []LicenseOrder `json:"order_list"`
		NextCursor string         `json:"next_cursor"`
		HasMore    int            `json:"has_more"`
	}
	path := "/cgi-bin/license/list_order?provider_access_token=" + url.QueryEscape(token)
	if err := c.client.postJSON(ctx, path, payload, &resp); err != nil {
		return nil, "", false, err
	}
	return resp.OrderList, resp.NextCursor, resp.HasMore == 1, nil
}

func (c *LicenseClient) ListOrderAccounts(ctx context.Context, orderID, cursor string, limit int) ([]LicenseOrderAccount, string, bool, error) {
	token, err := c.getProviderAccessToken(ctx)
	if err != nil {
		return nil, "", false, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	payload := map[string]any{
		"order_id": strings.TrimSpace(orderID),
		"limit":    limit,
	}
	if strings.TrimSpace(cursor) != "" {
		payload["cursor"] = strings.TrimSpace(cursor)
	}
	var resp struct {
		apiErrorResponse
		AccountList []LicenseOrderAccount `json:"account_list"`
		NextCursor  string                `json:"next_cursor"`
		HasMore     int                   `json:"has_more"`
	}
	path := "/cgi-bin/license/list_order_account?provider_access_token=" + url.QueryEscape(token)
	if err := c.client.postJSON(ctx, path, payload, &resp); err != nil {
		return nil, "", false, err
	}
	return resp.AccountList, resp.NextCursor, resp.HasMore == 1, nil
}

type LicenseService struct {
	client *LicenseClient
}

func NewLicenseService(client *LicenseClient) *LicenseService {
	return &LicenseService{client: client}
}

func (s *LicenseService) IsEnabled() bool {
	return s != nil && s.client != nil && s.client.IsEnabled()
}

func (s *LicenseService) Activate(ctx context.Context, corpID, userID, activeCode string) (*LicenseActivationResult, error) {
	corpID = strings.TrimSpace(corpID)
	userID = strings.TrimSpace(userID)
	activeCode = strings.TrimSpace(activeCode)
	if corpID == "" || userID == "" {
		return nil, fmt.Errorf("corp_id and user_id are required")
	}
	status, info, err := s.client.GetActiveInfoByUser(ctx, corpID, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	for _, item := range info {
		if item.Type == LicenseAccountTypeBase && item.ExpireTime > now {
			return &LicenseActivationResult{
				AlreadyActive: true,
				ActiveStatus:  status,
				Expired:       false,
				ActiveInfo:    info,
			}, nil
		}
	}

	orderID := ""
	if activeCode == "" {
		activeCode, orderID, err = s.findAvailableBaseCode(ctx)
		if err != nil {
			return nil, err
		}
	}
	if err := s.client.ActivateAccount(ctx, corpID, userID, activeCode); err != nil {
		return nil, err
	}
	status, info, err = s.client.GetActiveInfoByUser(ctx, corpID, userID)
	if err != nil {
		return nil, err
	}
	return &LicenseActivationResult{
		ActiveStatus: status,
		Expired:      !hasValidBaseLicense(info, time.Now().Unix()),
		ActiveInfo:   info,
		OrderID:      orderID,
	}, nil
}

func (s *LicenseService) Status(ctx context.Context, corpID, userID string) (*LicenseActivationResult, error) {
	corpID = strings.TrimSpace(corpID)
	userID = strings.TrimSpace(userID)
	if corpID == "" || userID == "" {
		return nil, fmt.Errorf("corp_id and user_id are required")
	}
	status, info, err := s.client.GetActiveInfoByUser(ctx, corpID, userID)
	if err != nil {
		return nil, err
	}
	valid := hasValidBaseLicense(info, time.Now().Unix())
	return &LicenseActivationResult{
		AlreadyActive: valid,
		ActiveStatus:  status,
		Expired:       len(info) > 0 && !valid,
		ActiveInfo:    info,
	}, nil
}

func hasValidBaseLicense(info []LicenseActiveInfo, now int64) bool {
	for _, item := range info {
		if item.Type == LicenseAccountTypeBase && item.ExpireTime > now {
			return true
		}
	}
	return false
}

func (s *LicenseService) findAvailableBaseCode(ctx context.Context) (string, string, error) {
	now := time.Now().Unix()
	start := time.Now().AddDate(0, 0, -31).Unix()
	cursor := ""
	for {
		orders, nextCursor, hasMore, err := s.client.ListOrders(ctx, start, now, cursor, 1000)
		if err != nil {
			return "", "", err
		}
		for _, order := range orders {
			accountCursor := ""
			for {
				accounts, nextAccountCursor, accountsHaveMore, err := s.client.ListOrderAccounts(ctx, order.OrderID, accountCursor, 1000)
				if err != nil {
					return "", "", err
				}
				for _, account := range accounts {
					if account.Type == LicenseAccountTypeBase && strings.TrimSpace(account.UserID) == "" && strings.TrimSpace(account.ActiveCode) != "" {
						return account.ActiveCode, order.OrderID, nil
					}
				}
				if !accountsHaveMore {
					break
				}
				accountCursor = nextAccountCursor
			}
		}
		if !hasMore {
			break
		}
		cursor = nextCursor
	}
	return "", "", fmt.Errorf("no unused paid base-account license code is available")
}

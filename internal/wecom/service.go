package wecom

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	internalauth "github.com/freeasyman/lingce-api/internal/auth"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	jwtauth "github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5"
)

const defaultWeComEmployeeHost = "employee.khgl.xyz"
const (
	minWeComCorpIDLength = 8
	maxWeComCorpIDLength = 64
	minWeComSecretLength = 8
	maxWeComSecretLength = 256
)

type Service struct {
	store           *Store
	authStore       *internalauth.Store
	client          *Client
	secretProtector *SecretProtector
	jwtSecret       string
	jwtExpiryHours  int
	onBindingUpsert func(context.Context, int64, int64) error
}

func NewService(store *Store, authStore *internalauth.Store, client *Client, jwtSecret string, jwtExpiryHours int) *Service {
	return &Service{
		store:           store,
		authStore:       authStore,
		client:          client,
		secretProtector: NewSecretProtector(jwtSecret),
		jwtSecret:       jwtSecret,
		jwtExpiryHours:  jwtExpiryHours,
	}
}

func (s *Service) SetBindingUpsertHook(fn func(context.Context, int64, int64) error) {
	s.onBindingUpsert = fn
}

func (s *Service) IsEnabled() bool {
	return s.client != nil && s.store != nil && s.authStore != nil && s.secretProtector != nil
}

func (s *Service) VerifyURL(ctx context.Context, signature, timestamp, nonce, echostr string) (string, error) {
	plain, _, err := s.decryptWithTenantCallbackApps(ctx, "", signature, timestamp, nonce, echostr)
	if err == nil {
		return plain, nil
	}
	return "", err
}

func (s *Service) HandleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	var envelope EncryptedCallbackEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parse callback xml: %w", err)
	}
	if strings.TrimSpace(envelope.Encrypt) == "" {
		return fmt.Errorf("missing encrypted payload")
	}
	plain, matchedApp, err := s.decryptWithTenantCallbackApps(ctx, envelope.ToUser, signature, timestamp, nonce, envelope.Encrypt)
	if err != nil {
		return err
	}
	slog.Info("wecom tenant callback matched", "corp_id", matchedApp.CorpID, "tenant_id", matchedApp.TenantID, "agent_id", matchedApp.AgentID)
	var event CallbackEvent
	if err := xml.Unmarshal([]byte(plain), &event); err != nil {
		return fmt.Errorf("parse decrypted callback: %w", err)
	}
	resolvedCorpID := strings.TrimSpace(event.AuthCorpID)
	if resolvedCorpID == "" {
		resolvedCorpID = matchedApp.CorpID
	}
	_ = s.store.SaveEventLog(ctx, resolvedCorpID, event.InfoType, plain)
	slog.Info("wecom callback received", "info_type", event.InfoType, "corp_id", resolvedCorpID)
	return nil
}

func decryptWithCrypto(crypto *Crypto, signature, timestamp, nonce, encrypted string) (string, bool) {
	if crypto == nil {
		return "", false
	}
	if !crypto.VerifySignature(signature, timestamp, nonce, encrypted) {
		return "", false
	}
	plain, err := crypto.Decrypt(encrypted)
	if err != nil {
		return "", false
	}
	return plain, true
}

func (s *Service) decryptWithTenantCallbackApps(ctx context.Context, corpID, signature, timestamp, nonce, encrypted string) (string, *TenantWeComAppRecord, error) {
	if s.store == nil {
		return "", nil, fmt.Errorf("wecom callback apps store is not configured")
	}

	if strings.TrimSpace(corpID) != "" {
		app, err := s.store.GetTenantAppByCorpID(ctx, strings.TrimSpace(corpID))
		if err == nil {
			if plain, ok := decryptWithTenantApp(app, signature, timestamp, nonce, encrypted); ok {
				return plain, app, nil
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return "", nil, err
		}
	}

	apps, err := s.store.ListEnabledTenantCallbackApps(ctx)
	if err != nil {
		return "", nil, err
	}
	for _, app := range apps {
		if strings.TrimSpace(corpID) != "" && strings.TrimSpace(app.CorpID) == strings.TrimSpace(corpID) {
			continue
		}
		if plain, ok := decryptWithTenantApp(app, signature, timestamp, nonce, encrypted); ok {
			return plain, app, nil
		}
	}
	return "", nil, fmt.Errorf("invalid wecom callback signature")
}

func decryptWithTenantApp(app *TenantWeComAppRecord, signature, timestamp, nonce, encrypted string) (string, bool) {
	if app == nil || !app.Enabled {
		return "", false
	}
	if strings.TrimSpace(app.Token) == "" || strings.TrimSpace(app.EncodingAESKey) == "" {
		return "", false
	}
	crypto, err := NewCrypto(app.Token, app.EncodingAESKey, app.CorpID)
	if err != nil {
		return "", false
	}
	return decryptWithCrypto(crypto, signature, timestamp, nonce, encrypted)
}

func (s *Service) LoginWithOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("code is required")
	}
	corpID = strings.TrimSpace(corpID)
	if corpID == "" {
		return nil, fmt.Errorf("corp_id is required")
	}
	slog.Info("wecom oauth login started", "corp_id", corpID, "code_prefix", truncateToken(code, 12))
	app, err := s.store.GetTenantAppByCorpID(ctx, corpID)
	if err != nil {
		slog.Warn("wecom oauth login failed: app not found", "corp_id", corpID, "error", err)
		return nil, fmt.Errorf("wecom app not found")
	}
	if !app.Enabled {
		return nil, fmt.Errorf("wecom app disabled")
	}
	if strings.TrimSpace(app.SecretCiphertext) == "" {
		return nil, fmt.Errorf("wecom app secret is not configured")
	}
	corpAccessToken, err := s.resolveTenantAppAccessToken(ctx, app)
	if err != nil {
		slog.Warn("wecom oauth login failed: access token", "corp_id", corpID, "error", err)
		return nil, err
	}
	userInfo, err := s.client.GetCorpUserInfo(ctx, corpAccessToken, code)
	if err != nil {
		if isWeComAccessTokenExpired(err) {
			corpAccessToken, err = s.forceResolveTenantAppAccessToken(ctx, app)
			if err == nil {
				userInfo, err = s.client.GetCorpUserInfo(ctx, corpAccessToken, code)
			}
		}
	}
	if err != nil {
		slog.Warn("wecom oauth login failed: get user info", "corp_id", corpID, "code_prefix", truncateToken(code, 12), "error", err)
		return nil, err
	}
	if strings.TrimSpace(userInfo.CorpID) != "" && strings.TrimSpace(userInfo.CorpID) != corpID {
		return nil, fmt.Errorf("wecom corp id mismatch")
	}
	if strings.TrimSpace(userInfo.UserID) == "" {
		return nil, fmt.Errorf("wecom returned empty user id")
	}
	userDetail, corpAccessToken, err := s.getOAuthUserDetail(ctx, app, corpAccessToken, userInfo.UserID, userInfo.UserTicket)
	if err != nil {
		slog.Warn("wecom oauth login failed: get user detail", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "error", err)
		return nil, err
	}
	profile := &OAuthUserProfile{
		CorpID:      corpID,
		WeComUserID: userInfo.UserID,
		OpenUserID:  userInfo.OpenUserID,
		Name:        userDetail.Name,
		Mobile:      userDetail.Mobile,
		Avatar:      userDetail.Avatar,
	}
	binding, err := s.store.GetUserBinding(ctx, ModeSelfBuilt, "", corpID, userInfo.UserID)
	if err != nil {
		slog.Warn("wecom oauth login failed: query binding", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "error", err)
		return nil, err
	}
	if binding != nil && binding.TenantID != app.TenantID {
		return nil, fmt.Errorf("tenant mismatch")
	}
	if binding == nil && strings.TrimSpace(userDetail.Mobile) != "" {
		employeeID, err := s.store.FindUniqueEmployeeIDByPhoneAndTenant(ctx, userDetail.Mobile, app.TenantID)
		if err != nil {
			slog.Warn("wecom oauth login failed: find employee by phone", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "mobile_suffix", maskPhone(userDetail.Mobile), "error", err)
			return nil, err
		}
		if employeeID != nil {
			employee, err := s.authStore.GetEmployeeByID(ctx, *employeeID)
			if err != nil {
				slog.Warn("wecom oauth login failed: load employee for auto bind", "employee_id", *employeeID, "error", err)
				return nil, err
			}
			if employee.TenantID != app.TenantID {
				return nil, fmt.Errorf("tenant mismatch")
			}
			if err := s.store.UpsertUserBinding(ctx, UserBindingRecord{
				Mode:        ModeSelfBuilt,
				ProviderApp: "",
				CorpID:      corpID,
				WeComUserID: userInfo.UserID,
				EmployeeID:  employee.ID,
				TenantID:    employee.TenantID,
				Source:      "auto_phone",
			}); err != nil {
				slog.Warn("wecom oauth login failed: upsert auto binding", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "employee_id", employee.ID, "error", err)
				return nil, err
			}
			s.runBindingUpsertHook(ctx, employee.TenantID, employee.ID)
			binding = &UserBindingRecord{
				Mode:        ModeSelfBuilt,
				ProviderApp: "",
				CorpID:      corpID,
				WeComUserID: userInfo.UserID,
				EmployeeID:  employee.ID,
				TenantID:    employee.TenantID,
				Source:      "auto_phone",
			}
		}
	}
	if binding != nil {
		authResp, err := s.issueMobileLogin(ctx, binding.EmployeeID)
		if err != nil {
			slog.Warn("wecom oauth login failed: issue mobile login", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "employee_id", binding.EmployeeID, "error", err)
			return nil, err
		}
		slog.Info("wecom oauth login succeeded", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "employee_id", binding.EmployeeID, "source", binding.Source)
		return &OAuthLoginResponse{Status: "logged_in", Auth: authResp, Profile: profile}, nil
	}
	slog.Info("wecom oauth login rejected: no employee phone match", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "mobile_suffix", maskPhone(userDetail.Mobile))
	return nil, fmt.Errorf("未找到与当前企业微信手机号匹配的灵策员工，请联系管理员检查企业微信通讯录手机号和灵策员工手机号")
}

func (s *Service) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	return s.bindEmployee(ctx, corpID, wecomUserID, employeeID, true)
}

func (s *Service) bindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64, enforcePhoneMatch bool) error {
	app, err := s.store.GetTenantAppByCorpID(ctx, strings.TrimSpace(corpID))
	if err != nil {
		return err
	}
	if !app.Enabled {
		return fmt.Errorf("wecom app disabled")
	}
	employee, err := s.authStore.GetEmployeeByID(ctx, employeeID)
	if err != nil {
		return err
	}
	if employee.TenantID != app.TenantID {
		return fmt.Errorf("tenant mismatch")
	}
	corpAccessToken, err := s.resolveTenantAppAccessToken(ctx, app)
	if err != nil {
		return err
	}
	userDetail, err := s.client.GetUserDetail(ctx, corpAccessToken, strings.TrimSpace(wecomUserID))
	if err != nil {
		if isWeComAccessTokenExpired(err) {
			corpAccessToken, err = s.forceResolveTenantAppAccessToken(ctx, app)
			if err == nil {
				userDetail, err = s.client.GetUserDetail(ctx, corpAccessToken, strings.TrimSpace(wecomUserID))
			}
		}
	}
	if err != nil {
		return err
	}
	if normalizePhone(userDetail.Mobile) == "" {
		return fmt.Errorf("当前企业微信账号未返回手机号，请联系管理员检查企业微信通讯录")
	}
	if enforcePhoneMatch && normalizePhone(employee.Phone) != normalizePhone(userDetail.Mobile) {
		return fmt.Errorf("当前登录手机号与企业微信通讯录手机号不一致，请使用本人账号或联系管理员")
	}
	existing, err := s.store.GetUserBinding(ctx, ModeSelfBuilt, "", corpID, wecomUserID)
	if err != nil {
		return err
	}
	if existing != nil && existing.EmployeeID != employee.ID {
		return fmt.Errorf("binding already exists")
	}
	if err := s.store.UpsertUserBinding(ctx, UserBindingRecord{
		Mode:        ModeSelfBuilt,
		ProviderApp: "",
		CorpID:      corpID,
		WeComUserID: wecomUserID,
		EmployeeID:  employee.ID,
		TenantID:    employee.TenantID,
		Source:      "manual_bind",
	}); err != nil {
		return err
	}
	s.runBindingUpsertHook(ctx, employee.TenantID, employee.ID)
	slog.Info("wecom manual binding created", "corp_id", corpID, "wecom_user_id", wecomUserID, "employee_id", employee.ID)
	return nil
}

func (s *Service) runBindingUpsertHook(ctx context.Context, tenantID, employeeID int64) {
	if s.onBindingUpsert == nil {
		return
	}
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
		return
	}
	if err := s.onBindingUpsert(ctx, tenantID, employeeID); err != nil {
		slog.Warn("wecom binding post hook failed", "tenant_id", tenantID, "employee_id", employeeID, "error", err)
	}
}

func normalizePhone(phone string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(phone) {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func defaultWeComHomeURL(corpID string, agentID int64) string {
	values := url.Values{}
	if strings.TrimSpace(corpID) != "" {
		values.Set("corp_id", strings.TrimSpace(corpID))
	}
	if agentID > 0 {
		values.Set("agent_id", fmt.Sprintf("%d", agentID))
	}
	values.Set("mode", "self_built")
	target := url.URL{
		Scheme:   "https",
		Host:     defaultWeComEmployeeHost,
		Path:     "/login",
		RawQuery: values.Encode(),
	}
	return target.String()
}

func generateRandomToken(length int) (string, error) {
	if length <= 0 {
		length = 32
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, length)
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(random[i])%len(alphabet)]
	}
	return string(buf), nil
}

func generateEncodingAESKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.TrimRight(base64.StdEncoding.EncodeToString(buf), "="), nil
}

func isAlphaNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func attachWeComEntryParams(targetURL string, corpID string, agentID int64) string {
	target := strings.TrimSpace(targetURL)
	if target == "" || strings.TrimSpace(corpID) == "" || agentID <= 0 {
		return target
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	query := parsed.Query()
	if query.Get("corp_id") == "" && query.Get("corpid") == "" {
		query.Set("corp_id", strings.TrimSpace(corpID))
	}
	if query.Get("agent_id") == "" && query.Get("agentid") == "" {
		query.Set("agent_id", fmt.Sprintf("%d", agentID))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (s *Service) SendInternalMessage(ctx context.Context, req InternalSendMessageRequest) (*InternalSendMessageResponse, error) {
	if strings.TrimSpace(req.MessageScene) == "" {
		return nil, fmt.Errorf("message_scene is required")
	}
	if strings.TrimSpace(req.DedupeKey) == "" {
		return nil, fmt.Errorf("dedupe_key is required")
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}
	if strings.TrimSpace(req.TargetURL) == "" {
		return nil, fmt.Errorf("target_url is required")
	}

	employeeIDs := uniqueInt64s(req.EmployeeIDs)
	resp := &InternalSendMessageResponse{Requested: len(employeeIDs)}
	if len(employeeIDs) == 0 {
		return resp, nil
	}

	appCache := make(map[string]*TenantWeComAppRecord)
	tokenCache := make(map[string]string)
	for _, employeeID := range employeeIDs {
		binding, err := s.store.GetActiveBindingByEmployeeID(ctx, employeeID)
		if err != nil {
			return nil, err
		}
		if binding == nil || strings.TrimSpace(binding.CorpID) == "" || strings.TrimSpace(binding.WeComUserID) == "" || binding.AgentID <= 0 {
			perRecipientKey := fmt.Sprintf("%s:%d", strings.TrimSpace(req.DedupeKey), employeeID)
			_, inserted, insertErr := s.store.InsertMessageLog(ctx, MessageLogRecord{
				EmployeeID:     employeeID,
				MessageScene:   req.MessageScene,
				DedupeKey:      perRecipientKey,
				Title:          req.Title,
				Content:        req.Content,
				TargetURL:      req.TargetURL,
				Status:         "skipped_unbound",
				RequestPayload: mustJSON(map[string]any{"message_scene": req.MessageScene, "employee_id": employeeID}),
				BizDate:        req.BizDate,
			})
			if insertErr != nil {
				return nil, insertErr
			}
			if inserted {
				resp.Skipped++
			}
			continue
		}

		targetURL := attachWeComEntryParams(req.TargetURL, binding.CorpID, binding.AgentID)
		app, ok := appCache[binding.CorpID]
		if !ok {
			app, err = s.store.GetTenantAppByCorpID(ctx, binding.CorpID)
			if err != nil {
				_, inserted, insertErr := s.store.InsertMessageLog(ctx, MessageLogRecord{
					CorpID:         binding.CorpID,
					TenantID:       binding.TenantID,
					EmployeeID:     employeeID,
					WeComUserID:    binding.WeComUserID,
					MessageScene:   req.MessageScene,
					DedupeKey:      fmt.Sprintf("%s:%d", strings.TrimSpace(req.DedupeKey), employeeID),
					Title:          req.Title,
					Content:        req.Content,
					TargetURL:      targetURL,
					Status:         "failed",
					ErrorMessage:   err.Error(),
					RequestPayload: mustJSON(map[string]any{"message_scene": req.MessageScene, "employee_id": employeeID}),
					BizDate:        req.BizDate,
				})
				if insertErr != nil {
					return nil, insertErr
				}
				if inserted {
					resp.Failed++
				}
				continue
			}
			appCache[binding.CorpID] = app
		}
		if !app.Enabled {
			_, inserted, insertErr := s.store.InsertMessageLog(ctx, MessageLogRecord{
				CorpID:         binding.CorpID,
				TenantID:       binding.TenantID,
				EmployeeID:     employeeID,
				WeComUserID:    binding.WeComUserID,
				MessageScene:   req.MessageScene,
				DedupeKey:      fmt.Sprintf("%s:%d", strings.TrimSpace(req.DedupeKey), employeeID),
				Title:          req.Title,
				Content:        req.Content,
				TargetURL:      targetURL,
				Status:         "skipped_unbound",
				RequestPayload: mustJSON(map[string]any{"message_scene": req.MessageScene, "employee_id": employeeID}),
				BizDate:        req.BizDate,
			})
			if insertErr != nil {
				return nil, insertErr
			}
			if inserted {
				resp.Skipped++
			}
			continue
		}

		requestPayload := mustJSON(map[string]any{
			"message_scene": req.MessageScene,
			"employee_id":   employeeID,
			"corp_id":       binding.CorpID,
			"wecom_user_id": binding.WeComUserID,
			"title":         req.Title,
			"content":       req.Content,
			"target_url":    targetURL,
			"button_text":   req.ButtonText,
			"extra":         req.Extra,
		})
		logID, inserted, err := s.store.InsertMessageLog(ctx, MessageLogRecord{
			CorpID:         binding.CorpID,
			TenantID:       binding.TenantID,
			EmployeeID:     employeeID,
			WeComUserID:    binding.WeComUserID,
			MessageScene:   req.MessageScene,
			DedupeKey:      fmt.Sprintf("%s:%d", strings.TrimSpace(req.DedupeKey), employeeID),
			Title:          req.Title,
			Content:        req.Content,
			TargetURL:      targetURL,
			Status:         "pending",
			RequestPayload: requestPayload,
			BizDate:        req.BizDate,
		})
		if err != nil {
			return nil, err
		}
		if !inserted {
			resp.Skipped++
			continue
		}

		corpToken, ok := tokenCache[binding.CorpID]
		if !ok {
			corpToken, err = s.resolveTenantAppAccessToken(ctx, app)
			if err != nil {
				_ = s.store.UpdateMessageLogStatus(ctx, logID, "failed", err.Error(), "")
				resp.Failed++
				continue
			}
			tokenCache[binding.CorpID] = corpToken
		}

		sendResp, err := s.client.SendTextCardMessage(ctx, corpToken, binding.AgentID, binding.WeComUserID, req.Title, req.Content, targetURL, req.ButtonText)
		if err != nil {
			if isWeComAccessTokenExpired(err) {
				corpToken, err = s.forceResolveTenantAppAccessToken(ctx, app)
				if err == nil {
					tokenCache[binding.CorpID] = corpToken
					sendResp, err = s.client.SendTextCardMessage(ctx, corpToken, binding.AgentID, binding.WeComUserID, req.Title, req.Content, targetURL, req.ButtonText)
				}
			}
		}
		if err != nil {
			_ = s.store.UpdateMessageLogStatus(ctx, logID, "failed", err.Error(), "")
			resp.Failed++
			continue
		}
		_ = s.store.UpdateMessageLogStatus(ctx, logID, "sent", "", mustJSON(sendResp))
		resp.Sent++
	}

	return resp, nil
}

func (s *Service) ListTenantApps(ctx context.Context, tenantID *int64) ([]*TenantWeComAppResponse, error) {
	items, err := s.store.ListTenantApps(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*TenantWeComAppResponse, 0, len(items))
	for _, item := range items {
		out = append(out, s.toTenantAppResponse(ctx, item))
	}
	return out, nil
}

func (s *Service) ListCorpInstalls(ctx context.Context, tenantID *int64, corpID string) ([]*CorpInstallResponse, error) {
	items, err := s.store.ListCorpInstalls(ctx, tenantID, corpID)
	if err != nil {
		return nil, err
	}
	resp := make([]*CorpInstallResponse, 0, len(items))
	for _, item := range items {
		row := &CorpInstallResponse{
			TenantID:     item.TenantID,
			CorpID:       item.CorpID,
			CorpName:     item.CorpName,
			AgentID:      item.AgentID,
			Status:       item.Status,
			HasPermanent: strings.TrimSpace(item.PermanentCode) != "",
		}
		if item.UpdatedAt != nil {
			value := formatTime(*item.UpdatedAt)
			row.UpdatedAt = &value
		}
		if item.CancelledAt != nil {
			value := formatTime(*item.CancelledAt)
			row.CancelledAt = &value
		}
		resp = append(resp, row)
	}
	return resp, nil
}

func (s *Service) ListBindingStatuses(ctx context.Context, tenantID *int64, keyword, status string, page, pageSize int) ([]*BindingStatusResponse, int, error) {
	items, total, err := s.store.ListBindingStatuses(ctx, tenantID, keyword, status, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	resp := make([]*BindingStatusResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toBindingStatusResponse(item))
	}
	return resp, total, nil
}

func (s *Service) ListDirectoryMembers(ctx context.Context, params DirectoryMemberListParams) ([]*DirectoryMemberResponse, int, error) {
	items, total, err := s.store.ListDirectoryMembers(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	resp := make([]*DirectoryMemberResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toDirectoryMemberResponse(item))
	}
	return resp, total, nil
}

func (s *Service) SyncDirectoryAndPrebind(ctx context.Context, tenantID int64) (*DirectorySyncResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	app, err := s.store.GetTenantAppByTenantID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if app == nil || !app.Enabled {
		return nil, fmt.Errorf("tenant wecom app is not enabled")
	}
	corpAccessToken, err := s.resolveTenantAppAccessToken(ctx, app)
	if err != nil {
		return nil, err
	}
	users, err := s.client.ListDepartmentUsers(ctx, corpAccessToken, 1, true)
	if err != nil {
		if isWeComAccessTokenExpired(err) {
			corpAccessToken, err = s.forceResolveTenantAppAccessToken(ctx, app)
			if err == nil {
				users, err = s.client.ListDepartmentUsers(ctx, corpAccessToken, 1, true)
			}
		}
	}
	if err != nil {
		return nil, err
	}

	summary := &DirectorySyncResponse{TenantID: tenantID, CorpID: app.CorpID}
	for _, user := range users {
		if strings.TrimSpace(user.UserID) == "" {
			continue
		}
		summary.SyncedCount++
		normalizedMobile := normalizePhone(user.Mobile)
		matchStatus := "unmatched"
		var matchedEmployeeID *int64
		if user.Status != 1 {
			matchStatus = "inactive"
		} else if normalizedMobile != "" {
			matches, matchErr := s.store.FindActiveEmployeesByNormalizedPhoneAndTenant(ctx, normalizedMobile, tenantID)
			if matchErr != nil {
				return nil, matchErr
			}
			if len(matches) == 1 {
				id := matches[0].EmployeeID
				matchedEmployeeID = &id
				binding, bindErr := s.store.GetUserBinding(ctx, ModeSelfBuilt, "", app.CorpID, strings.TrimSpace(user.UserID))
				if bindErr != nil {
					return nil, bindErr
				}
				if binding == nil || binding.EmployeeID == id {
					if upsertErr := s.store.UpsertUserBinding(ctx, UserBindingRecord{
						Mode:        ModeSelfBuilt,
						ProviderApp: "",
						CorpID:      app.CorpID,
						WeComUserID: strings.TrimSpace(user.UserID),
						EmployeeID:  id,
						TenantID:    tenantID,
						Source:      "directory_sync",
					}); upsertErr != nil {
						return nil, upsertErr
					}
					s.runBindingUpsertHook(ctx, tenantID, id)
					matchStatus = "prebound"
					summary.PreboundCount++
				} else {
					matchStatus = "conflict"
					summary.ConflictCount++
				}
			} else if len(matches) > 1 {
				matchStatus = "conflict"
				summary.ConflictCount++
			} else {
				summary.UnmatchedCount++
			}
			if matchStatus == "unmatched" && matchedEmployeeID != nil {
				matchStatus = "matched"
			}
		} else {
			matchStatus = "no_mobile"
			summary.UnmatchedCount++
		}
		if matchStatus == "matched" {
			summary.MatchedCount++
		}
		if matchStatus == "inactive" {
			summary.UnmatchedCount++
		}
		if err := s.store.UpsertDirectoryMember(ctx, DirectoryMemberRecord{
			TenantID:          tenantID,
			CorpID:            app.CorpID,
			WeComUserID:       strings.TrimSpace(user.UserID),
			Name:              strings.TrimSpace(user.Name),
			Mobile:            normalizedMobile,
			DepartmentIDsJSON: marshalDepartmentIDs(user.Department),
			WeComStatus:       user.Status,
			MatchStatus:       matchStatus,
			MatchedEmployeeID: matchedEmployeeID,
		}); err != nil {
			return nil, err
		}
	}
	return summary, nil
}

func (s *Service) AdminBindEmployee(ctx context.Context, req AdminBindingRequest) error {
	if err := tenancy.RequirePositiveID("tenant_id", req.TenantID); err != nil {
		return err
	}
	if err := tenancy.RequirePositiveID("employee_id", req.EmployeeID); err != nil {
		return err
	}
	wecomUserID := strings.TrimSpace(req.WeComUserID)
	if wecomUserID == "" {
		return fmt.Errorf("wecom_user_id is required")
	}
	corpID := strings.TrimSpace(req.CorpID)
	if corpID == "" {
		app, err := s.store.GetTenantAppByTenantID(ctx, req.TenantID)
		if err != nil {
			return err
		}
		corpID = strings.TrimSpace(app.CorpID)
	}
	if corpID == "" {
		return fmt.Errorf("corp_id is required")
	}
	return s.bindEmployee(ctx, corpID, wecomUserID, req.EmployeeID, false)
}

func (s *Service) DeleteBinding(ctx context.Context, id int64) error {
	item, err := s.store.GetBindingByID(ctx, id)
	if err != nil {
		return err
	}
	if item == nil {
		return fmt.Errorf("binding not found")
	}
	return s.store.DeleteBinding(ctx, id)
}

func (s *Service) GetTenantApp(ctx context.Context, id int64) (*TenantWeComAppResponse, error) {
	item, err := s.store.GetTenantAppByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.toTenantAppResponse(ctx, item), nil
}

func (s *Service) UpsertTenantApp(ctx context.Context, req TenantWeComAppRequest) (*TenantWeComAppResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", req.TenantID); err != nil {
		return nil, err
	}
	corpID := strings.TrimSpace(req.CorpID)
	if corpID == "" {
		return nil, fmt.Errorf("corp_id is required")
	}
	if len(corpID) < minWeComCorpIDLength || len(corpID) > maxWeComCorpIDLength || !isAlphaNumeric(corpID) {
		return nil, fmt.Errorf("corp_id format is invalid")
	}
	if err := tenancy.RequirePositiveID("agent_id", req.AgentID); err != nil {
		return nil, err
	}
	current, err := s.store.GetTenantAppByTenantID(ctx, req.TenantID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	existingByCorp, err := s.store.GetTenantAppByCorpID(ctx, corpID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if existingByCorp != nil && (current == nil || existingByCorp.ID != current.ID) {
		return nil, fmt.Errorf("企业ID %s 已被其他企业微信配置占用，请确认是否重复录入或先删除原配置后再保存", corpID)
	}
	secretCiphertext := ""
	if strings.TrimSpace(req.Secret) != "" {
		secret := strings.TrimSpace(req.Secret)
		if len(secret) < minWeComSecretLength || len(secret) > maxWeComSecretLength {
			return nil, fmt.Errorf("secret length is invalid")
		}
		secretCiphertext, err = s.secretProtector.Encrypt(secret)
		if err != nil {
			return nil, err
		}
	} else if current != nil {
		secretCiphertext = current.SecretCiphertext
	}
	if strings.TrimSpace(secretCiphertext) == "" {
		return nil, fmt.Errorf("secret is required")
	}
	token := strings.TrimSpace(req.Token)
	if token == "" && current != nil {
		token = current.Token
	}
	if token == "" {
		token, err = generateRandomToken(32)
		if err != nil {
			return nil, err
		}
	}
	encodingAESKey := strings.TrimSpace(req.EncodingAESKey)
	if encodingAESKey == "" && current != nil {
		encodingAESKey = current.EncodingAESKey
	}
	if encodingAESKey == "" {
		encodingAESKey, err = generateEncodingAESKey()
		if err != nil {
			return nil, err
		}
	}
	homeURL := strings.TrimSpace(req.HomeURL)
	if homeURL == "" {
		homeURL = defaultWeComHomeURL(req.CorpID, req.AgentID)
	}
	trustedDomain := strings.TrimSpace(req.TrustedDomain)
	if trustedDomain == "" {
		trustedDomain = defaultWeComEmployeeHost
	}
	jsapiDomain := strings.TrimSpace(req.JSAPIDomain)
	if jsapiDomain == "" {
		jsapiDomain = defaultWeComEmployeeHost
	}
	item, err := s.store.UpsertTenantApp(ctx, TenantWeComAppRecord{
		TenantID:         req.TenantID,
		CorpID:           corpID,
		CorpName:         strings.TrimSpace(req.CorpName),
		AgentID:          req.AgentID,
		SecretCiphertext: secretCiphertext,
		Token:            token,
		EncodingAESKey:   encodingAESKey,
		HomeURL:          homeURL,
		TrustedDomain:    trustedDomain,
		JSAPIDomain:      jsapiDomain,
		Enabled:          req.Enabled,
		ConfigConfirmed:  req.ConfigConfirmed,
	})
	if err != nil {
		return nil, err
	}
	return s.toTenantAppResponse(ctx, item), nil
}

func (s *Service) DeleteTenantApp(ctx context.Context, id int64) error {
	return s.store.DeleteTenantApp(ctx, id)
}

func (s *Service) TestTenantAppToken(ctx context.Context, id int64) (*TenantWeComAppResponse, error) {
	item, err := s.store.GetTenantAppByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("tenant wecom app not found")
	}
	_, err = s.resolveTenantAppAccessToken(ctx, item)
	if err != nil {
		return nil, err
	}
	updated, err := s.store.GetTenantAppByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.toTenantAppResponse(ctx, updated), nil
}

func (s *Service) resolveTenantAppAccessToken(ctx context.Context, app *TenantWeComAppRecord) (string, error) {
	return s.resolveTenantAppAccessTokenWithForce(ctx, app, false)
}

func (s *Service) forceResolveTenantAppAccessToken(ctx context.Context, app *TenantWeComAppRecord) (string, error) {
	return s.resolveTenantAppAccessTokenWithForce(ctx, app, true)
}

func (s *Service) getOAuthUserDetail(ctx context.Context, app *TenantWeComAppRecord, corpAccessToken, userID, userTicket string) (*userDetailResponse, string, error) {
	userID = strings.TrimSpace(userID)
	userTicket = strings.TrimSpace(userTicket)
	if userTicket != "" {
		detail, token, err := s.getAuthUserDetailWithRetry(ctx, app, corpAccessToken, userTicket)
		if err == nil && normalizePhone(detail.Mobile) != "" {
			if strings.TrimSpace(detail.UserID) == "" {
				detail.UserID = userID
			}
			return detail, token, nil
		}
		if err != nil {
			slog.Warn("wecom oauth private user detail unavailable, fallback to directory detail", "corp_id", app.CorpID, "wecom_user_id", userID, "error", err)
		}
	}

	detail, token, err := s.getDirectoryUserDetailWithRetry(ctx, app, corpAccessToken, userID)
	if err != nil {
		return nil, token, err
	}
	return detail, token, nil
}

func (s *Service) getAuthUserDetailWithRetry(ctx context.Context, app *TenantWeComAppRecord, corpAccessToken, userTicket string) (*userDetailResponse, string, error) {
	detail, err := s.client.GetAuthUserDetail(ctx, corpAccessToken, userTicket)
	if err != nil {
		if isWeComAccessTokenExpired(err) {
			refreshedToken, refreshErr := s.forceResolveTenantAppAccessToken(ctx, app)
			if refreshErr != nil {
				return nil, corpAccessToken, refreshErr
			}
			corpAccessToken = refreshedToken
			detail, err = s.client.GetAuthUserDetail(ctx, corpAccessToken, userTicket)
		}
	}
	return detail, corpAccessToken, err
}

func (s *Service) getDirectoryUserDetailWithRetry(ctx context.Context, app *TenantWeComAppRecord, corpAccessToken, userID string) (*userDetailResponse, string, error) {
	detail, err := s.client.GetUserDetail(ctx, corpAccessToken, userID)
	if err != nil {
		if isWeComAccessTokenExpired(err) {
			refreshedToken, refreshErr := s.forceResolveTenantAppAccessToken(ctx, app)
			if refreshErr != nil {
				return nil, corpAccessToken, refreshErr
			}
			corpAccessToken = refreshedToken
			detail, err = s.client.GetUserDetail(ctx, corpAccessToken, userID)
		}
	}
	return detail, corpAccessToken, err
}

func (s *Service) resolveTenantAppAccessTokenWithForce(ctx context.Context, app *TenantWeComAppRecord, force bool) (string, error) {
	if app == nil {
		return "", fmt.Errorf("tenant wecom app not found")
	}
	if !force && strings.TrimSpace(app.AccessToken) != "" && app.AccessTokenExpiredAt != nil && time.Until(*app.AccessTokenExpiredAt) > 5*time.Minute {
		return app.AccessToken, nil
	}
	secret, err := s.secretProtector.Decrypt(app.SecretCiphertext)
	if err != nil {
		return "", err
	}
	token, expiresIn, err := s.client.GetCorpAccessToken(ctx, app.CorpID, secret)
	if err != nil {
		return "", err
	}
	expiredAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	if err := s.store.UpdateTenantAppAccessToken(ctx, app.ID, token, expiredAt); err != nil {
		return "", err
	}
	app.AccessToken = token
	app.AccessTokenExpiredAt = &expiredAt
	return token, nil
}

func isWeComAccessTokenExpired(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == 42001 || apiErr.Code == 40014
}

func (s *Service) toTenantAppResponse(ctx context.Context, item *TenantWeComAppRecord) *TenantWeComAppResponse {
	if item == nil {
		return nil
	}
	tenantName := ""
	if item.TenantID > 0 && s.authStore != nil {
		if tenant, err := s.authStore.GetTenantByID(ctx, item.TenantID); err == nil && tenant != nil {
			tenantName = tenant.Name
		}
	}
	resp := &TenantWeComAppResponse{
		ID:                item.ID,
		TenantID:          item.TenantID,
		TenantName:        tenantName,
		CorpID:            item.CorpID,
		CorpName:          item.CorpName,
		AgentID:           item.AgentID,
		SecretMasked:      maskString(item.SecretCiphertext),
		HasSecret:         strings.TrimSpace(item.SecretCiphertext) != "",
		HasToken:          strings.TrimSpace(item.Token) != "",
		HasEncodingAESKey: strings.TrimSpace(item.EncodingAESKey) != "",
		Token:             item.Token,
		EncodingAESKey:    item.EncodingAESKey,
		HomeURL:           item.HomeURL,
		TrustedDomain:     item.TrustedDomain,
		JSAPIDomain:       item.JSAPIDomain,
		Enabled:           item.Enabled,
		ConfigConfirmed:   item.ConfigConfirmed,
		CreatedAt:         formatTime(item.CreatedAt),
		UpdatedAt:         formatTime(item.UpdatedAt),
	}
	if item.AccessTokenExpiredAt != nil {
		v := formatTime(*item.AccessTokenExpiredAt)
		resp.AccessTokenExpiredAt = &v
	}
	if item.LastSyncAt != nil {
		v := formatTime(*item.LastSyncAt)
		resp.LastSyncAt = &v
	}
	return resp
}

func toBindingStatusResponse(item *BindingStatusRecord) *BindingStatusResponse {
	if item == nil {
		return nil
	}
	resp := &BindingStatusResponse{
		BindingID:     item.BindingID,
		TenantID:      item.TenantID,
		TenantName:    item.TenantName,
		EmployeeID:    item.EmployeeID,
		EmployeeName:  item.EmployeeName,
		EmployeePhone: item.EmployeePhone,
		CorpID:        item.CorpID,
		CorpName:      item.CorpName,
		WeComUserID:   item.WeComUserID,
		Source:        item.Source,
		AppEnabled:    item.AppEnabled,
		IsBound:       item.IsBound,
	}
	if item.BoundAt != nil {
		v := formatTime(*item.BoundAt)
		resp.BoundAt = &v
	}
	if item.BindingUpdated != nil {
		v := formatTime(*item.BindingUpdated)
		resp.BindingUpdated = &v
	}
	return resp
}

func toDirectoryMemberResponse(item *DirectoryMemberListRecord) *DirectoryMemberResponse {
	if item == nil {
		return nil
	}
	resp := &DirectoryMemberResponse{
		ID:                  item.ID,
		TenantID:            item.TenantID,
		TenantName:          item.TenantName,
		CorpID:              item.CorpID,
		CorpName:            item.CorpName,
		WeComUserID:         item.WeComUserID,
		Name:                item.Name,
		Mobile:              item.Mobile,
		MatchStatus:         item.MatchStatus,
		MatchedEmployeeID:   item.MatchedEmployeeID,
		MatchedEmployeeName: item.MatchedEmployeeName,
		BindingEmployeeID:   item.BindingEmployeeID,
		BindingSource:       item.BindingSource,
	}
	v := formatTime(item.LastSyncedAt)
	resp.LastSyncedAt = &v
	return resp
}

func (s *Service) issueMobileLogin(ctx context.Context, employeeID int64) (*internalauth.LoginResponse, error) {
	employee, err := s.authStore.GetEmployeeByID(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	if !employee.IsActive {
		return nil, fmt.Errorf("account is inactive")
	}
	tenant, err := s.authStore.GetTenantByID(ctx, employee.TenantID)
	if err != nil {
		return nil, err
	}
	if !tenant.IsActive {
		return nil, fmt.Errorf("tenant is inactive")
	}
	now := time.Now()
	if tenant.ValidTo != nil && tenant.ValidTo.Before(now) {
		return nil, fmt.Errorf("tenant subscription expired")
	}
	token, expiresAt, err := jwtauth.GenerateToken(s.jwtSecret, employee.ID, jwtauth.UserTypeMobile, &employee.TenantID, employee.SessionVersion, s.jwtExpiryHours)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}
	return &internalauth.LoginResponse{
		Token:       token,
		AccessToken: token,
		TokenType:   "bearer",
		UserType:    string(jwtauth.UserTypeMobile),
		UserID:      employee.ID,
		Username:    employee.Username,
		TenantID:    &employee.TenantID,
		ExpiresAt:   expiresAt,
		User: &internalauth.LoginUser{
			ID:         employee.ID,
			Name:       firstNonEmpty(employee.FullName, employee.Name, employee.Username, employee.Phone),
			Phone:      employee.Phone,
			Role:       string(jwtauth.UserTypeMobile),
			TenantID:   &employee.TenantID,
			TenantName: &tenant.Name,
		},
		UserInfo: map[string]any{
			"name":      firstNonEmpty(employee.FullName, employee.Name, employee.Username, employee.Phone),
			"full_name": employee.FullName,
			"phone":     employee.Phone,
		},
	}, nil
}

func truncateToken(value string, keep int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if keep <= 0 || len(value) <= keep {
		return value
	}
	return value[:keep]
}

func maskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if len(phone) < 7 {
		return phone
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func uniqueInt64s(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	out := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func mustJSON(value any) string {
	if value == nil {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

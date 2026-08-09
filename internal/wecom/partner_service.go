package wecom

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	internalauth "github.com/freeasyman/lingce-api/internal/auth"
	jwtauth "github.com/freeasyman/lingce-api/pkg/auth"
)

type PartnerService struct {
	store              *Store
	authStore          *internalauth.Store
	client             *Client
	crypto             *Crypto
	suiteID            string
	jwtSecret          string
	jwtExpiryHours     int
	callbackBaseURL    string
	installRedirectURL string
	installAuthType    int
}

func NewPartnerService(store *Store, authStore *internalauth.Store, client *Client, jwtSecret string, jwtExpiryHours int, suiteID, token, encodingAESKey, callbackBaseURL, installRedirectURL string, installAuthType int) *PartnerService {
	suiteID = strings.TrimSpace(suiteID)
	token = strings.TrimSpace(token)
	encodingAESKey = strings.TrimSpace(encodingAESKey)
	var crypto *Crypto
	if suiteID != "" && token != "" && encodingAESKey != "" {
		if c, err := NewCrypto(token, encodingAESKey, suiteID); err == nil {
			crypto = c
		}
	}
	if installAuthType <= 0 {
		installAuthType = 1
	}
	return &PartnerService{
		store:              store,
		authStore:          authStore,
		client:             client,
		crypto:             crypto,
		suiteID:            suiteID,
		jwtSecret:          jwtSecret,
		jwtExpiryHours:     jwtExpiryHours,
		callbackBaseURL:    strings.TrimRight(strings.TrimSpace(callbackBaseURL), "/"),
		installRedirectURL: strings.TrimSpace(installRedirectURL),
		installAuthType:    installAuthType,
	}
}

func (s *PartnerService) IsEnabled() bool {
	return s.client != nil && s.store != nil && s.authStore != nil && s.crypto != nil && strings.TrimSpace(s.suiteID) != ""
}

func (s *PartnerService) VerifyURL(signature, timestamp, nonce, echostr string) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("wecom partner mode is not configured")
	}
	if !s.crypto.VerifySignature(signature, timestamp, nonce, echostr) {
		return "", fmt.Errorf("invalid signature")
	}
	return s.crypto.Decrypt(echostr)
}

func (s *PartnerService) HandleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	if !s.IsEnabled() {
		return fmt.Errorf("wecom partner mode is not configured")
	}
	var envelope EncryptedCallbackEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parse callback xml: %w", err)
	}
	if strings.TrimSpace(envelope.Encrypt) == "" {
		return fmt.Errorf("missing encrypted payload")
	}
	if !s.crypto.VerifySignature(signature, timestamp, nonce, envelope.Encrypt) {
		return fmt.Errorf("invalid signature")
	}
	plain, err := s.crypto.Decrypt(envelope.Encrypt)
	if err != nil {
		return err
	}
	var event CallbackEvent
	if err := xml.Unmarshal([]byte(plain), &event); err != nil {
		return fmt.Errorf("parse decrypted callback: %w", err)
	}
	if event.SuiteID != "" && event.SuiteID != s.suiteID {
		return fmt.Errorf("suite id mismatch")
	}
	_ = s.store.SaveEventLog(ctx, event.AuthCorpID, event.InfoType, plain)
	switch event.InfoType {
	case "suite_ticket":
		return s.store.SaveSuiteTicket(ctx, SuiteTicketRecord{SuiteID: s.suiteID, SuiteTicket: event.SuiteTicket})
	case "create_auth", "change_auth":
		if _, err := s.syncCorpInstall(ctx, event.AuthCorpID, event.AuthCode); err != nil {
			return err
		}
		return nil
	case "cancel_auth":
		return s.store.MarkCorpInstallCancelled(ctx, event.AuthCorpID)
	default:
		return nil
	}
}

func (s *PartnerService) BuildInstallURL(ctx context.Context, state string, authType int) (*InstallURLResponse, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom partner mode is not configured")
	}
	redirectURI, err := s.getInstallRedirectURI()
	if err != nil {
		return nil, err
	}
	if authType < 0 {
		authType = s.installAuthType
	}
	if authType <= 0 {
		authType = 1
	}
	state = sanitizeState(state)
	if state == "" {
		state = fmt.Sprintf("lingce%d", time.Now().Unix())
	}
	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.suiteID)
	if err != nil {
		return nil, err
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		return nil, err
	}
	preAuthCode, _, err := s.client.GetPreAuthCode(ctx, suiteAccessToken)
	if err != nil {
		return nil, err
	}
	if err := s.client.SetSessionInfo(ctx, suiteAccessToken, preAuthCode, authType); err != nil {
		return nil, err
	}
	installURL := "https://open.work.weixin.qq.com/3rdapp/install?suite_id=" + url.QueryEscape(s.suiteID) +
		"&pre_auth_code=" + url.QueryEscape(preAuthCode) +
		"&redirect_uri=" + url.QueryEscape(redirectURI) +
		"&state=" + url.QueryEscape(state)
	return &InstallURLResponse{
		InstallURL:  installURL,
		RedirectURI: redirectURI,
		State:       state,
		AuthType:    authType,
	}, nil
}

func (s *PartnerService) HandleInstallCallback(ctx context.Context, authCode string) (*InstallCallbackResult, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom partner mode is not configured")
	}
	install, err := s.syncCorpInstall(ctx, "", authCode)
	if err != nil {
		return nil, err
	}
	return &InstallCallbackResult{CorpID: install.CorpID, CorpName: install.CorpName}, nil
}

func (s *PartnerService) LoginWithOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom partner mode is not configured")
	}
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("code is required")
	}
	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.suiteID)
	if err != nil {
		return nil, err
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		return nil, err
	}
	userInfo, err := s.client.GetUserInfo3rd(ctx, suiteAccessToken, code)
	if err != nil {
		return nil, err
	}
	if corpID == "" {
		corpID = userInfo.CorpID
	}
	corpID = strings.TrimSpace(corpID)
	if corpID == "" {
		return nil, fmt.Errorf("corp_id is required")
	}
	if strings.TrimSpace(userInfo.UserID) == "" {
		return nil, fmt.Errorf("wecom returned empty user id")
	}
	install, err := s.store.GetCorpInstallByCorpID(ctx, corpID)
	if err != nil {
		return nil, err
	}
	corpAccessToken, _, err := s.client.GetCorpToken(ctx, suiteAccessToken, corpID, install.PermanentCode)
	if err != nil {
		return nil, err
	}
	userDetail, err := s.client.GetUserDetail(ctx, corpAccessToken, userInfo.UserID)
	if err != nil && strings.TrimSpace(userInfo.UserTicket) != "" {
		userDetail, err = s.client.GetAuthUserDetail(ctx, corpAccessToken, userInfo.UserTicket)
	}
	if err != nil {
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
	binding, err := s.store.GetUserBinding(ctx, corpID, userInfo.UserID)
	if err != nil {
		return nil, err
	}
	if binding != nil {
		authResp, err := s.issueMobileLogin(ctx, binding.EmployeeID)
		if err != nil {
			return nil, err
		}
		return &OAuthLoginResponse{Status: "logged_in", Auth: authResp, Profile: profile}, nil
	}
	if mobile := normalizePhone(userDetail.Mobile); mobile != "" {
		employeeID, err := s.store.FindUniqueEmployeeIDByPhone(ctx, mobile)
		if err != nil {
			return nil, err
		}
		if employeeID != nil {
			employee, err := s.authStore.GetEmployeeByID(ctx, *employeeID)
			if err != nil {
				return nil, err
			}
			if err := s.store.UpsertUserBinding(ctx, UserBindingRecord{
				CorpID:      corpID,
				WeComUserID: userInfo.UserID,
				EmployeeID:  employee.ID,
				TenantID:    employee.TenantID,
				Source:      "auto_phone",
			}); err != nil {
				return nil, err
			}
			authResp, err := s.issueMobileLogin(ctx, employee.ID)
			if err != nil {
				return nil, err
			}
			return &OAuthLoginResponse{Status: "logged_in", AutoBound: true, Auth: authResp, Profile: profile}, nil
		}
	}
	return &OAuthLoginResponse{Status: "needs_bind", Profile: profile}, nil
}

func (s *PartnerService) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	employee, err := s.authStore.GetEmployeeByID(ctx, employeeID)
	if err != nil {
		return err
	}
	return s.store.UpsertUserBinding(ctx, UserBindingRecord{
		CorpID:      strings.TrimSpace(corpID),
		WeComUserID: strings.TrimSpace(wecomUserID),
		EmployeeID:  employee.ID,
		TenantID:    employee.TenantID,
		Source:      "manual_bind",
	})
}

func (s *PartnerService) syncCorpInstall(ctx context.Context, corpID, authCode string) (*CorpInstallRecord, error) {
	authCode = strings.TrimSpace(authCode)
	if authCode == "" {
		return nil, fmt.Errorf("missing auth_code")
	}
	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.suiteID)
	if err != nil {
		return nil, err
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		return nil, err
	}
	permanentResp, err := s.client.GetPermanentCode(ctx, suiteAccessToken, authCode)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(corpID) == "" {
		corpID = strings.TrimSpace(permanentResp.AuthCorpInfo.CorpID)
	}
	if strings.TrimSpace(corpID) == "" {
		return nil, fmt.Errorf("missing corp install context")
	}
	infoResp, err := s.client.GetAuthInfo(ctx, suiteAccessToken, corpID, permanentResp.PermanentCode)
	if err != nil {
		return nil, err
	}
	var agentID int64
	if len(infoResp.AuthInfo.Agent) > 0 {
		agentID = infoResp.AuthInfo.Agent[0].AgentID
	}
	record := CorpInstallRecord{
		CorpID:        corpID,
		CorpName:      strings.TrimSpace(infoResp.AuthCorpInfo.CorpName),
		PermanentCode: permanentResp.PermanentCode,
		AgentID:       agentID,
		Status:        "active",
	}
	if err := s.store.UpsertCorpInstall(ctx, record); err != nil {
		return nil, err
	}
	slog.Info("wecom partner corp install synced", "corp_id", record.CorpID, "corp_name", record.CorpName, "agent_id", record.AgentID)
	return &record, nil
}

func (s *PartnerService) issueMobileLogin(ctx context.Context, employeeID int64) (*internalauth.LoginResponse, error) {
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

func (s *PartnerService) SendInternalMessage(ctx context.Context, req InternalSendMessageRequest) (*InternalSendMessageResponse, error) {
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

	appCache := make(map[string]*CorpInstallRecord)
	tokenCache := make(map[string]string)
	for _, employeeID := range employeeIDs {
		binding, err := s.store.GetActivePartnerBindingByEmployeeID(ctx, employeeID)
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
		install, ok := appCache[binding.CorpID]
		if !ok {
			install, err = s.store.GetCorpInstallByCorpID(ctx, binding.CorpID)
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
			appCache[binding.CorpID] = install
		}
		if strings.TrimSpace(install.PermanentCode) == "" {
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
				ErrorMessage:   "corp install is not configured",
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

		suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.suiteID)
		if err != nil {
			_ = s.store.UpdateMessageLogStatus(ctx, logID, "failed", err.Error(), "")
			resp.Failed++
			continue
		}
		suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
		if err != nil {
			_ = s.store.UpdateMessageLogStatus(ctx, logID, "failed", err.Error(), "")
			resp.Failed++
			continue
		}

		corpToken, ok := tokenCache[binding.CorpID]
		if !ok {
			corpToken, _, err = s.client.GetCorpToken(ctx, suiteAccessToken, binding.CorpID, install.PermanentCode)
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
				corpToken, _, err = s.client.GetCorpToken(ctx, suiteAccessToken, binding.CorpID, install.PermanentCode)
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

func (s *PartnerService) getInstallRedirectURI() (string, error) {
	if s.installRedirectURL != "" {
		return s.installRedirectURL, nil
	}
	if s.callbackBaseURL == "" {
		return "", fmt.Errorf("wecom install redirect url is not configured")
	}
	return s.callbackBaseURL + "/api/v1/wecom/partner/install/callback", nil
}

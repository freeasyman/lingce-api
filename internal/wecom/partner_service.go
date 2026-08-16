package wecom

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
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
	mode               string
	appID              string
	appIDLabel         string
	routePrefix        string
	installURLBase     string
	jwtSecret          string
	jwtExpiryHours     int
	callbackBaseURL    string
	installRedirectURL string
	installAuthType    int
}

func NewPartnerService(store *Store, authStore *internalauth.Store, client *Client, jwtSecret string, jwtExpiryHours int, mode, appID, appIDLabel, routePrefix, token, encodingAESKey, callbackBaseURL, installRedirectURL string, installAuthType int) *PartnerService {
	mode = strings.TrimSpace(mode)
	appID = strings.TrimSpace(appID)
	appIDLabel = strings.TrimSpace(appIDLabel)
	routePrefix = strings.TrimSpace(routePrefix)
	token = strings.TrimSpace(token)
	encodingAESKey = strings.TrimSpace(encodingAESKey)
	var crypto *Crypto
	if appID != "" && token != "" && encodingAESKey != "" {
		if c, err := NewCrypto(token, encodingAESKey, appID); err == nil {
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
		mode:               mode,
		appID:              appID,
		appIDLabel:         firstNonEmpty(appIDLabel, "suite_id"),
		routePrefix:        strings.TrimRight(firstNonEmpty(routePrefix, "/api/v1/wecom/partner"), "/"),
		installURLBase:     "https://open.work.weixin.qq.com/3rdapp/install",
		jwtSecret:          jwtSecret,
		jwtExpiryHours:     jwtExpiryHours,
		callbackBaseURL:    strings.TrimRight(strings.TrimSpace(callbackBaseURL), "/"),
		installRedirectURL: strings.TrimSpace(installRedirectURL),
		installAuthType:    installAuthType,
	}
}

func (s *PartnerService) IsEnabled() bool {
	return s.client != nil && s.store != nil && s.authStore != nil && s.crypto != nil && strings.TrimSpace(s.appID) != ""
}

func (s *PartnerService) routePrefixValue() string {
	if s == nil {
		return ""
	}
	return s.routePrefix
}

func (s *PartnerService) verifyURL(signature, timestamp, nonce, echostr string) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("wecom %s mode is not configured", s.mode)
	}
	if !s.crypto.VerifySignature(signature, timestamp, nonce, echostr) {
		return "", fmt.Errorf("invalid signature")
	}
	return s.crypto.Decrypt(echostr)
}

func (s *PartnerService) handleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	if !s.IsEnabled() {
		return fmt.Errorf("wecom %s mode is not configured", s.mode)
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
	if event.SuiteID != "" && event.SuiteID != s.appID {
		return fmt.Errorf("%s mismatch: expected=%s actual=%s", s.appIDLabel, s.appID, event.SuiteID)
	}
	switch event.InfoType {
	case "suite_ticket":
		_ = s.store.SaveEventLog(ctx, event.AuthCorpID, event.InfoType, plain)
		return s.store.SaveSuiteTicket(ctx, SuiteTicketRecord{Mode: s.mode, ProviderApp: s.appID, SuiteID: s.appID, SuiteTicket: event.SuiteTicket})
	case "create_auth", "change_auth":
		existingCount, err := s.store.CountEventLogsByPayload(ctx, event.InfoType, plain)
		if err != nil {
			return err
		}
		_ = s.store.SaveEventLog(ctx, event.AuthCorpID, event.InfoType, plain)
		if existingCount > 0 {
			slog.Info("skip duplicate wecom auth callback",
				"mode", s.mode,
				"info_type", event.InfoType,
				"corp_id", event.AuthCorpID,
			)
			return nil
		}
		if strings.TrimSpace(event.AuthCode) == "" {
			slog.Info("skip wecom auth callback sync because auth_code is missing",
				"mode", s.mode,
				"info_type", event.InfoType,
				"corp_id", event.AuthCorpID,
			)
			return nil
		}
		go s.syncCorpInstallAsync(event.InfoType, event.AuthCorpID, event.AuthCode)
		return nil
	case "cancel_auth":
		_ = s.store.SaveEventLog(ctx, event.AuthCorpID, event.InfoType, plain)
		return s.store.MarkCorpInstallCancelled(ctx, s.mode, s.appID, event.AuthCorpID)
	default:
		_ = s.store.SaveEventLog(ctx, event.AuthCorpID, event.InfoType, plain)
		return nil
	}
}

func (s *PartnerService) syncCorpInstallAsync(infoType, corpID, authCode string) {
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		syncCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, err := s.syncCorpInstall(syncCtx, 0, corpID, authCode)
		cancel()
		if err == nil {
			slog.Info("wecom auth callback synced asynchronously",
				"mode", s.mode,
				"info_type", infoType,
				"corp_id", corpID,
				"attempt", attempt,
			)
			return
		}
		slog.Warn("wecom auth callback async sync failed",
			"mode", s.mode,
			"info_type", infoType,
			"corp_id", corpID,
			"attempt", attempt,
			"error", err,
		)
		if attempt < maxAttempts {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
}

func (s *PartnerService) completeOAuthLogin(ctx context.Context, install *CorpInstallRecord, corpAccessToken string, userInfo *userInfo3rdResponse) (*OAuthLoginResponse, error) {
	if install == nil {
		return nil, fmt.Errorf("corp install not found")
	}
	if userInfo == nil || strings.TrimSpace(userInfo.UserID) == "" {
		return nil, fmt.Errorf("wecom returned empty user id")
	}
	userDetail, err := s.client.GetUserDetail(ctx, corpAccessToken, userInfo.UserID)
	if (err != nil || strings.TrimSpace(userDetail.Mobile) == "") && strings.TrimSpace(userInfo.UserTicket) != "" {
		userDetail, err = s.client.GetAuthUserDetail(ctx, corpAccessToken, userInfo.UserTicket)
	}
	if err != nil {
		return nil, err
	}
	profile := &OAuthUserProfile{
		CorpID:      install.CorpID,
		WeComUserID: userInfo.UserID,
		OpenUserID:  userInfo.OpenUserID,
		Name:        userDetail.Name,
		Mobile:      userDetail.Mobile,
		Avatar:      userDetail.Avatar,
	}
	binding, err := s.store.GetUserBinding(ctx, s.mode, s.appID, install.CorpID, userInfo.UserID)
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
		employeeID, err := s.store.FindUniqueEmployeeIDByPhoneAndTenant(ctx, mobile, install.TenantID)
		if err != nil {
			return nil, err
		}
		if employeeID != nil {
			employee, err := s.authStore.GetEmployeeByID(ctx, *employeeID)
			if err != nil {
				return nil, err
			}
			if install.TenantID > 0 && employee.TenantID != install.TenantID {
				return nil, fmt.Errorf("tenant mismatch")
			}
			if err := s.store.UpsertUserBinding(ctx, UserBindingRecord{
				Mode:        s.mode,
				ProviderApp: s.appID,
				CorpID:      install.CorpID,
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

func (s *PartnerService) bindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	employee, err := s.authStore.GetEmployeeByID(ctx, employeeID)
	if err != nil {
		return err
	}
	return s.store.UpsertUserBinding(ctx, UserBindingRecord{
		Mode:        s.mode,
		ProviderApp: s.appID,
		CorpID:      strings.TrimSpace(corpID),
		WeComUserID: strings.TrimSpace(wecomUserID),
		EmployeeID:  employee.ID,
		TenantID:    employee.TenantID,
		Source:      "manual_bind",
	})
}

func (s *PartnerService) syncCorpInstall(ctx context.Context, tenantID int64, corpID, authCode string) (*CorpInstallRecord, error) {
	authCode = strings.TrimSpace(authCode)
	if authCode == "" {
		return nil, fmt.Errorf("missing auth_code")
	}
	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.mode, s.appID)
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
		Mode:          s.mode,
		ProviderApp:   s.appID,
		TenantID:      tenantID,
		CorpID:        corpID,
		CorpName:      strings.TrimSpace(infoResp.AuthCorpInfo.CorpName),
		PermanentCode: permanentResp.PermanentCode,
		AgentID:       agentID,
		Status:        "active",
	}
	if err := s.store.UpsertCorpInstall(ctx, record); err != nil {
		return nil, err
	}
	slog.Info("wecom third-party corp install synced", "mode", s.mode, "corp_id", record.CorpID, "corp_name", record.CorpName, "agent_id", record.AgentID)
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

func (s *PartnerService) sendInternalMessage(ctx context.Context, req InternalSendMessageRequest, resolveCorpToken func(context.Context, *CorpInstallRecord) (string, int64, error)) (*InternalSendMessageResponse, error) {
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
		binding, err := s.store.GetActivePartnerBindingByEmployeeID(ctx, s.mode, s.appID, employeeID)
		if err != nil {
			return nil, err
		}
		if binding == nil || strings.TrimSpace(binding.CorpID) == "" || strings.TrimSpace(binding.WeComUserID) == "" || binding.AgentID <= 0 {
			perRecipientKey := fmt.Sprintf("%s:%d", strings.TrimSpace(req.DedupeKey), employeeID)
			_, inserted, insertErr := s.store.InsertMessageLog(ctx, MessageLogRecord{
				Mode:           s.mode,
				ProviderApp:    s.appID,
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

		targetURL := attachWeComPartnerEntryParams(req.TargetURL, s.appID, binding.CorpID, s.mode)
		install, ok := appCache[binding.CorpID]
		if !ok {
			install, err = s.store.GetCorpInstallByCorpID(ctx, s.mode, s.appID, binding.CorpID)
			if err != nil {
				_, inserted, insertErr := s.store.InsertMessageLog(ctx, MessageLogRecord{
					Mode:           s.mode,
					ProviderApp:    s.appID,
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
				Mode:           s.mode,
				ProviderApp:    s.appID,
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
			Mode:           s.mode,
			ProviderApp:    s.appID,
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
			corpToken, _, err = resolveCorpToken(ctx, install)
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
				corpToken, _, err = resolveCorpToken(ctx, install)
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

package wecom

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	internalauth "github.com/freeasyman/lingce-api/internal/auth"
	jwtauth "github.com/freeasyman/lingce-api/pkg/auth"
)

type Service struct {
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

func NewService(store *Store, authStore *internalauth.Store, client *Client, crypto *Crypto, suiteID, jwtSecret string, jwtExpiryHours int, callbackBaseURL, installRedirectURL string, installAuthType int) *Service {
	return &Service{
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

func (s *Service) IsEnabled() bool {
	return s.client != nil && s.crypto != nil && strings.TrimSpace(s.suiteID) != ""
}

func (s *Service) VerifyURL(signature, timestamp, nonce, echostr string) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("wecom is not configured")
	}
	if !s.crypto.VerifySignature(signature, timestamp, nonce, echostr) {
		return "", fmt.Errorf("invalid signature")
	}
	return s.crypto.Decrypt(echostr)
}

func (s *Service) HandleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	if !s.IsEnabled() {
		return fmt.Errorf("wecom is not configured")
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
	if event.SuiteID != "" && strings.TrimSpace(s.suiteID) != "" && event.SuiteID != s.suiteID {
		return fmt.Errorf("suite id mismatch")
	}
	_ = s.store.SaveEventLog(ctx, event.AuthCorpID, event.InfoType, plain)
	slog.Info("wecom callback received", "info_type", event.InfoType, "corp_id", event.AuthCorpID)

	switch event.InfoType {
	case "suite_ticket":
		return s.store.SaveSuiteTicket(ctx, SuiteTicketRecord{SuiteID: s.suiteID, SuiteTicket: event.SuiteTicket})
	case "create_auth", "change_auth":
		install, err := s.syncCorpInstall(ctx, event.AuthCorpID, event.AuthCode)
		if err != nil {
			if s.shouldTreatCreateAuthAsSuccess(ctx, event.InfoType, plain, err) {
				slog.Warn("wecom duplicate create_auth ignored", "info_type", event.InfoType, "suite_id", s.suiteID, "auth_code_prefix", truncateToken(event.AuthCode, 16), "error", err)
				return nil
			}
			slog.Error("wecom sync corp install failed", "info_type", event.InfoType, "suite_id", s.suiteID, "auth_corp_id", event.AuthCorpID, "auth_code_prefix", truncateToken(event.AuthCode, 16), "error", err)
			return err
		}
		slog.Info("wecom sync corp install succeeded", "info_type", event.InfoType, "suite_id", s.suiteID, "corp_id", install.CorpID, "corp_name", install.CorpName, "agent_id", install.AgentID)
		return nil
	case "cancel_auth":
		return s.store.MarkCorpInstallCancelled(ctx, event.AuthCorpID)
	default:
		return nil
	}
}

func (s *Service) BuildInstallURL(ctx context.Context, state string, authType int) (*InstallURLResponse, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom is not configured")
	}
	redirectURI, err := s.getInstallRedirectURI()
	if err != nil {
		return nil, err
	}
	if authType < 0 {
		authType = s.installAuthType
	}
	if authType < 0 {
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

func (s *Service) HandleInstallCallback(ctx context.Context, authCode string) (*InstallCallbackResult, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom is not configured")
	}
	if strings.TrimSpace(authCode) == "" {
		return nil, fmt.Errorf("auth_code is required")
	}
	install, err := s.syncCorpInstall(ctx, "", authCode)
	if err != nil {
		return nil, err
	}
	return &InstallCallbackResult{CorpID: install.CorpID, CorpName: install.CorpName}, nil
}

func (s *Service) LoginWithOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom is not configured")
	}
	slog.Info("wecom oauth login started", "corp_id", corpID, "code_prefix", truncateToken(code, 12))
	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.suiteID)
	if err != nil {
		slog.Warn("wecom oauth login failed: suite ticket missing", "error", err)
		return nil, err
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		slog.Warn("wecom oauth login failed: suite access token", "error", err)
		return nil, err
	}
	userInfo, err := s.client.GetUserInfo3rd(ctx, suiteAccessToken, code)
	if err != nil {
		slog.Warn("wecom oauth login failed: get user info", "corp_id", corpID, "code_prefix", truncateToken(code, 12), "error", err)
		return nil, err
	}
	if corpID == "" {
		corpID = userInfo.CorpID
	}
	if corpID == "" {
		slog.Warn("wecom oauth login failed: corp id empty", "user_info_corp_id", userInfo.CorpID)
		return nil, fmt.Errorf("corp id is empty")
	}
	if strings.TrimSpace(userInfo.UserID) == "" {
		slog.Warn("wecom oauth login failed: empty user id", "corp_id", corpID)
		return nil, fmt.Errorf("wecom returned empty user id")
	}
	install, err := s.store.GetCorpInstallByCorpID(ctx, corpID)
	if err != nil {
		slog.Warn("wecom oauth login failed: corp install not found", "corp_id", corpID, "error", err)
		return nil, err
	}
	corpAccessToken, _, err := s.client.GetCorpToken(ctx, suiteAccessToken, corpID, install.PermanentCode)
	if err != nil {
		slog.Warn("wecom oauth login failed: get corp token", "corp_id", corpID, "error", err)
		return nil, err
	}
	userDetail, err := s.client.GetUserDetail(ctx, corpAccessToken, userInfo.UserID)
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
	binding, err := s.store.GetUserBinding(ctx, corpID, userInfo.UserID)
	if err != nil {
		slog.Warn("wecom oauth login failed: query binding", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "error", err)
		return nil, err
	}
	if mobile := strings.TrimSpace(userDetail.Mobile); mobile != "" {
		employeeID, err := s.store.FindUniqueEmployeeIDByPhone(ctx, mobile)
		if err != nil {
			slog.Warn("wecom oauth login failed: find employee by phone", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "mobile_suffix", maskPhone(mobile), "error", err)
			return nil, err
		}
		if employeeID != nil {
			if binding == nil || binding.EmployeeID != *employeeID {
				employee, err := s.authStore.GetEmployeeByID(ctx, *employeeID)
				if err != nil {
					slog.Warn("wecom oauth login failed: load employee for auto bind", "employee_id", *employeeID, "error", err)
					return nil, err
				}
				source := "auto_phone"
				if binding != nil && binding.EmployeeID != employee.ID {
					source = "auto_phone_rebind"
				}
				if err := s.store.UpsertUserBinding(ctx, UserBindingRecord{
					CorpID:      corpID,
					WeComUserID: userInfo.UserID,
					EmployeeID:  employee.ID,
					TenantID:    employee.TenantID,
					Source:      source,
				}); err != nil {
					slog.Warn("wecom oauth login failed: upsert auto binding", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "employee_id", employee.ID, "error", err)
					return nil, err
				}
				slog.Info("wecom oauth auto binding updated", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "employee_id", employee.ID, "source", source)
				binding = &UserBindingRecord{
					CorpID:      corpID,
					WeComUserID: userInfo.UserID,
					EmployeeID:  employee.ID,
					TenantID:    employee.TenantID,
					Source:      source,
				}
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
	slog.Info("wecom oauth login needs bind", "corp_id", corpID, "wecom_user_id", userInfo.UserID, "mobile_suffix", maskPhone(userDetail.Mobile))
	return &OAuthLoginResponse{Status: "needs_bind", Profile: profile}, nil
}

func (s *Service) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	employee, err := s.authStore.GetEmployeeByID(ctx, employeeID)
	if err != nil {
		return err
	}
	if err := s.store.UpsertUserBinding(ctx, UserBindingRecord{
		CorpID:      corpID,
		WeComUserID: wecomUserID,
		EmployeeID:  employee.ID,
		TenantID:    employee.TenantID,
		Source:      "manual",
	}); err != nil {
		return err
	}
	slog.Info("wecom manual binding created", "corp_id", corpID, "wecom_user_id", wecomUserID, "employee_id", employee.ID)
	return nil
}

func (s *Service) SendInternalMessage(ctx context.Context, req InternalSendMessageRequest) (*InternalSendMessageResponse, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom is not configured")
	}
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

	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.suiteID)
	if err != nil {
		return nil, err
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		return nil, err
	}

	corpTokens := make(map[string]string)
	for _, employeeID := range employeeIDs {
		binding, err := s.store.GetActiveBindingByEmployeeID(ctx, employeeID)
		if err != nil {
			return nil, err
		}
		perRecipientKey := fmt.Sprintf("%s:%d", strings.TrimSpace(req.DedupeKey), employeeID)
		if binding == nil || strings.TrimSpace(binding.CorpID) == "" || strings.TrimSpace(binding.WeComUserID) == "" || binding.AgentID <= 0 {
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

		requestPayload := mustJSON(map[string]any{
			"message_scene": req.MessageScene,
			"employee_id":   employeeID,
			"corp_id":       binding.CorpID,
			"wecom_user_id": binding.WeComUserID,
			"title":         req.Title,
			"content":       req.Content,
			"target_url":    req.TargetURL,
			"button_text":   req.ButtonText,
			"extra":         req.Extra,
		})
		logID, inserted, err := s.store.InsertMessageLog(ctx, MessageLogRecord{
			CorpID:         binding.CorpID,
			TenantID:       binding.TenantID,
			EmployeeID:     employeeID,
			WeComUserID:    binding.WeComUserID,
			MessageScene:   req.MessageScene,
			DedupeKey:      perRecipientKey,
			Title:          req.Title,
			Content:        req.Content,
			TargetURL:      req.TargetURL,
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

		corpToken, ok := corpTokens[binding.CorpID]
		if !ok {
			corpToken, _, err = s.client.GetCorpToken(ctx, suiteAccessToken, binding.CorpID, binding.PermanentCode)
			if err != nil {
				_ = s.store.UpdateMessageLogStatus(ctx, logID, "failed", err.Error(), "")
				resp.Failed++
				continue
			}
			corpTokens[binding.CorpID] = corpToken
		}

		sendResp, err := s.client.SendTextCardMessage(ctx, corpToken, binding.AgentID, binding.WeComUserID, req.Title, req.Content, req.TargetURL, req.ButtonText)
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

func (s *Service) syncCorpInstall(ctx context.Context, corpID, authCode string) (*CorpInstallRecord, error) {
	if strings.TrimSpace(authCode) == "" {
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
		CorpName:      infoResp.AuthCorpInfo.CorpName,
		PermanentCode: permanentResp.PermanentCode,
		AgentID:       agentID,
		Status:        "active",
	}
	if err := s.store.UpsertCorpInstall(ctx, record); err != nil {
		return nil, err
	}
	return &record, nil
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

func (s *Service) getInstallRedirectURI() (string, error) {
	if strings.TrimSpace(s.installRedirectURL) != "" {
		return s.installRedirectURL, nil
	}
	if strings.TrimSpace(s.callbackBaseURL) == "" {
		return "", fmt.Errorf("wecom install redirect url is not configured")
	}
	return s.callbackBaseURL + "/api/v1/wecom/install/callback", nil
}

func sanitizeState(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func (s *Service) shouldTreatCreateAuthAsSuccess(ctx context.Context, infoType, rawPayload string, err error) bool {
	if err == nil {
		return false
	}
	if !strings.Contains(err.Error(), "40078 invalid auth_code") {
		return false
	}
	count, countErr := s.store.CountEventLogsByPayload(ctx, infoType, rawPayload)
	if countErr != nil {
		slog.Warn("wecom duplicate callback check failed", "info_type", infoType, "error", countErr)
		return false
	}
	return count > 1
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

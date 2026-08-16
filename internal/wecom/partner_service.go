package wecom

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
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

func (s *PartnerService) VerifyURL(signature, timestamp, nonce, echostr string) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("wecom %s mode is not configured", s.mode)
	}
	if !s.crypto.VerifySignature(signature, timestamp, nonce, echostr) {
		return "", fmt.Errorf("invalid signature")
	}
	return s.crypto.Decrypt(echostr)
}

func (s *PartnerService) VerifyEnterpriseURL(corpID, signature, timestamp, nonce, echostr string) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("wecom %s mode is not configured", s.mode)
	}
	corpID = strings.TrimSpace(corpID)
	if corpID != "" {
		crypto, err := NewCrypto(s.crypto.token, base64EncodingAESKey(s.crypto.aesKey), corpID)
		if err != nil {
			return "", err
		}
		if !crypto.VerifySignature(signature, timestamp, nonce, echostr) {
			return "", fmt.Errorf("invalid signature")
		}
		return crypto.Decrypt(echostr)
	}

	installs, err := s.store.ListActiveCorpInstallsByMode(context.Background(), s.mode, s.appID)
	if err != nil {
		return "", err
	}
	errs := make([]string, 0, len(installs))
	for _, install := range installs {
		if install == nil || strings.TrimSpace(install.CorpID) == "" {
			continue
		}
		crypto, err := NewCrypto(s.crypto.token, base64EncodingAESKey(s.crypto.aesKey), strings.TrimSpace(install.CorpID))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", install.CorpID, err))
			continue
		}
		if !crypto.VerifySignature(signature, timestamp, nonce, echostr) {
			continue
		}
		plain, err := crypto.Decrypt(echostr)
		if err == nil {
			return plain, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", install.CorpID, err))
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("no active corp install matched callback")
	}
	return "", errors.New(strings.Join(errs, "; "))
}

func (s *PartnerService) HandleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
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

func (s *PartnerService) HandleEnterpriseCallback(ctx context.Context, corpID, signature, timestamp, nonce string, body []byte) error {
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
	corpID = strings.TrimSpace(corpID)
	candidateCorpIDs := make([]string, 0, 8)
	if corpID != "" {
		candidateCorpIDs = append(candidateCorpIDs, corpID)
	} else {
		installs, err := s.store.ListActiveCorpInstallsByMode(ctx, s.mode, s.appID)
		if err != nil {
			return err
		}
		for _, install := range installs {
			if install == nil || strings.TrimSpace(install.CorpID) == "" {
				continue
			}
			candidateCorpIDs = append(candidateCorpIDs, strings.TrimSpace(install.CorpID))
		}
	}
	if len(candidateCorpIDs) == 0 {
		return fmt.Errorf("no active corp install matched callback")
	}
	errs := make([]string, 0, len(candidateCorpIDs))
	for _, candidateCorpID := range candidateCorpIDs {
		crypto, err := NewCrypto(s.crypto.token, base64EncodingAESKey(s.crypto.aesKey), candidateCorpID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", candidateCorpID, err))
			continue
		}
		if !crypto.VerifySignature(signature, timestamp, nonce, envelope.Encrypt) {
			continue
		}
		plain, err := crypto.Decrypt(envelope.Encrypt)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", candidateCorpID, err))
			continue
		}
		var event CallbackEvent
		if err := xml.Unmarshal([]byte(plain), &event); err != nil {
			return fmt.Errorf("parse decrypted callback: %w", err)
		}
		_ = s.store.SaveEventLog(ctx, candidateCorpID, firstNonEmpty(event.InfoType, "enterprise_callback"), plain)
		return nil
	}
	if len(errs) == 0 {
		return fmt.Errorf("invalid signature")
	}
	return errors.New(strings.Join(errs, "; "))
}

func base64EncodingAESKey(raw []byte) string {
	encoded := base64.StdEncoding.EncodeToString(raw)
	return strings.TrimSuffix(encoded, "=")
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

func (s *PartnerService) BuildInstallURL(ctx context.Context, state string, authType int) (*InstallURLResponse, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom %s mode is not configured", s.mode)
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
	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.mode, s.appID)
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
	installURL := s.installURLBase + "?suite_id=" + url.QueryEscape(s.appID) +
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

func (s *PartnerService) HandleInstallCallback(ctx context.Context, authCode, state string) (*InstallCallbackResult, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom %s mode is not configured", s.mode)
	}
	tenantID := decodePartnerInstallTenantID(state)
	install, err := s.syncCorpInstall(ctx, tenantID, "", authCode)
	if err != nil {
		return nil, err
	}
	return &InstallCallbackResult{TenantID: install.TenantID, CorpID: install.CorpID, CorpName: install.CorpName}, nil
}

func (s *PartnerService) LoginWithOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom %s mode is not configured", s.mode)
	}
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("code is required")
	}
	if s.mode == ModePartnerTemplate {
		return s.loginWithTemplateOAuth(ctx, code, corpID)
	}
	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.mode, s.appID)
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
	if s.mode == ModePartnerTemplate || corpID == "" {
		corpID = userInfo.CorpID
	}
	corpID = strings.TrimSpace(corpID)
	if corpID == "" {
		return nil, fmt.Errorf("corp_id is required")
	}
	if strings.TrimSpace(userInfo.UserID) == "" {
		return nil, fmt.Errorf("wecom returned empty user id")
	}
	install, err := s.store.GetCorpInstallByCorpID(ctx, s.mode, s.appID, corpID)
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
	binding, err := s.store.GetUserBinding(ctx, s.mode, s.appID, corpID, userInfo.UserID)
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

func (s *PartnerService) loginWithTemplateOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	install, corpAccessToken, userInfo, err := s.resolveTemplateOAuthContext(ctx, code, strings.TrimSpace(corpID))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(userInfo.UserID) == "" {
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
	if binding == nil {
		binding, err = s.store.GetAnyUserBinding(ctx, install.CorpID, userInfo.UserID)
		if err != nil {
			return nil, err
		}
		if binding != nil {
			if err := s.store.UpsertUserBinding(ctx, UserBindingRecord{
				Mode:        s.mode,
				ProviderApp: s.appID,
				CorpID:      install.CorpID,
				WeComUserID: userInfo.UserID,
				EmployeeID:  binding.EmployeeID,
				TenantID:    binding.TenantID,
				Source:      "legacy_promoted",
			}); err != nil {
				return nil, err
			}
		}
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

func (s *PartnerService) resolveTemplateOAuthContext(ctx context.Context, code, corpID string) (*CorpInstallRecord, string, *userInfo3rdResponse, error) {
	if corpID != "" {
		install, err := s.store.GetCorpInstallByCorpID(ctx, s.mode, s.appID, corpID)
		if err == nil {
			corpAccessToken, _, tokenErr := s.resolveTemplateCorpToken(ctx, install)
			if tokenErr != nil {
				return nil, "", nil, tokenErr
			}
			userInfo, userErr := s.client.GetCorpUserInfo(ctx, corpAccessToken, code)
			if userErr != nil {
				return nil, "", nil, userErr
			}
			return install, corpAccessToken, userInfo, nil
		}
	}

	// A code can only be consumed by the enterprise that issued it. Trying the
	// active installs preserves existing deployments until public-to-open CorpID
	// conversion is configured for deterministic lookup.
	installs, err := s.store.ListActiveCorpInstallsByMode(ctx, s.mode, s.appID)
	if err != nil {
		return nil, "", nil, err
	}
	var lastErr error
	for _, install := range installs {
		if install == nil || strings.TrimSpace(install.CorpID) == "" {
			continue
		}
		corpAccessToken, _, tokenErr := s.resolveTemplateCorpToken(ctx, install)
		if tokenErr != nil {
			lastErr = tokenErr
			continue
		}
		userInfo, userErr := s.client.GetCorpUserInfo(ctx, corpAccessToken, code)
		if userErr != nil {
			lastErr = userErr
			continue
		}
		if strings.TrimSpace(userInfo.UserID) != "" {
			return install, corpAccessToken, userInfo, nil
		}
		lastErr = fmt.Errorf("wecom returned empty user id")
	}
	if lastErr != nil {
		return nil, "", nil, lastErr
	}
	return nil, "", nil, fmt.Errorf("corp install not found")
}

func (s *PartnerService) resolveTemplateCorpToken(ctx context.Context, install *CorpInstallRecord) (string, int64, error) {
	if install == nil {
		return "", 0, fmt.Errorf("corp install not found")
	}
	// For a partner-template app, permanent_code is the generated Secret of
	// the customer's commissioned self-built app. It must use gettoken, not
	// the standard third-party application's service/get_corp_token endpoint.
	return s.client.GetCorpAccessToken(ctx, install.CorpID, install.PermanentCode)
}

func (s *PartnerService) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
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
			corpToken, _, err = s.resolveInstalledCorpToken(ctx, install)
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
				corpToken, _, err = s.resolveInstalledCorpToken(ctx, install)
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

func (s *PartnerService) resolveInstalledCorpToken(ctx context.Context, install *CorpInstallRecord) (string, int64, error) {
	if s.mode == ModePartnerTemplate {
		return s.resolveTemplateCorpToken(ctx, install)
	}
	if install == nil {
		return "", 0, fmt.Errorf("corp install not found")
	}
	suiteTicket, err := s.store.GetLatestSuiteTicket(ctx, s.mode, s.appID)
	if err != nil {
		return "", 0, err
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		return "", 0, err
	}
	return s.client.GetCorpToken(ctx, suiteAccessToken, install.CorpID, install.PermanentCode)
}

func (s *PartnerService) getInstallRedirectURI() (string, error) {
	if s.installRedirectURL != "" {
		return s.installRedirectURL, nil
	}
	if s.callbackBaseURL == "" {
		return "", fmt.Errorf("wecom install redirect url is not configured")
	}
	return s.callbackBaseURL + s.routePrefix + "/install/callback", nil
}

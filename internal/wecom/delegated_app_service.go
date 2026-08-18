package wecom

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	internalauth "github.com/freeasyman/lingce-api/internal/auth"
)

type PartnerTemplateService struct {
	*PartnerService
}

func NewPartnerTemplateService(store *Store, authStore *internalauth.Store, client *Client, jwtSecret string, jwtExpiryHours int, appID, appIDLabel, routePrefix, token, encodingAESKey, callbackBaseURL, installRedirectURL string, installAuthType int) *PartnerTemplateService {
	return &PartnerTemplateService{
		PartnerService: NewPartnerService(
			store,
			authStore,
			client,
			jwtSecret,
			jwtExpiryHours,
			ModePartnerTemplate,
			appID,
			appIDLabel,
			routePrefix,
			token,
			encodingAESKey,
			callbackBaseURL,
			installRedirectURL,
			installAuthType,
		),
	}
}

func (s *PartnerTemplateService) RoutePrefix() string {
	return s.routePrefixValue()
}

func (s *PartnerTemplateService) SupportsInstallFlow() bool {
	return false
}

func (s *PartnerTemplateService) VerifyURL(signature, timestamp, nonce, echostr string) (string, error) {
	return s.verifyURL(signature, timestamp, nonce, echostr)
}

func (s *PartnerTemplateService) HandleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	return s.handleCallback(ctx, signature, timestamp, nonce, body)
}

func (s *PartnerTemplateService) LoginWithOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	return s.loginTemplateOAuth(ctx, code, corpID)
}

func (s *PartnerTemplateService) VerifyEnterpriseURL(corpID, signature, timestamp, nonce, echostr string) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("wecom %s mode is not configured", s.mode)
	}
	corpID = strings.TrimSpace(corpID)
	if corpID != "" {
		crypto, err := NewCrypto(s.crypto.token, templateBase64EncodingAESKey(s.crypto.aesKey), corpID)
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
		crypto, err := NewCrypto(s.crypto.token, templateBase64EncodingAESKey(s.crypto.aesKey), strings.TrimSpace(install.CorpID))
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

func (s *PartnerTemplateService) HandleEnterpriseCallback(ctx context.Context, corpID, signature, timestamp, nonce string, body []byte) error {
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
		crypto, err := NewCrypto(s.crypto.token, templateBase64EncodingAESKey(s.crypto.aesKey), candidateCorpID)
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
		eventType := firstNonEmpty(strings.TrimSpace(event.InfoType), strings.TrimSpace(event.Event), "enterprise_callback")
		_ = s.store.SaveEventLog(ctx, candidateCorpID, eventType, plain)
		return nil
	}
	if len(errs) == 0 {
		return fmt.Errorf("invalid signature")
	}
	return errors.New(strings.Join(errs, "; "))
}

func (s *PartnerTemplateService) BuildInstallURL(ctx context.Context, state string, authType int) (*InstallURLResponse, error) {
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

func (s *PartnerTemplateService) HandleInstallCallback(ctx context.Context, authCode, state string) (*InstallCallbackResult, error) {
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

func (s *PartnerTemplateService) SendInternalMessage(ctx context.Context, req InternalSendMessageRequest) (*InternalSendMessageResponse, error) {
	return s.sendInternalMessage(ctx, req, s.resolveTemplateCorpToken)
}

func (s *PartnerTemplateService) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	return s.bindEmployee(ctx, corpID, wecomUserID, employeeID)
}

func (s *PartnerTemplateService) loginTemplateOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("wecom %s mode is not configured", s.mode)
	}
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("code is required")
	}
	install, corpAccessToken, userInfo, err := s.resolveTemplateOAuthContext(ctx, code, strings.TrimSpace(corpID))
	if err != nil {
		return nil, err
	}
	return s.completeOAuthLogin(ctx, install, corpAccessToken, userInfo)
}

func (s *PartnerTemplateService) resolveTemplateOAuthContext(ctx context.Context, code, corpID string) (*CorpInstallRecord, string, *userInfo3rdResponse, error) {
	if corpID == "" {
		return nil, "", nil, ErrCorpInstallContextMiss
	}
	install, err := s.store.GetCorpInstallByCorpID(ctx, s.mode, s.appID, corpID)
	if err != nil {
		return nil, "", nil, err
	}
	corpAccessToken, _, err := s.resolveTemplateCorpToken(ctx, install)
	if err != nil {
		return nil, "", nil, err
	}
	userInfo, err := s.client.GetCorpUserInfo(ctx, corpAccessToken, code)
	if err != nil {
		return nil, "", nil, err
	}
	return install, corpAccessToken, userInfo, nil
}

func (s *PartnerTemplateService) resolveTemplateCorpToken(ctx context.Context, install *CorpInstallRecord) (string, int64, error) {
	if install == nil {
		return "", 0, ErrCorpInstallMissing
	}
	return s.client.GetCorpAccessToken(ctx, install.CorpID, install.PermanentCode)
}

func (s *PartnerTemplateService) getInstallRedirectURI() (string, error) {
	if s.installRedirectURL != "" {
		return s.installRedirectURL, nil
	}
	if s.callbackBaseURL == "" {
		return "", fmt.Errorf("wecom install redirect url is not configured")
	}
	return s.callbackBaseURL + s.routePrefix + "/install/callback", nil
}

func templateBase64EncodingAESKey(raw []byte) string {
	encoded := base64.StdEncoding.EncodeToString(raw)
	return strings.TrimSuffix(encoded, "=")
}

package wecom

import (
	"context"

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
	return s.verifyEnterpriseURL(corpID, signature, timestamp, nonce, echostr)
}

func (s *PartnerTemplateService) HandleEnterpriseCallback(ctx context.Context, corpID, signature, timestamp, nonce string, body []byte) error {
	return s.handleEnterpriseCallback(ctx, corpID, signature, timestamp, nonce, body)
}

func (s *PartnerTemplateService) BuildInstallURL(ctx context.Context, state string, authType int) (*InstallURLResponse, error) {
	return s.buildInstallURL(ctx, state, authType)
}

func (s *PartnerTemplateService) HandleInstallCallback(ctx context.Context, authCode, state string) (*InstallCallbackResult, error) {
	return s.handleInstallCallback(ctx, authCode, state)
}

func (s *PartnerTemplateService) SendInternalMessage(ctx context.Context, req InternalSendMessageRequest) (*InternalSendMessageResponse, error) {
	return s.sendInternalMessage(ctx, req, s.resolveTemplateCorpToken)
}

func (s *PartnerTemplateService) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	return s.bindEmployee(ctx, corpID, wecomUserID, employeeID)
}

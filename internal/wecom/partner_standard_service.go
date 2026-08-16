package wecom

import (
	"context"

	internalauth "github.com/freeasyman/lingce-api/internal/auth"
)

type PartnerStandardService struct {
	*PartnerService
}

func NewPartnerStandardService(store *Store, authStore *internalauth.Store, client *Client, jwtSecret string, jwtExpiryHours int, appID, appIDLabel, routePrefix, token, encodingAESKey, callbackBaseURL, installRedirectURL string, installAuthType int) *PartnerStandardService {
	return &PartnerStandardService{
		PartnerService: NewPartnerService(
			store,
			authStore,
			client,
			jwtSecret,
			jwtExpiryHours,
			ModePartnerStandard,
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

func (s *PartnerStandardService) RoutePrefix() string {
	return s.routePrefixValue()
}

func (s *PartnerStandardService) VerifyURL(signature, timestamp, nonce, echostr string) (string, error) {
	return s.verifyURL(signature, timestamp, nonce, echostr)
}

func (s *PartnerStandardService) HandleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	return s.handleCallback(ctx, signature, timestamp, nonce, body)
}

func (s *PartnerStandardService) LoginWithOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	return s.loginStandardOAuth(ctx, code, corpID)
}

func (s *PartnerStandardService) VerifyEnterpriseURL(corpID, signature, timestamp, nonce, echostr string) (string, error) {
	return s.verifyEnterpriseURL(corpID, signature, timestamp, nonce, echostr)
}

func (s *PartnerStandardService) HandleEnterpriseCallback(ctx context.Context, corpID, signature, timestamp, nonce string, body []byte) error {
	return s.handleEnterpriseCallback(ctx, corpID, signature, timestamp, nonce, body)
}

func (s *PartnerStandardService) BuildInstallURL(ctx context.Context, state string, authType int) (*InstallURLResponse, error) {
	return s.buildInstallURL(ctx, state, authType)
}

func (s *PartnerStandardService) HandleInstallCallback(ctx context.Context, authCode, state string) (*InstallCallbackResult, error) {
	return s.handleInstallCallback(ctx, authCode, state)
}

func (s *PartnerStandardService) SendInternalMessage(ctx context.Context, req InternalSendMessageRequest) (*InternalSendMessageResponse, error) {
	return s.sendInternalMessage(ctx, req, s.resolveInstalledCorpToken)
}

func (s *PartnerStandardService) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	return s.bindEmployee(ctx, corpID, wecomUserID, employeeID)
}

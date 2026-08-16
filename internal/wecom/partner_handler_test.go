package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type stubPartnerHTTPService struct {
	routePrefix         string
	supportsInstallFlow bool
	loginErr            error
}

func (s *stubPartnerHTTPService) RoutePrefix() string {
	return s.routePrefix
}

func (s *stubPartnerHTTPService) VerifyURL(signature, timestamp, nonce, echostr string) (string, error) {
	return "", nil
}

func (s *stubPartnerHTTPService) VerifyEnterpriseURL(corpID, signature, timestamp, nonce, echostr string) (string, error) {
	return "", nil
}

func (s *stubPartnerHTTPService) HandleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	return nil
}

func (s *stubPartnerHTTPService) HandleEnterpriseCallback(ctx context.Context, corpID, signature, timestamp, nonce string, body []byte) error {
	return nil
}

func (s *stubPartnerHTTPService) BuildInstallURL(ctx context.Context, state string, authType int) (*InstallURLResponse, error) {
	return &InstallURLResponse{InstallURL: "https://example.com/install"}, nil
}

func (s *stubPartnerHTTPService) HandleInstallCallback(ctx context.Context, authCode, state string) (*InstallCallbackResult, error) {
	return &InstallCallbackResult{}, nil
}

func (s *stubPartnerHTTPService) LoginWithOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	if s.loginErr != nil {
		return nil, s.loginErr
	}
	return &OAuthLoginResponse{Status: "logged_in"}, nil
}

func (s *stubPartnerHTTPService) SendInternalMessage(ctx context.Context, req InternalSendMessageRequest) (*InternalSendMessageResponse, error) {
	return nil, nil
}

func (s *stubPartnerHTTPService) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	return nil
}

func (s *stubPartnerHTTPService) SupportsInstallFlow() bool {
	return s.supportsInstallFlow
}

func TestPartnerHandlerRegisterRoutesSkipsInstallFlowForTemplateLikeService(t *testing.T) {
	mux := http.NewServeMux()
	handler := NewPartnerHandler(&stubPartnerHTTPService{
		routePrefix:         "/api/v1/wecom/partner-template",
		supportsInstallFlow: false,
	})
	handler.RegisterRoutes(mux, "", (*pgxpool.Pool)(nil), "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/wecom/partner-template/install-url", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for template install-url route, got %d", rec.Code)
	}
}

func TestWritePartnerAPIErrorMapsSuiteTicketMissing(t *testing.T) {
	rec := httptest.NewRecorder()
	writePartnerAPIError(rec, ErrSuiteTicketMissing)

	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "WECOM_SUITE_TICKET_MISSING") {
		t.Fatalf("expected suite-ticket error code in response, got %s", rec.Body.String())
	}
}

func TestWritePartnerAPIErrorMapsCorpInstallMissing(t *testing.T) {
	rec := httptest.NewRecorder()
	writePartnerAPIError(rec, ErrCorpInstallMissing)

	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "WECOM_CORP_INSTALL_MISSING") {
		t.Fatalf("expected corp-install error code in response, got %s", rec.Body.String())
	}
}

func TestWritePartnerAPIErrorMapsAPIError(t *testing.T) {
	rec := httptest.NewRecorder()
	writePartnerAPIError(rec, &APIError{Code: 40078, Message: "invalid auth_code"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "WECOM_API_ERROR") {
		t.Fatalf("expected api error code in response, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "40078") {
		t.Fatalf("expected errcode details in response, got %s", rec.Body.String())
	}
}

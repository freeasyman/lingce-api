package delegated

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	internalauth "github.com/freeasyman/lingce-api/internal/auth"
	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/opportunityalert"
	jwtauth "github.com/freeasyman/lingce-api/pkg/auth"
)

type Service struct {
	store                    *Store
	authStore                *internalauth.Store
	client                   *client
	providerApp              string
	providerCorpID           string
	token                    string
	encodingAESKey           string
	callbackURL              string
	trustedDomain            string
	launchURL                string
	enterpriseCallbackToken  string
	enterpriseCallbackAESKey string
	jwtSecret                string
	jwtExpiryHours           int
}

func NewService(store *Store, authStore *internalauth.Store, cfg config.WeComConfig, jwtSecret string, jwtExpiryHours int) *Service {
	callbackBaseURL := strings.TrimRight(strings.TrimSpace(cfg.DelegatedApp.CallbackBaseURL), "/")
	if jwtExpiryHours <= 0 {
		jwtExpiryHours = 24
	}
	return &Service{
		store:     store,
		authStore: authStore,
		client: newClient(
			strings.TrimSpace(cfg.APIBaseURL),
			strings.TrimSpace(cfg.DelegatedApp.SuiteID),
			strings.TrimSpace(cfg.DelegatedApp.SuiteSecret),
			strings.TrimSpace(cfg.Provider.CorpID),
			strings.TrimSpace(cfg.Provider.Secret),
		),
		providerApp:              strings.TrimSpace(cfg.DelegatedApp.SuiteID),
		providerCorpID:           strings.TrimSpace(cfg.Provider.CorpID),
		token:                    strings.TrimSpace(cfg.DelegatedApp.Token),
		encodingAESKey:           strings.TrimSpace(cfg.DelegatedApp.EncodingAESKey),
		callbackURL:              callbackBaseURL + "/api/v1/wecom/delegated-app/enterprise-callback",
		trustedDomain:            hostFromURL(defaultLaunchURL("")),
		launchURL:                callbackBaseURL + "/api/v1/wecom/delegated-app/launch",
		enterpriseCallbackToken:  strings.TrimSpace(cfg.DelegatedApp.EnterpriseCallback.Token),
		enterpriseCallbackAESKey: strings.TrimSpace(cfg.DelegatedApp.EnterpriseCallback.EncodingAESKey),
		jwtSecret:                strings.TrimSpace(jwtSecret),
		jwtExpiryHours:           jwtExpiryHours,
	}
}

func (s *Service) IsEnabled() bool {
	return s != nil &&
		s.store != nil &&
		s.authStore != nil &&
		s.client != nil &&
		strings.TrimSpace(s.providerApp) != ""
}

func (s *Service) ListCorpInstalls(ctx context.Context, tenantID *int64, corpID string) ([]*CorpInstallResponse, error) {
	items, err := s.store.ListCorpInstalls(ctx, tenantID, corpID)
	if err != nil {
		return nil, err
	}
	resp := make([]*CorpInstallResponse, 0, len(items))
	for _, item := range items {
		row := s.toResponse(item)
		if item != nil {
			health, _ := s.CheckInstallHealth(ctx, item)
			row.HealthCheck = health
			row.HealthSummary = summarizeHealthCheck(health)
		}
		resp = append(resp, row)
	}
	return resp, nil
}

func (s *Service) GetCorpInstallDetail(ctx context.Context, providerApp, corpID string) (*CorpInstallDetailResponse, error) {
	item, err := s.store.GetCorpInstallByCorpID(ctx, providerApp, corpID)
	if err != nil {
		return nil, err
	}
	events, err := s.store.ListRecentEventLogsByCorpID(ctx, corpID, 20)
	if err != nil {
		return nil, err
	}
	respEvents := make([]*EventLogResponse, 0, len(events))
	for _, event := range events {
		respEvents = append(respEvents, &EventLogResponse{
			ID:         fmt.Sprintf("%d", event.ID),
			CorpID:     event.CorpID,
			InfoType:   event.InfoType,
			RawPayload: event.RawPayload,
			CreatedAt:  event.CreatedAt,
		})
	}
	install := s.toResponse(item)
	if item != nil {
		health, _ := s.CheckInstallHealth(ctx, item)
		install.HealthCheck = health
		install.HealthSummary = summarizeHealthCheck(health)
	}
	return &CorpInstallDetailResponse{
		Install:      install,
		RecentEvents: respEvents,
	}, nil
}

func (s *Service) GetOverview(ctx context.Context) (*DelegatedAppOverviewResponse, error) {
	state, err := s.store.GetRuntimeState(ctx, s.providerApp)
	if err != nil {
		return nil, err
	}
	activeInstallCount, err := s.store.CountActiveCorpInstalls(ctx, s.providerApp)
	if err != nil {
		return nil, err
	}
	events, err := s.store.ListRecentEventLogs(ctx, 10)
	if err != nil {
		return nil, err
	}
	healthCheck, _ := s.CheckHealth(ctx)
	respEvents := make([]*EventLogResponse, 0, len(events))
	for _, event := range events {
		respEvents = append(respEvents, &EventLogResponse{
			ID:         fmt.Sprintf("%d", event.ID),
			CorpID:     event.CorpID,
			InfoType:   event.InfoType,
			RawPayload: event.RawPayload,
			CreatedAt:  event.CreatedAt,
		})
	}
	return &DelegatedAppOverviewResponse{
		ProviderApp:        s.providerApp,
		TemplateConnected:  strings.TrimSpace(state.SuiteTicket) != "",
		LastSuiteTicketAt:  state.SuiteTicketReceivedAt,
		ActiveInstallCount: activeInstallCount,
		HealthCheck:        healthCheck,
		RecentEvents:       respEvents,
	}, nil
}

func (s *Service) CheckHealth(ctx context.Context) (*DelegatedAppHealthCheckResponse, error) {
	state, err := s.store.GetRuntimeState(ctx, s.providerApp)
	if err != nil {
		return nil, err
	}
	result := &DelegatedAppHealthCheckResponse{
		SuiteToken:       &DelegatedAppStepCheck{OK: false, Status: "missing"},
		AuthInfo:         &DelegatedAppStepCheck{OK: false, Status: "missing"},
		CorpToken:        &DelegatedAppStepCheck{OK: false, Status: "missing"},
		LicenseAutoActiv: &DelegatedAppStepCheck{OK: false, Status: "missing"},
	}
	suiteTicket := strings.TrimSpace(state.SuiteTicket)
	if suiteTicket == "" {
		result.SuiteToken.Error = "suite_ticket missing"
		result.AuthInfo.Error = "suite_ticket missing"
		result.CorpToken.Error = "suite_ticket missing"
		result.LicenseAutoActiv.Error = "suite_ticket missing"
		return result, nil
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		msg := err.Error()
		result.SuiteToken.Status = "failed"
		result.SuiteToken.Error = msg
		result.AuthInfo.Status = "blocked"
		result.AuthInfo.Error = msg
		result.CorpToken.Status = "blocked"
		result.CorpToken.Error = msg
		result.LicenseAutoActiv.Error = msg
		return result, nil
	}
	result.SuiteToken.OK = true
	result.SuiteToken.Status = "ok"
	items, err := s.store.ListCorpInstalls(ctx, nil, "")
	if err != nil {
		msg := err.Error()
		result.AuthInfo.Status = "failed"
		result.AuthInfo.Error = msg
		result.CorpToken.Status = "blocked"
		result.CorpToken.Error = msg
		result.LicenseAutoActiv.Error = msg
		return result, nil
	}
	var item *corpInstallRecord
	for _, candidate := range items {
		if candidate == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(candidate.Status), "active") {
			item = candidate
			break
		}
	}
	if item == nil || strings.TrimSpace(item.PermanentCode) == "" || strings.TrimSpace(item.CorpID) == "" {
		result.AuthInfo.Error = "install permanent code missing"
		result.CorpToken.Error = "install permanent code missing"
		result.LicenseAutoActiv.Error = "install permanent code missing"
		return result, nil
	}
	if _, err := s.client.GetAuthInfo(ctx, suiteAccessToken, item.CorpID, item.PermanentCode); err != nil {
		msg := err.Error()
		result.AuthInfo.Status = "failed"
		result.AuthInfo.Error = msg
		result.CorpToken.Status = "blocked"
		result.CorpToken.Error = msg
		result.LicenseAutoActiv.Error = msg
		return result, nil
	}
	result.AuthInfo.OK = true
	result.AuthInfo.Status = "ok"
	if _, _, err := s.client.GetDelegatedCorpAccessToken(ctx, suiteAccessToken, item.CorpID, item.PermanentCode); err != nil {
		msg := err.Error()
		result.CorpToken.Status = "failed"
		result.CorpToken.Error = msg
		return result, nil
	}
	result.CorpToken.OK = true
	result.CorpToken.Status = "ok"
	s.checkLicenseAutoActivation(ctx, item, result.LicenseAutoActiv)
	return result, nil
}

func (s *Service) CheckInstallHealth(ctx context.Context, item *corpInstallRecord) (*DelegatedAppHealthCheckResponse, error) {
	result := &DelegatedAppHealthCheckResponse{
		SuiteToken:       &DelegatedAppStepCheck{OK: false, Status: "missing"},
		AuthInfo:         &DelegatedAppStepCheck{OK: false, Status: "missing"},
		CorpToken:        &DelegatedAppStepCheck{OK: false, Status: "missing"},
		LicenseAutoActiv: &DelegatedAppStepCheck{OK: false, Status: "missing"},
	}
	if item == nil {
		result.SuiteToken.Error = "install missing"
		result.AuthInfo.Error = "install missing"
		result.CorpToken.Error = "install missing"
		result.LicenseAutoActiv.Error = "install missing"
		return result, nil
	}
	state, err := s.store.GetRuntimeState(ctx, s.providerApp)
	if err != nil {
		return nil, err
	}
	suiteTicket := strings.TrimSpace(state.SuiteTicket)
	if suiteTicket == "" {
		result.SuiteToken.Error = "suite_ticket missing"
		result.AuthInfo.Error = "suite_ticket missing"
		result.CorpToken.Error = "suite_ticket missing"
		result.LicenseAutoActiv.Error = "suite_ticket missing"
		return result, nil
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		msg := err.Error()
		result.SuiteToken.Status = "failed"
		result.SuiteToken.Error = msg
		result.AuthInfo.Status = "blocked"
		result.AuthInfo.Error = msg
		result.CorpToken.Status = "blocked"
		result.CorpToken.Error = msg
		result.LicenseAutoActiv.Error = msg
		return result, nil
	}
	result.SuiteToken.OK = true
	result.SuiteToken.Status = "ok"
	if strings.TrimSpace(item.PermanentCode) == "" || strings.TrimSpace(item.CorpID) == "" {
		result.AuthInfo.Error = "install permanent code missing"
		result.CorpToken.Error = "install permanent code missing"
		result.LicenseAutoActiv.Error = "install permanent code missing"
		return result, nil
	}
	if _, err := s.client.GetAuthInfo(ctx, suiteAccessToken, item.CorpID, item.PermanentCode); err != nil {
		msg := err.Error()
		result.AuthInfo.Status = "failed"
		result.AuthInfo.Error = msg
		result.CorpToken.Status = "blocked"
		result.CorpToken.Error = msg
		result.LicenseAutoActiv.Error = msg
		return result, nil
	}
	result.AuthInfo.OK = true
	result.AuthInfo.Status = "ok"
	if _, _, err := s.client.GetDelegatedCorpAccessToken(ctx, suiteAccessToken, item.CorpID, item.PermanentCode); err != nil {
		msg := err.Error()
		result.CorpToken.Status = "failed"
		result.CorpToken.Error = msg
		return result, nil
	}
	result.CorpToken.OK = true
	result.CorpToken.Status = "ok"
	s.checkLicenseAutoActivation(ctx, item, result.LicenseAutoActiv)
	return result, nil
}

func summarizeHealthCheck(health *DelegatedAppHealthCheckResponse) string {
	if health == nil {
		return "未检查"
	}
	if health.CorpToken != nil && health.CorpToken.OK {
		if health.LicenseAutoActiv != nil && !health.LicenseAutoActiv.OK {
			return "许可自动激活异常"
		}
		return "可发消息"
	}
	if health.CorpToken != nil && strings.TrimSpace(health.CorpToken.Error) != "" {
		return "企业凭证不可用"
	}
	if health.AuthInfo != nil && !health.AuthInfo.OK {
		return "授权未完成"
	}
	if health.SuiteToken != nil && !health.SuiteToken.OK {
		return "模板票据异常"
	}
	return "待检查"
}

func (s *Service) BindCorpInstallTenant(ctx context.Context, req CorpInstallBindRequest) (*CorpInstallResponse, error) {
	if strings.TrimSpace(req.ProviderApp) == "" {
		return nil, fmt.Errorf("provider_app is required")
	}
	if strings.TrimSpace(req.CorpID) == "" {
		return nil, fmt.Errorf("corp_id is required")
	}
	if req.TenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if err := s.store.BindCorpInstallTenant(ctx, req.ProviderApp, req.CorpID, req.TenantID); err != nil {
		return nil, err
	}
	item, err := s.store.GetCorpInstallByCorpID(ctx, req.ProviderApp, req.CorpID)
	if err != nil {
		return nil, err
	}
	return s.toResponse(item), nil
}

func (s *Service) ResolveLoginEntryURL(ctx context.Context, corpID string) (string, error) {
	corpID = strings.TrimSpace(corpID)
	if corpID == "" {
		return "", fmt.Errorf("corp_id is required")
	}
	install, err := s.store.GetCorpInstallByCorpID(ctx, s.providerApp, corpID)
	if err != nil {
		return "", err
	}
	target := defaultLaunchURL("")
	if install.AgentID > 0 {
		target += "?mode=delegated_app&provider_app=" + s.providerApp + "&corp_id=" + install.CorpID + fmt.Sprintf("&agent_id=%d", install.AgentID)
	} else {
		target += "?mode=delegated_app&provider_app=" + s.providerApp + "&corp_id=" + install.CorpID
	}
	return target, nil
}

func (s *Service) LoginWithOAuth(ctx context.Context, code, corpID string) (*OAuthLoginResponse, error) {
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("code is required")
	}
	corpID = strings.TrimSpace(corpID)
	if corpID == "" {
		return nil, fmt.Errorf("missing corp_id")
	}
	install, err := s.store.GetCorpInstallByCorpID(ctx, s.providerApp, corpID)
	if err != nil {
		return nil, err
	}
	slog.Info("delegated oauth login start",
		"corp_id", corpID,
		"provider_app", s.providerApp,
	)
	if err := s.enableLicenseAutoActivation(ctx, corpID); err != nil {
		// Activation is asynchronous and must not prevent us from returning the
		// normal WeCom error, which the handler maps to a stable business code.
		slog.Warn("delegated oauth license auto activation request failed",
			"corp_id", corpID,
			"error", err,
		)
	}
	corpAccessToken, _, err := s.resolveCorpAccessToken(ctx, install)
	if err != nil {
		slog.Warn("delegated oauth login corp access token failed",
			"corp_id", corpID,
			"error", err,
		)
		return nil, err
	}
	userInfo, err := s.client.GetCorpUserInfo(ctx, corpAccessToken, code)
	if err != nil {
		slog.Warn("delegated oauth login get corp userinfo failed",
			"corp_id", corpID,
			"error", err,
		)
		return nil, err
	}
	slog.Info("delegated oauth login got corp userinfo",
		"corp_id", corpID,
		"wecom_user_id", strings.TrimSpace(userInfo.UserID),
		"has_user_ticket", strings.TrimSpace(userInfo.UserTicket) != "",
	)
	return s.completeOAuthLogin(ctx, install, corpAccessToken, userInfo)
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

	tokenCache := make(map[string]string)
	installCache := make(map[string]*corpInstallRecord)
	licenseActivationCache := make(map[string]error)
	for _, employeeID := range employeeIDs {
		binding, err := s.store.GetActiveBindingByEmployeeID(ctx, s.providerApp, employeeID)
		if err != nil {
			return nil, err
		}
		perEmployeeKey := fmt.Sprintf("%s:%d", strings.TrimSpace(req.DedupeKey), employeeID)
		if binding == nil || strings.TrimSpace(binding.CorpID) == "" || strings.TrimSpace(binding.WeComUserID) == "" || binding.AgentID <= 0 {
			_, inserted, insertErr := s.store.InsertMessageLog(ctx, messageLogRecord{
				ProviderApp:    s.providerApp,
				EmployeeID:     employeeID,
				MessageScene:   req.MessageScene,
				DedupeKey:      perEmployeeKey,
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
		logID, inserted, err := s.store.InsertMessageLog(ctx, messageLogRecord{
			ProviderApp:    s.providerApp,
			CorpID:         binding.CorpID,
			TenantID:       binding.TenantID,
			EmployeeID:     employeeID,
			WeComUserID:    binding.WeComUserID,
			MessageScene:   req.MessageScene,
			DedupeKey:      perEmployeeKey,
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

		install, ok := installCache[binding.CorpID]
		if !ok {
			install, err = s.store.GetCorpInstallByCorpID(ctx, s.providerApp, binding.CorpID)
			if err != nil {
				_ = s.store.UpdateMessageLogStatus(ctx, logID, "failed", err.Error(), "")
				resp.Failed++
				continue
			}
			installCache[binding.CorpID] = install
		}
		activationErr, activationChecked := licenseActivationCache[binding.CorpID]
		if !activationChecked {
			activationErr = s.enableLicenseAutoActivation(ctx, binding.CorpID)
			licenseActivationCache[binding.CorpID] = activationErr
		}
		if activationErr != nil {
			slog.Warn("delegated message license auto activation request failed",
				"corp_id", binding.CorpID,
				"employee_id", employeeID,
				"error", activationErr,
			)
		}

		corpToken, ok := tokenCache[binding.CorpID]
		if !ok {
			corpToken, _, err = s.resolveCorpAccessToken(ctx, install)
			if err != nil {
				_ = s.store.UpdateMessageLogStatus(ctx, logID, "failed", err.Error(), "")
				resp.Failed++
				continue
			}
			tokenCache[binding.CorpID] = corpToken
		}

		sendResp, err := s.client.SendTextCardMessage(ctx, corpToken, binding.AgentID, binding.WeComUserID, req.Title, req.Content, targetURL, req.ButtonText)
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

func (s *Service) SendOpportunityAlert(ctx context.Context, req opportunityalert.MessageSendRequest) error {
	if !s.IsEnabled() {
		return nil
	}
	_, err := s.SendInternalMessage(ctx, InternalSendMessageRequest{
		MessageScene: req.MessageScene,
		DedupeKey:    req.DedupeKey,
		EmployeeIDs:  req.EmployeeIDs,
		Title:        req.Title,
		Content:      req.Content,
		TargetURL:    req.TargetURL,
		ButtonText:   req.ButtonText,
		Extra:        req.Extra,
	})
	return err
}

func (s *Service) BindEmployee(ctx context.Context, corpID, wecomUserID string, employeeID int64) error {
	employee, err := s.authStore.GetEmployeeByID(ctx, employeeID)
	if err != nil {
		return err
	}
	return s.store.UpsertUserBinding(ctx, userBindingRecord{
		ProviderApp: s.providerApp,
		CorpID:      strings.TrimSpace(corpID),
		WeComUserID: strings.TrimSpace(wecomUserID),
		EmployeeID:  employee.ID,
		TenantID:    employee.TenantID,
		Source:      "manual_bind",
	})
}

func (s *Service) VerifyCallbackURL(signature, timestamp, nonce, echostr string) (string, error) {
	if strings.TrimSpace(signature) == "" {
		return "", fmt.Errorf("invalid signature")
	}
	receiverIDs := []string{s.providerApp}
	if corpID := strings.TrimSpace(s.providerCorpID); corpID != "" && corpID != s.providerApp {
		receiverIDs = append(receiverIDs, corpID)
	}
	var lastErr error
	for _, receiverID := range receiverIDs {
		crypto, err := newCrypto(s.token, s.encodingAESKey, receiverID)
		if err != nil {
			lastErr = err
			continue
		}
		if !crypto.VerifySignature(signature, timestamp, nonce, echostr) {
			lastErr = fmt.Errorf("invalid signature")
			continue
		}
		plain, err := crypto.Decrypt(echostr)
		if err == nil {
			return plain, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("invalid signature")
}

func (s *Service) HandleCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	crypto, err := newCrypto(s.token, s.encodingAESKey, s.providerApp)
	if err != nil {
		return err
	}
	var envelope encryptedCallbackEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parse callback xml: %w", err)
	}
	if strings.TrimSpace(envelope.Encrypt) == "" {
		return fmt.Errorf("missing encrypted payload")
	}
	if !crypto.VerifySignature(signature, timestamp, nonce, envelope.Encrypt) {
		return fmt.Errorf("invalid signature")
	}
	plain, err := crypto.Decrypt(envelope.Encrypt)
	if err != nil {
		return err
	}
	var event callbackEvent
	if err := xml.Unmarshal([]byte(plain), &event); err != nil {
		return fmt.Errorf("parse decrypted callback: %w", err)
	}
	if strings.TrimSpace(event.SuiteID) != "" && strings.TrimSpace(event.SuiteID) != s.providerApp {
		return fmt.Errorf("suite id mismatch")
	}
	return s.handleCallbackEvent(ctx, plain, &event)
}

func (s *Service) VerifyEnterpriseCallbackURL(signature, timestamp, nonce, echostr string) (string, error) {
	token := firstNonEmpty(s.enterpriseCallbackToken, s.token)
	aesKey := firstNonEmpty(s.enterpriseCallbackAESKey, s.encodingAESKey)
	var lastErr error
	for _, receiverID := range s.enterpriseReceiverIDs(context.Background()) {
		crypto, err := newCrypto(token, aesKey, receiverID)
		if err != nil {
			lastErr = err
			continue
		}
		if !crypto.VerifySignature(signature, timestamp, nonce, echostr) {
			lastErr = fmt.Errorf("invalid signature")
			continue
		}
		plain, err := crypto.Decrypt(echostr)
		if err == nil {
			return plain, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("invalid signature")
}

func (s *Service) HandleEnterpriseCallback(ctx context.Context, signature, timestamp, nonce string, body []byte) error {
	token := firstNonEmpty(s.enterpriseCallbackToken, s.token)
	aesKey := firstNonEmpty(s.enterpriseCallbackAESKey, s.encodingAESKey)
	var envelope encryptedCallbackEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parse callback xml: %w", err)
	}
	if strings.TrimSpace(envelope.Encrypt) == "" {
		return fmt.Errorf("missing encrypted payload")
	}
	var (
		plain             string
		matchedReceiverID string
		lastErr           error
	)
	for _, receiverID := range s.enterpriseReceiverIDs(ctx) {
		crypto, err := newCrypto(token, aesKey, receiverID)
		if err != nil {
			lastErr = err
			continue
		}
		if !crypto.VerifySignature(signature, timestamp, nonce, envelope.Encrypt) {
			lastErr = fmt.Errorf("invalid signature")
			continue
		}
		plain, err = crypto.Decrypt(envelope.Encrypt)
		if err == nil {
			matchedReceiverID = receiverID
			break
		}
		lastErr = err
	}
	if plain == "" {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("invalid signature")
	}
	var event callbackEvent
	if err := xml.Unmarshal([]byte(plain), &event); err != nil {
		return fmt.Errorf("parse decrypted enterprise callback: %w", err)
	}
	infoType := strings.TrimSpace(event.InfoType)
	if infoType == "" {
		infoType = "enterprise_callback"
	}
	corpID := firstNonEmpty(event.AuthCorpID, matchedReceiverID)
	if err := s.store.SaveEventLog(ctx, corpID, infoType, plain); err != nil {
		return err
	}
	if isAutoActivateEvent(infoType) {
		slog.Info("delegated app license auto activation event received",
			"corp_id", corpID,
			"wecom_user_id", firstNonEmpty(event.UserID, event.UserIDAlt),
			"scene", strings.TrimSpace(event.Scene),
			"license_type", strings.TrimSpace(event.LicenseType),
			"license_status", strings.TrimSpace(event.LicenseStatus),
			"active_time", strings.TrimSpace(event.ActiveTime),
			"expire_time", firstNonEmpty(event.ExpireTime, event.LicenseExpireTime),
		)
	}
	return nil
}

func isAutoActivateEvent(infoType string) bool {
	switch strings.ToLower(strings.TrimSpace(infoType)) {
	case "auto_activate", "license_auto_activate", "license_auto_active":
		return true
	default:
		return false
	}
}

func (s *Service) enterpriseReceiverIDs(ctx context.Context) []string {
	ids := []string{s.providerApp, s.providerCorpID}
	if s.store != nil {
		if installs, err := s.store.ListCorpInstalls(ctx, nil, ""); err == nil {
			for _, item := range installs {
				if item != nil {
					ids = append(ids, item.CorpID)
				}
			}
		}
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *Service) handleCallbackEvent(ctx context.Context, plain string, event *callbackEvent) error {
	if event == nil {
		return fmt.Errorf("callback event is required")
	}
	infoType := strings.TrimSpace(event.InfoType)
	corpID := strings.TrimSpace(event.AuthCorpID)
	switch infoType {
	case "suite_ticket":
		if err := s.store.SaveEventLog(ctx, corpID, infoType, plain); err != nil {
			return err
		}
		return s.store.UpsertRuntimeStateSuiteTicket(ctx, s.providerApp, event.SuiteTicket)
	case "create_auth", "change_auth", "enter_agent":
		count, err := s.store.CountEventLogsByPayload(ctx, infoType, plain)
		if err != nil {
			return err
		}
		if err := s.store.SaveEventLog(ctx, corpID, infoType, plain); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		if infoType == "enter_agent" {
			return nil
		}
		if strings.TrimSpace(event.AuthCode) == "" {
			return nil
		}
		if err := s.syncCorpInstall(ctx, corpID, event.AuthCode); err != nil {
			return err
		}
		if deleted, err := s.store.DeleteUserBindingsByCorpID(ctx, s.providerApp, corpID); err != nil {
			return err
		} else if deleted > 0 {
			slog.Info("delegated app user bindings cleared after reinstall",
				"corp_id", corpID,
				"deleted", deleted,
			)
		}
		return nil
	case "cancel_auth":
		if err := s.store.SaveEventLog(ctx, corpID, infoType, plain); err != nil {
			return err
		}
		return s.store.MarkCorpInstallCancelled(ctx, s.providerApp, corpID)
	default:
		return s.store.SaveEventLog(ctx, corpID, infoType, plain)
	}
}

func (s *Service) syncCorpInstall(ctx context.Context, corpID, authCode string) error {
	suiteTicket, err := s.store.GetRuntimeStateSuiteTicket(ctx, s.providerApp)
	if err != nil {
		return err
	}
	suiteAccessToken, _, err := s.client.GetSuiteAccessToken(ctx, suiteTicket)
	if err != nil {
		return err
	}
	permanentResp, err := s.client.GetPermanentCode(ctx, suiteAccessToken, authCode)
	if err != nil {
		return err
	}
	if strings.TrimSpace(corpID) == "" {
		corpID = strings.TrimSpace(permanentResp.AuthCorpInfo.CorpID)
	}
	if strings.TrimSpace(corpID) == "" {
		return fmt.Errorf("missing corp_id")
	}
	authInfo, err := s.client.GetAuthInfo(ctx, suiteAccessToken, corpID, permanentResp.PermanentCode)
	if err != nil {
		return err
	}
	var agentID int64
	if len(authInfo.AuthInfo.Agent) > 0 {
		agentID = authInfo.AuthInfo.Agent[0].AgentID
	}
	if err := s.store.UpsertCorpInstall(ctx, corpInstallRecord{
		ProviderApp:   s.providerApp,
		CorpID:        corpID,
		CorpName:      strings.TrimSpace(authInfo.AuthCorpInfo.CorpName),
		PermanentCode: strings.TrimSpace(permanentResp.PermanentCode),
		AgentID:       agentID,
		Status:        "active",
	}); err != nil {
		return err
	}
	if err := s.enableLicenseAutoActivation(ctx, corpID); err != nil {
		// License setup must not make WeCom reject an otherwise valid install callback.
		// The failure is retained in event logs and exposed by the OPS health check.
		slog.Warn("delegated app license auto activation setup failed",
			"corp_id", corpID,
			"error", err,
		)
		_ = s.store.SaveEventLog(ctx, corpID, "license_auto_activate_failed", err.Error())
	}
	return nil
}

func (s *Service) completeOAuthLogin(ctx context.Context, install *corpInstallRecord, corpAccessToken string, userInfo *userInfo3rdResponse) (*OAuthLoginResponse, error) {
	if install == nil {
		return nil, fmt.Errorf("wecom corp install not found")
	}
	if userInfo == nil || strings.TrimSpace(userInfo.UserID) == "" {
		return nil, fmt.Errorf("wecom returned empty user id")
	}
	slog.Info("delegated oauth user info resolved",
		"corp_id", install.CorpID,
		"wecom_user_id", strings.TrimSpace(userInfo.UserID),
		"open_user_id", strings.TrimSpace(userInfo.OpenUserID),
		"has_user_ticket", strings.TrimSpace(userInfo.UserTicket) != "",
	)
	userDetail, err := s.client.GetUserDetail(ctx, corpAccessToken, userInfo.UserID)
	if (err != nil || strings.TrimSpace(userDetail.Mobile) == "") && strings.TrimSpace(userInfo.UserTicket) != "" {
		userDetail, err = s.client.GetAuthUserDetail(ctx, corpAccessToken, userInfo.UserTicket)
	}
	if err != nil {
		return nil, err
	}
	slog.Info("delegated oauth user detail resolved",
		"corp_id", install.CorpID,
		"wecom_user_id", strings.TrimSpace(userInfo.UserID),
		"name", strings.TrimSpace(userDetail.Name),
		"mobile", strings.TrimSpace(userDetail.Mobile),
		"has_user_ticket", strings.TrimSpace(userInfo.UserTicket) != "",
	)
	profile := &OAuthUserProfile{
		CorpID:      install.CorpID,
		WeComUserID: userInfo.UserID,
		OpenUserID:  userInfo.OpenUserID,
		Name:        userDetail.Name,
		Mobile:      userDetail.Mobile,
		Avatar:      userDetail.Avatar,
	}
	binding, err := s.store.GetUserBinding(ctx, s.providerApp, install.CorpID, userInfo.UserID)
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
			if err := s.store.UpsertUserBinding(ctx, userBindingRecord{
				ProviderApp: s.providerApp,
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
	reason := "企业微信身份已识别，但未匹配到灵策员工手机号"
	if strings.TrimSpace(userDetail.Mobile) == "" {
		reason = "企业微信身份已识别，但未获取到手机号"
	} else if normalizePhone(userDetail.Mobile) != "" {
		reason = fmt.Sprintf("手机号 %s 未匹配到当前租户员工", normalizePhone(userDetail.Mobile))
	}
	return &OAuthLoginResponse{Status: "needs_bind", Reason: reason, Profile: profile}, nil
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

func (s *Service) resolveCorpAccessToken(ctx context.Context, install *corpInstallRecord) (string, int64, error) {
	if install == nil {
		return "", 0, fmt.Errorf("wecom corp install not found")
	}
	return s.client.GetCorpAccessToken(ctx, install.CorpID, install.PermanentCode)
}

func (s *Service) getProviderAccessToken(ctx context.Context) (string, error) {
	state, err := s.store.GetRuntimeState(ctx, s.providerApp)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(state.ProviderAccessToken) != "" && state.ProviderAccessTokenExpiresAt != nil {
		expiresAt, parseErr := time.Parse(time.RFC3339, *state.ProviderAccessTokenExpiresAt)
		if parseErr == nil && expiresAt.After(time.Now().Add(60*time.Second)) {
			return state.ProviderAccessToken, nil
		}
	}
	token, expiresIn, err := s.client.GetProviderAccessToken(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("provider access token is empty")
	}
	if err := s.store.UpsertRuntimeStateProviderAccessToken(ctx, s.providerApp, token, expiresIn); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) enableLicenseAutoActivation(ctx context.Context, corpID string) error {
	providerAccessToken, err := s.getProviderAccessToken(ctx)
	if err != nil {
		return err
	}
	return s.client.SetLicenseAutoActiveStatus(ctx, providerAccessToken, strings.TrimSpace(corpID))
}

func (s *Service) checkLicenseAutoActivation(ctx context.Context, item *corpInstallRecord, result *DelegatedAppStepCheck) {
	if result == nil {
		return
	}
	if item == nil || strings.TrimSpace(item.CorpID) == "" {
		result.Error = "install corp_id missing"
		return
	}
	providerAccessToken, err := s.getProviderAccessToken(ctx)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return
	}
	license, err := s.client.GetAppLicenseInfo(ctx, providerAccessToken, item.CorpID)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return
	}
	result.LicenseStatus = &license.Status
	if license.CheckTime > 0 {
		result.LicenseCheckTime = &license.CheckTime
	}
	if isReadyLicenseStatus(license.Status) {
		result.OK = true
		result.Status = "ready"
		return
	}
	result.Status = "unavailable"
	result.Error = "接口调用许可未开通或无可用许可"
}

func isReadyLicenseStatus(status int) bool {
	// WeCom reports 1 for trial and 2 for a purchased license.
	return status == LicenseStatusTrial || status == LicenseStatusPurchased
}

func (s *Service) toResponse(item *corpInstallRecord) *CorpInstallResponse {
	if item == nil {
		return nil
	}
	launchURL := s.launchURL
	if strings.TrimSpace(item.CorpID) != "" {
		if strings.Contains(launchURL, "?") {
			launchURL += "&corp_id=" + item.CorpID
		} else {
			launchURL += "?corp_id=" + item.CorpID
		}
	}
	return &CorpInstallResponse{
		ID:             fmt.Sprintf("%d", item.ID),
		ProviderApp:    item.ProviderApp,
		TenantID:       item.TenantID,
		CorpID:         item.CorpID,
		CorpName:       item.CorpName,
		AgentID:        item.AgentID,
		Status:         item.Status,
		HasPermanent:   strings.TrimSpace(item.PermanentCode) != "",
		LaunchURL:      launchURL,
		TrustedDomain:  s.trustedDomain,
		CallbackURL:    s.callbackURL,
		Token:          firstNonEmpty(s.enterpriseCallbackToken, s.token),
		EncodingAESKey: firstNonEmpty(s.enterpriseCallbackAESKey, s.encodingAESKey),
		UpdatedAt:      item.UpdatedAt,
		CancelledAt:    item.CancelledAt,
	}
}

func hostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	parts := strings.Split(raw, "/")
	return strings.TrimSpace(parts[0])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func uniqueInt64s(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
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
	return out
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func attachWeComEntryParams(targetURL, corpID string, agentID int64) string {
	target := strings.TrimSpace(targetURL)
	if target == "" || strings.TrimSpace(corpID) == "" || agentID <= 0 {
		return target
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	if strings.Contains(target, "corp_id=") || strings.Contains(target, "agent_id=") {
		return target
	}
	return fmt.Sprintf("%s%scorp_id=%s&agent_id=%d", target, separator, corpID, agentID)
}

type client struct {
	baseURL        string
	suiteID        string
	suiteSecret    string
	providerCorpID string
	providerSecret string
}

func newClient(baseURL, suiteID, suiteSecret, providerCorpID, providerSecret string) *client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://qyapi.weixin.qq.com"
	}
	return &client{
		baseURL:        baseURL,
		suiteID:        strings.TrimSpace(suiteID),
		suiteSecret:    strings.TrimSpace(suiteSecret),
		providerCorpID: strings.TrimSpace(providerCorpID),
		providerSecret: strings.TrimSpace(providerSecret),
	}
}

func (c *client) GetProviderAccessToken(ctx context.Context) (string, int64, error) {
	if strings.TrimSpace(c.providerCorpID) == "" || strings.TrimSpace(c.providerSecret) == "" {
		return "", 0, fmt.Errorf("wecom provider credentials are not configured")
	}
	type response struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"provider_access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	var resp response
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/service/get_provider_token", map[string]string{
		"corpid":          c.providerCorpID,
		"provider_secret": c.providerSecret,
	}, &resp); err != nil {
		return "", 0, err
	}
	return resp.AccessToken, resp.ExpiresIn, nil
}

func (c *client) SetLicenseAutoActiveStatus(ctx context.Context, providerAccessToken, corpID string) error {
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	return postJSON(ctx, c.baseURL+"/cgi-bin/license/set_auto_active_status?provider_access_token="+url.QueryEscape(providerAccessToken), map[string]any{
		"auth_corpid": corpID,
		"suite_id":    c.suiteID,
		"auto_active": 1,
	}, &resp)
}

type appLicenseInfo struct {
	Status    int   `json:"license_status"`
	CheckTime int64 `json:"license_check_time"`
}

func (c *client) GetAppLicenseInfo(ctx context.Context, providerAccessToken, corpID string) (appLicenseInfo, error) {
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		appLicenseInfo
	}
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/license/get_app_license_info?provider_access_token="+url.QueryEscape(providerAccessToken), map[string]string{
		"corpid":   corpID,
		"suite_id": c.suiteID,
	}, &resp); err != nil {
		return appLicenseInfo{}, err
	}
	return resp.appLicenseInfo, nil
}

func (c *client) GetSuiteAccessToken(ctx context.Context, suiteTicket string) (string, int64, error) {
	type response struct {
		ErrCode          int    `json:"errcode"`
		ErrMsg           string `json:"errmsg"`
		SuiteAccessToken string `json:"suite_access_token"`
		ExpiresIn        int64  `json:"expires_in"`
	}
	var resp response
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/service/get_suite_token", map[string]string{
		"suite_id":     c.suiteID,
		"suite_secret": c.suiteSecret,
		"suite_ticket": suiteTicket,
	}, &resp); err != nil {
		return "", 0, err
	}
	return resp.SuiteAccessToken, resp.ExpiresIn, nil
}

func (c *client) GetPermanentCode(ctx context.Context, suiteAccessToken, authCode string) (*struct {
	AuthCorpInfo struct {
		CorpID string `json:"corpid"`
	} `json:"auth_corp_info"`
	PermanentCode string `json:"permanent_code"`
}, error) {
	type response struct {
		ErrCode      int    `json:"errcode"`
		ErrMsg       string `json:"errmsg"`
		AuthCorpInfo struct {
			CorpID string `json:"corpid"`
		} `json:"auth_corp_info"`
		PermanentCode string `json:"permanent_code"`
	}
	var resp response
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/service/get_permanent_code?suite_access_token="+suiteAccessToken, map[string]string{
		"auth_code": authCode,
	}, &resp); err != nil {
		return nil, err
	}
	return &struct {
		AuthCorpInfo struct {
			CorpID string `json:"corpid"`
		} `json:"auth_corp_info"`
		PermanentCode string `json:"permanent_code"`
	}{
		AuthCorpInfo:  resp.AuthCorpInfo,
		PermanentCode: resp.PermanentCode,
	}, nil
}

func (c *client) GetAuthInfo(ctx context.Context, suiteAccessToken, corpID, permanentCode string) (*struct {
	AuthCorpInfo struct {
		CorpID   string `json:"corpid"`
		CorpName string `json:"corp_name"`
	} `json:"auth_corp_info"`
	AuthInfo struct {
		Agent []struct {
			AgentID int64 `json:"agentid"`
		} `json:"agent"`
	} `json:"auth_info"`
}, error) {
	type response struct {
		ErrCode      int    `json:"errcode"`
		ErrMsg       string `json:"errmsg"`
		AuthCorpInfo struct {
			CorpID   string `json:"corpid"`
			CorpName string `json:"corp_name"`
		} `json:"auth_corp_info"`
		AuthInfo struct {
			Agent []struct {
				AgentID int64 `json:"agentid"`
			} `json:"agent"`
		} `json:"auth_info"`
	}
	var resp response
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/service/get_auth_info?suite_access_token="+suiteAccessToken, map[string]string{
		"auth_corpid":    corpID,
		"permanent_code": permanentCode,
	}, &resp); err != nil {
		return nil, err
	}
	return &struct {
		AuthCorpInfo struct {
			CorpID   string `json:"corpid"`
			CorpName string `json:"corp_name"`
		} `json:"auth_corp_info"`
		AuthInfo struct {
			Agent []struct {
				AgentID int64 `json:"agentid"`
			} `json:"agent"`
		} `json:"auth_info"`
	}{
		AuthCorpInfo: resp.AuthCorpInfo,
		AuthInfo:     resp.AuthInfo,
	}, nil
}

func (c *client) GetUserInfo3rd(ctx context.Context, suiteAccessToken, code string) (*userInfo3rdResponse, error) {
	var resp userInfo3rdResponse
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/service/getuserinfo3rd?suite_access_token="+suiteAccessToken, map[string]string{
		"code": code,
	}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *client) GetCorpAccessToken(ctx context.Context, corpID, corpSecret string) (string, int64, error) {
	type response struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	var resp response
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/gettoken", map[string]string{
		"corpid":     corpID,
		"corpsecret": corpSecret,
	}, &resp); err != nil {
		return "", 0, err
	}
	return resp.AccessToken, resp.ExpiresIn, nil
}

func (c *client) GetDelegatedCorpAccessToken(ctx context.Context, suiteAccessToken, corpID, permanentCode string) (string, int64, error) {
	type response struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	var resp response
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/service/get_corp_token?suite_access_token="+suiteAccessToken, map[string]string{
		"auth_corpid":    corpID,
		"permanent_code": permanentCode,
	}, &resp); err != nil {
		return "", 0, err
	}
	return resp.AccessToken, resp.ExpiresIn, nil
}

func (c *client) GetCorpUserInfo(ctx context.Context, corpAccessToken, code string) (*userInfo3rdResponse, error) {
	var resp userInfo3rdResponse
	if err := getJSON(ctx, c.baseURL+"/cgi-bin/auth/getuserinfo?access_token="+corpAccessToken+"&code="+code, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *client) GetUserDetail(ctx context.Context, corpAccessToken, userID string) (*userDetailResponse, error) {
	var resp userDetailResponse
	if err := getJSON(ctx, c.baseURL+"/cgi-bin/user/get?access_token="+corpAccessToken+"&userid="+userID, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *client) GetAuthUserDetail(ctx context.Context, corpAccessToken, userTicket string) (*userDetailResponse, error) {
	var resp userDetailResponse
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/auth/getuserdetail?access_token="+corpAccessToken, map[string]string{
		"user_ticket": userTicket,
	}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type sendMessageResponse struct {
	ErrCode      int    `json:"errcode"`
	ErrMsg       string `json:"errmsg"`
	InvalidUser  string `json:"invaliduser,omitempty"`
	InvalidParty string `json:"invalidparty,omitempty"`
	InvalidTag   string `json:"invalidtag,omitempty"`
	ResponseCode string `json:"response_code,omitempty"`
}

func (c *client) SendTextCardMessage(ctx context.Context, corpAccessToken string, agentID int64, toUser, title, description, targetURL, buttonText string) (*sendMessageResponse, error) {
	var resp sendMessageResponse
	payload := map[string]any{
		"touser":  toUser,
		"msgtype": "textcard",
		"agentid": agentID,
		"textcard": map[string]any{
			"title":       title,
			"description": description,
			"url":         targetURL,
			"btntxt":      buttonText,
		},
		"safe": 0,
	}
	if strings.TrimSpace(buttonText) == "" {
		delete(payload["textcard"].(map[string]any), "btntxt")
	}
	if err := postJSON(ctx, c.baseURL+"/cgi-bin/message/send?access_token="+corpAccessToken, payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type wecomAPIError struct {
	Code int
	Msg  string
}

func (e *wecomAPIError) Error() string {
	if strings.TrimSpace(e.Msg) == "" {
		return fmt.Sprintf("wecom api error: %d", e.Code)
	}
	return fmt.Sprintf("wecom api error: %d %s", e.Code, e.Msg)
}

func (e *wecomAPIError) IsLicenseUnavailable() bool {
	switch e.Code {
	case 48002, 701000, 701001, 701002:
		return true
	default:
		message := strings.ToLower(e.Msg)
		return strings.Contains(message, "接口调用许可") || strings.Contains(message, "api forbidden")
	}
}

func postJSON(ctx context.Context, targetURL string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom api http error: %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	var apiErr struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.ErrCode != 0 {
		return &wecomAPIError{Code: apiErr.ErrCode, Msg: apiErr.ErrMsg}
	}
	return nil
}

func getJSON(ctx context.Context, targetURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom api http error: %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	var apiErr struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.ErrCode != 0 {
		return &wecomAPIError{Code: apiErr.ErrCode, Msg: apiErr.ErrMsg}
	}
	return nil
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type crypto struct {
	token      string
	receiverID string
	aesKey     []byte
}

func newCrypto(token, encodingAESKey, receiverID string) (*crypto, error) {
	decoded, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, fmt.Errorf("decode encoding aes key: %w", err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("invalid encoding aes key length: %d", len(decoded))
	}
	return &crypto{
		token:      strings.TrimSpace(token),
		receiverID: strings.TrimSpace(receiverID),
		aesKey:     decoded,
	}, nil
}

func (c *crypto) VerifySignature(signature, timestamp, nonce, encrypted string) bool {
	parts := []string{c.token, timestamp, nonce, encrypted}
	sort.Strings(parts)
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
	}
	return fmt.Sprintf("%x", h.Sum(nil)) == signature
}

func (c *crypto) Decrypt(encrypted string) (string, error) {
	cipherText, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(cipherText) == 0 || len(cipherText)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length")
	}
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	plain := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, c.aesKey[:aes.BlockSize]).CryptBlocks(plain, cipherText)
	plain, err = pkcs7Unpad(plain, 32)
	if err != nil {
		return "", err
	}
	if len(plain) < 20 {
		return "", fmt.Errorf("plaintext too short")
	}
	msgLen := int(binary.BigEndian.Uint32(plain[16:20]))
	if msgLen < 0 || len(plain) < 20+msgLen {
		return "", fmt.Errorf("invalid message length")
	}
	if c.receiverID != "" {
		receivedID := string(plain[20+msgLen:])
		if receivedID != c.receiverID {
			return "", fmt.Errorf("receiver id mismatch")
		}
	}
	return string(plain[20 : 20+msgLen]), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 data size")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	if !bytes.Equal(bytes.Repeat([]byte{byte(padding)}, padding), data[len(data)-padding:]) {
		return nil, fmt.Errorf("invalid pkcs7 padding content")
	}
	return data[:len(data)-padding], nil
}

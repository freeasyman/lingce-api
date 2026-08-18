package delegated

import (
	"context"
	"net/http"
	"strings"

	internalauth "github.com/freeasyman/lingce-api/internal/auth"
	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/opportunityalert"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
	handler *Handler
	service *Service
}

func NewModule(pool *pgxpool.Pool, cfg config.WeComConfig, jwtSecret string, jwtExpiryHours int) *Module {
	store := NewStore(pool)
	authStore := internalauth.NewStore(pool)
	service := NewService(store, authStore, cfg, jwtSecret, jwtExpiryHours)
	handler := NewHandler(service)
	return &Module{handler: handler, service: service}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret string, pool *pgxpool.Pool, internalToken string) {
	if m == nil || m.handler == nil {
		return
	}
	m.handler.RegisterRoutes(mux, jwtSecret, pool, internalToken)
}

type opportunityAlertSender struct {
	service *Service
}

func (s *opportunityAlertSender) IsEnabled() bool {
	return s != nil && s.service != nil && s.service.IsEnabled()
}

func (s *opportunityAlertSender) SendInternalMessage(ctx context.Context, req opportunityalert.MessageSendRequest) error {
	if s == nil || s.service == nil {
		return nil
	}
	return s.service.SendOpportunityAlert(ctx, req)
}

func (m *Module) MessageSender() opportunityalert.MessageSender {
	if m == nil || m.service == nil {
		return opportunityalert.NewNoopMessageSender()
	}
	return &opportunityAlertSender{service: m.service}
}

func defaultLaunchURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://employee.khgl.xyz"
	}
	return base + "/login/delegated-app"
}

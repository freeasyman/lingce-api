package emrtemplate

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/compliance"
	"github.com/freeasyman/lingce-api/internal/tenancy"
)

type Service struct {
	store             *Store
	complianceService *compliance.Service
}

func NewService(store *Store, complianceService *compliance.Service) *Service {
	return &Service{store: store, complianceService: complianceService}
}

func (s *Service) ensureRuleSeeds(ctx context.Context) error {
	if s.complianceService == nil {
		return nil
	}
	return s.complianceService.EnsureBuiltinRules(ctx)
}

func (s *Service) ListTemplates(ctx context.Context, tenantID int64) ([]*TemplateListItemResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := s.ensureRuleSeeds(ctx); err != nil {
		return nil, err
	}
	if err := s.store.EnsureSeedTemplates(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.store.ListTemplates(ctx, tenantID)
}

func (s *Service) GetTemplate(ctx context.Context, tenantID, templateID int64) (*TemplateDetailResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("template_id", templateID); err != nil {
		return nil, err
	}
	if err := s.ensureRuleSeeds(ctx); err != nil {
		return nil, err
	}
	if err := s.store.EnsureSeedTemplates(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.store.GetTemplate(ctx, tenantID, templateID)
}

func (s *Service) CreateTemplate(ctx context.Context, tenantID, actorID int64, req SaveTemplateRequest) (*TemplateDetailResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("actor_id", actorID); err != nil {
		return nil, err
	}
	if err := validateTemplateRequest(req); err != nil {
		return nil, err
	}
	return s.store.CreateTemplate(ctx, tenantID, actorID, req)
}

func (s *Service) UpdateTemplate(ctx context.Context, tenantID, templateID, actorID int64, req SaveTemplateRequest) (*TemplateDetailResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("template_id", templateID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("actor_id", actorID); err != nil {
		return nil, err
	}
	if err := validateTemplateRequest(req); err != nil {
		return nil, err
	}
	return s.store.UpdateTemplate(ctx, tenantID, templateID, actorID, req)
}

func (s *Service) SaveBindings(ctx context.Context, tenantID, templateID, actorID int64, req SaveBindingsRequest) ([]*BindingItemResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("template_id", templateID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("actor_id", actorID); err != nil {
		return nil, err
	}
	for _, item := range req.Items {
		if strings.TrimSpace(item.RuleID) == "" && strings.TrimSpace(item.RuleCode) == "" {
			return nil, fmt.Errorf("rule_id or rule_code is required")
		}
		if !isValidRuleScope(item.RuleScope) {
			return nil, fmt.Errorf("invalid rule_scope")
		}
	}
	return s.store.ReplaceBindings(ctx, tenantID, templateID, actorID, req.Items)
}

func validateTemplateRequest(req SaveTemplateRequest) error {
	if strings.TrimSpace(req.Code) == "" {
		return fmt.Errorf("code is required")
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(req.ShortName) == "" {
		return fmt.Errorf("short_name is required")
	}
	if req.Status != "enabled" && req.Status != "disabled" {
		return fmt.Errorf("invalid status")
	}
	if req.SchemaJSON == nil {
		req.SchemaJSON = map[string]any{}
	}
	return nil
}

func isValidRuleScope(value string) bool {
	return value == "common" || value == "specialty"
}

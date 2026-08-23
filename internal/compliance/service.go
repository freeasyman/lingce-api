package compliance

import (
	"context"
	"fmt"
	"strings"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) ListRules(ctx context.Context, tenantID int64, scope string) ([]*Rule, error) {
	return s.store.ListRules(ctx, tenantID, scope)
}

func (s *Service) GetRule(ctx context.Context, tenantID int64, id string) (*Rule, error) {
	return s.store.GetRule(ctx, tenantID, id)
}

func (s *Service) CreateRule(ctx context.Context, tenantID int64, req CreateRuleRequest) (*Rule, error) {
	rule := &Rule{
		ID:              newRuleID(),
		TenantID:        tenantID,
		Code:            strings.TrimSpace(req.Code),
		Name:            strings.TrimSpace(req.Name),
		Category:        strings.TrimSpace(req.Category),
		Scope:           strings.TrimSpace(req.Scope),
		Enabled:         true,
		Severity:        strings.TrimSpace(req.Severity),
		TriggerType:     strings.TrimSpace(req.TriggerType),
		Conditions:      req.Conditions,
		Description:     req.Description,
		LegalBasis:      req.LegalBasis,
		SuggestedScript: req.SuggestedScript,
		Examples:        req.Examples,
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if err := validateRule(rule); err != nil {
		return nil, err
	}
	return s.store.CreateRule(ctx, tenantID, rule)
}

func (s *Service) UpdateRule(ctx context.Context, tenantID int64, id string, req UpdateRuleRequest) (*Rule, error) {
	if req.Scope != nil {
		*req.Scope = strings.TrimSpace(*req.Scope)
		if !isValidScope(*req.Scope) {
			return nil, fmt.Errorf("invalid scope")
		}
	}
	if req.Severity != nil && !isValidSeverity(*req.Severity) {
		return nil, fmt.Errorf("invalid severity")
	}
	if req.TriggerType != nil && !isValidTriggerType(*req.TriggerType) {
		return nil, fmt.Errorf("invalid triggerType")
	}
	return s.store.UpdateRule(ctx, tenantID, id, req)
}

func (s *Service) DeleteRule(ctx context.Context, tenantID int64, id string) error {
	return s.store.DeleteRule(ctx, tenantID, id)
}

func (s *Service) RestoreBuiltinRules(ctx context.Context) ([]*Rule, error) {
	return s.store.RestoreBuiltinRules(ctx)
}

func (s *Service) EnsureBuiltinRules(ctx context.Context) error {
	return s.store.EnsureBuiltinRules(ctx)
}

func validateRule(rule *Rule) error {
	if rule.Code == "" {
		return fmt.Errorf("code is required")
	}
	if rule.Name == "" {
		return fmt.Errorf("name is required")
	}
	if rule.Category == "" {
		return fmt.Errorf("category is required")
	}
	if !isValidScope(rule.Scope) {
		return fmt.Errorf("invalid scope")
	}
	if !isValidSeverity(rule.Severity) {
		return fmt.Errorf("invalid severity")
	}
	if !isValidTriggerType(rule.TriggerType) {
		return fmt.Errorf("invalid triggerType")
	}
	if rule.Conditions == nil {
		rule.Conditions = JSONMap{}
	}
	return nil
}

func isValidScope(value string) bool {
	return value == "communication" || value == "content" || value == "emr"
}

func isValidSeverity(value string) bool {
	return value == "critical" || value == "high" || value == "medium" || value == "low"
}

func isValidTriggerType(value string) bool {
	return value == "keywords" || value == "semantic" || value == "pattern"
}

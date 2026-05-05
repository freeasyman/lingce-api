package knowledge

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

func (s *Service) List(ctx context.Context, req ListRequest) ([]*KnowledgeItem, int, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	return s.store.List(ctx, req)
}

func (s *Service) GetByID(ctx context.Context, id int64) (*KnowledgeItem, error) {
	return s.store.GetByID(ctx, id)
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*KnowledgeItem, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}
	return s.store.Create(ctx, req)
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*KnowledgeItem, error) {
	if req.Scope != nil {
		if !isValidScope(*req.Scope) {
			return nil, fmt.Errorf("invalid scope: %s", *req.Scope)
		}
	}
	if req.Category != nil {
		if !isValidCategory(*req.Category) {
			return nil, fmt.Errorf("invalid category: %s", *req.Category)
		}
	}
	return s.store.Update(ctx, id, req)
}

func (s *Service) UpdateStatus(ctx context.Context, id int64, status string) error {
	if !isValidStatus(status) {
		return fmt.Errorf("invalid status: %s", status)
	}
	return s.store.UpdateStatus(ctx, id, status)
}

func (s *Service) BatchUpdateStatus(ctx context.Context, ids []int64, status string) (int64, error) {
	if !isValidStatus(status) {
		return 0, fmt.Errorf("invalid status: %s", status)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > 100 {
		return 0, fmt.Errorf("batch size exceeds limit (max 100)")
	}
	return s.store.BatchUpdateStatus(ctx, ids, status)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.store.Delete(ctx, id)
}

// ── Validation ──

func validateCreate(req CreateRequest) error {
	if strings.TrimSpace(req.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if strings.TrimSpace(req.Content) == "" {
		return fmt.Errorf("content is required")
	}
	if !isValidScope(req.Scope) {
		return fmt.Errorf("invalid scope: %s", req.Scope)
	}
	if !isValidCategory(req.Category) {
		return fmt.Errorf("invalid category: %s", req.Category)
	}
	if !isValidSourceType(req.SourceType) {
		return fmt.Errorf("invalid source_type: %s", req.SourceType)
	}
	return nil
}

func isValidScope(s string) bool {
	switch s {
	case ScopeFrontdesk, ScopeDoctor, ScopeConsultant, ScopeCommon:
		return true
	}
	return false
}

func isValidCategory(c string) bool {
	switch c {
	case CategoryService, CategoryProduct, CategoryScript, CategoryCompliance, CategoryTemporal, CategoryFAQ:
		return true
	}
	return false
}

func isValidStatus(s string) bool {
	switch s {
	case StatusDraft, StatusActive, StatusArchived:
		return true
	}
	return false
}

func isValidSourceType(s string) bool {
	switch s {
	case SourceManual, SourceRecording, SourceDocument:
		return true
	}
	return false
}

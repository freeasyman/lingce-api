package emrtemplate

import (
	"context"
	"fmt"
	"strings"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service                     { return &Service{store: store} }
func (s *Service) EnsureBuiltin(ctx context.Context) error { return s.store.EnsureBuiltin(ctx) }
func (s *Service) List(ctx context.Context, tenantID int64) ([]*Template, error) {
	return s.store.List(ctx, tenantID)
}
func (s *Service) Get(ctx context.Context, tenantID int64, id string) (*TemplateDetail, error) {
	return s.store.Get(ctx, tenantID, id)
}
func (s *Service) ListVersions(ctx context.Context, tenantID int64, templateID string) ([]*TemplateVersion, error) {
	return s.store.ListVersions(ctx, tenantID, templateID)
}
func (s *Service) GetVersion(ctx context.Context, tenantID int64, versionID string) (*TemplateVersion, error) {
	return s.store.GetVersion(ctx, tenantID, versionID)
}
func (s *Service) Create(ctx context.Context, tenantID, actorID int64, req SaveTemplateRequest) (*TemplateDetail, error) {
	normalizeTemplate(&req)
	if req.Code == "" || req.Name == "" {
		return nil, fmt.Errorf("code and name are required")
	}
	if req.Status == "" {
		req.Status = "enabled"
	}
	return s.store.CreateTemplate(ctx, tenantID, actorID, req)
}
func (s *Service) CreateVersion(ctx context.Context, tenantID, actorID int64, templateID string, req SaveVersionRequest) (*TemplateVersion, error) {
	normalizeVersion(&req)
	if req.VersionNo == "" || req.Name == "" {
		return nil, fmt.Errorf("version_no and name are required")
	}
	return s.store.CreateVersion(ctx, tenantID, actorID, templateID, req)
}
func (s *Service) UpdateVersion(ctx context.Context, tenantID int64, versionID string, req SaveVersionRequest) (*TemplateVersion, error) {
	normalizeVersion(&req)
	return s.store.UpdateVersion(ctx, tenantID, versionID, req)
}
func (s *Service) SaveSections(ctx context.Context, tenantID int64, versionID string, req SaveSectionsRequest) ([]*Section, error) {
	if err := validateSections(req.Items); err != nil {
		return nil, err
	}
	return s.store.SaveSections(ctx, tenantID, versionID, req.Items)
}
func (s *Service) SaveBindings(ctx context.Context, tenantID int64, versionID string, req SaveBindingsRequest) ([]*RequirementBinding, error) {
	if err := validateBindings(req.Items); err != nil {
		return nil, err
	}
	return s.store.SaveBindings(ctx, tenantID, versionID, req.Items)
}
func (s *Service) Publish(ctx context.Context, tenantID int64, versionID string, actorID int64) error {
	return s.store.Publish(ctx, tenantID, versionID, actorID)
}
func (s *Service) Disable(ctx context.Context, tenantID int64, versionID string, actorID int64) error {
	return s.store.Disable(ctx, tenantID, versionID, actorID)
}
func normalizeTemplate(r *SaveTemplateRequest) {
	r.Code = strings.TrimSpace(r.Code)
	r.Name = strings.TrimSpace(r.Name)
	r.Status = strings.TrimSpace(r.Status)
}
func normalizeVersion(r *SaveVersionRequest) {
	r.VersionNo = strings.TrimSpace(r.VersionNo)
	r.Name = strings.TrimSpace(r.Name)
	r.DocumentType = strings.TrimSpace(r.DocumentType)
	r.VisitType = strings.TrimSpace(r.VisitType)
	r.PrintTitle = strings.TrimSpace(r.PrintTitle)
}

func validateSections(items []SectionInput) error {
	seenCodes := make(map[string]bool, len(items))
	seenOrders := make(map[int]bool, len(items))
	for _, item := range items {
		code := strings.TrimSpace(item.Code)
		if code == "" || strings.TrimSpace(item.Name) == "" {
			return fmt.Errorf("模板栏目编码和名称不能为空")
		}
		if seenCodes[code] {
			return fmt.Errorf("模板栏目编码重复：%s", code)
		}
		if seenOrders[item.DisplayOrder] {
			return fmt.Errorf("模板栏目展示顺序重复：%d", item.DisplayOrder)
		}
		seenCodes[code] = true
		seenOrders[item.DisplayOrder] = true
	}
	return nil
}

func validateBindings(items []BindingInput) error {
	validModes := map[string]bool{"程序判断": true, "大模型判断": true, "人工判断": true}
	validDeadlines := map[string]bool{"仅提示": true, "提交前处理": true, "确认前处理": true, "归档前处理": true, "归档后质控": true}
	seenRequirements := make(map[string]bool, len(items))
	seenOrders := make(map[int]bool, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.QualityRequirementID)
		if id == "" {
			return fmt.Errorf("质量要求标识不能为空")
		}
		if seenRequirements[id] {
			return fmt.Errorf("模板版本不能重复选用同一质量要求")
		}
		if seenOrders[item.DisplayOrder] {
			return fmt.Errorf("质量要求展示顺序重复：%d", item.DisplayOrder)
		}
		if !validModes[item.ExecutionMode] {
			return fmt.Errorf("无效的主要判断方式：%s", item.ExecutionMode)
		}
		if !validDeadlines[item.DeadlineAction] {
			return fmt.Errorf("无效的处理截止动作：%s", item.DeadlineAction)
		}
		seenRequirements[id] = true
		seenOrders[item.DisplayOrder] = true
	}
	return nil
}

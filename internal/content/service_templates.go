package content

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type runtimeTemplateEntry struct {
	Template TemplateResponse
	Versions []VersionResponse
}

func (s *Service) ListPromptTemplates(ctx context.Context, req TemplateListRequest, contentOnly bool) ([]TemplateResponse, int, error) {
	if contentOnly {
		return s.store.ListContentPromptTemplates(ctx, req)
	}
	_ = ctx
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	s.templateMu.RLock()
	defer s.templateMu.RUnlock()

	source := s.promptTemplates
	if contentOnly {
		source = s.contentTemplates
	}

	items := make([]TemplateResponse, 0, len(source))
	for _, entry := range source {
		t := entry.Template
		if len(req.TenantIDs) > 0 {
			if t.TenantID != nil && !tenantIDInList(*t.TenantID, req.TenantIDs) {
				continue
			}
		} else if req.TenantID != nil {
			// Include tenant-owned templates and global templates.
			if t.TenantID != nil && *t.TenantID != *req.TenantID {
				continue
			}
		}
		if req.Category != nil {
			if t.Category == nil || *t.Category != *req.Category {
				continue
			}
		}
		if req.FunctionType != nil {
			if t.FunctionType == nil || *t.FunctionType != *req.FunctionType {
				continue
			}
		}
		if req.IsActive != nil && t.IsActive != *req.IsActive {
			continue
		}
		items = append(items, t)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].ID > items[j].ID
	})

	total := len(items)
	start := (req.Page - 1) * req.PageSize
	if start >= total {
		return []TemplateResponse{}, total, nil
	}
	end := start + req.PageSize
	if end > total {
		end = total
	}
	return items[start:end], total, nil
}

func tenantIDInList(tenantID int64, tenantIDs []int64) bool {
	for _, id := range tenantIDs {
		if id == tenantID {
			return true
		}
	}
	return false
}

func (s *Service) CreatePromptTemplate(ctx context.Context, tenantID *int64, createdBy int64, req CreateTemplateRequest, contentOnly bool) (*TemplateResponse, error) {
	if contentOnly {
		return s.store.CreateContentPromptTemplate(ctx, tenantID, createdBy, req)
	}
	_ = ctx
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	templateText := strings.TrimSpace(req.Template)
	if templateText == "" && req.PromptTemplate != nil {
		templateText = strings.TrimSpace(*req.PromptTemplate)
	}
	if templateText == "" {
		return nil, fmt.Errorf("template is required")
	}

	s.templateMu.Lock()
	defer s.templateMu.Unlock()

	id := s.nextTemplateID
	s.nextTemplateID++

	code := strings.TrimSpace(req.Code)
	if code == "" {
		code = fmt.Sprintf("tpl_%d", id)
	}
	now := time.Now().Format(time.RFC3339)
	currentVersion := 1
	publishedVersion := 1
	resp := TemplateResponse{
		ID:               id,
		TenantID:         tenantID,
		Code:             code,
		Name:             req.Name,
		Description:      req.Description,
		Category:         req.Category,
		FunctionType:     req.FunctionType,
		Template:         templateText,
		PromptTemplate:   templateText,
		Variables:        req.Variables,
		CurrentVersion:   &currentVersion,
		PublishedVersion: &publishedVersion,
		IsActive:         true,
		ExtraData:        req.ExtraData,
		CreatedBy:        createdBy,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	version := VersionResponse{
		ID:          s.nextVersionID,
		TemplateID:  id,
		Version:     1,
		Template:    templateText,
		Variables:   req.Variables,
		ChangeLog:   strPtr("initial version"),
		IsPublished: true,
		PublishedAt: strPtr(now),
		CreatedBy:   createdBy,
		CreatedAt:   now,
	}
	s.nextVersionID++

	entry := &runtimeTemplateEntry{
		Template: resp,
		Versions: []VersionResponse{version},
	}
	if contentOnly {
		s.contentTemplates[id] = entry
	} else {
		s.promptTemplates[id] = entry
	}

	copyResp := resp
	return &copyResp, nil
}

func (s *Service) GetPromptTemplateByID(ctx context.Context, id int64, contentOnly bool) (*TemplateResponse, error) {
	if contentOnly {
		return s.store.GetContentPromptTemplateByID(ctx, id)
	}
	_ = ctx
	s.templateMu.RLock()
	defer s.templateMu.RUnlock()

	entry, ok := s.getTemplateMap(contentOnly)[id]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}
	resp := entry.Template
	return &resp, nil
}

func (s *Service) UpdatePromptTemplate(ctx context.Context, id int64, req UpdateTemplateRequest, contentOnly bool) (*TemplateResponse, error) {
	if contentOnly {
		return s.store.UpdateContentPromptTemplate(ctx, id, req)
	}
	_ = ctx
	s.templateMu.Lock()
	defer s.templateMu.Unlock()

	entry, ok := s.getTemplateMap(contentOnly)[id]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}
	if req.Name != nil {
		entry.Template.Name = *req.Name
	}
	if req.Description != nil {
		entry.Template.Description = req.Description
	}
	if req.Category != nil {
		entry.Template.Category = req.Category
	}
	if req.FunctionType != nil {
		entry.Template.FunctionType = req.FunctionType
	}
	if req.PromptTemplate != nil {
		text := strings.TrimSpace(*req.PromptTemplate)
		if text != "" {
			entry.Template.Template = text
			entry.Template.PromptTemplate = text
		}
	}
	if req.Template != nil {
		text := strings.TrimSpace(*req.Template)
		if text != "" {
			entry.Template.Template = text
			entry.Template.PromptTemplate = text
		}
	}
	if req.Variables != nil {
		entry.Template.Variables = req.Variables
	}
	if req.IsActive != nil {
		entry.Template.IsActive = *req.IsActive
	}
	if req.ExtraData != nil {
		entry.Template.ExtraData = req.ExtraData
	}
	entry.Template.UpdatedAt = time.Now().Format(time.RFC3339)
	resp := entry.Template
	return &resp, nil
}

func (s *Service) DeletePromptTemplate(ctx context.Context, id int64, contentOnly bool) error {
	if contentOnly {
		return s.store.DeleteContentPromptTemplate(ctx, id)
	}
	_ = ctx
	s.templateMu.Lock()
	defer s.templateMu.Unlock()
	m := s.getTemplateMap(contentOnly)
	if _, ok := m[id]; !ok {
		return fmt.Errorf("template not found")
	}
	delete(m, id)
	return nil
}

func (s *Service) ClonePromptTemplate(ctx context.Context, id int64, createdBy int64, contentOnly bool) (*TemplateResponse, error) {
	if contentOnly {
		return s.store.CloneContentPromptTemplate(ctx, id, createdBy)
	}
	_ = ctx
	s.templateMu.Lock()
	defer s.templateMu.Unlock()

	src, ok := s.getTemplateMap(contentOnly)[id]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}

	newID := s.nextTemplateID
	s.nextTemplateID++
	now := time.Now().Format(time.RFC3339)

	cloned := src.Template
	cloned.ID = newID
	cloned.Code = cloned.Code + "_clone"
	cloned.Name = cloned.Name + " (Copy)"
	cloned.CreatedBy = createdBy
	cloned.CreatedAt = now
	cloned.UpdatedAt = now

	versions := make([]VersionResponse, 0, len(src.Versions))
	for _, v := range src.Versions {
		nv := v
		nv.ID = s.nextVersionID
		s.nextVersionID++
		nv.TemplateID = newID
		nv.CreatedBy = createdBy
		nv.CreatedAt = now
		versions = append(versions, nv)
	}

	s.getTemplateMap(contentOnly)[newID] = &runtimeTemplateEntry{
		Template: cloned,
		Versions: versions,
	}
	resp := cloned
	return &resp, nil
}

func (s *Service) CreatePromptTemplateVersion(ctx context.Context, templateID int64, createdBy int64, req CreateVersionRequest) (*VersionResponse, error) {
	_ = ctx
	if strings.TrimSpace(req.Template) == "" {
		return nil, fmt.Errorf("template is required")
	}

	s.templateMu.Lock()
	defer s.templateMu.Unlock()
	entry, ok := s.promptTemplates[templateID]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}

	nextVersion := 1
	if entry.Template.CurrentVersion != nil {
		nextVersion = *entry.Template.CurrentVersion + 1
	}
	now := time.Now().Format(time.RFC3339)
	version := VersionResponse{
		ID:          s.nextVersionID,
		TemplateID:  templateID,
		Version:     nextVersion,
		Template:    req.Template,
		Variables:   req.Variables,
		ChangeLog:   req.ChangeLog,
		IsPublished: false,
		PublishedAt: nil,
		CreatedBy:   createdBy,
		CreatedAt:   now,
	}
	s.nextVersionID++

	entry.Versions = append(entry.Versions, version)
	entry.Template.Template = req.Template
	entry.Template.PromptTemplate = req.Template
	entry.Template.Variables = req.Variables
	entry.Template.CurrentVersion = &nextVersion
	entry.Template.UpdatedAt = now

	resp := version
	return &resp, nil
}

func (s *Service) ListPromptTemplateVersions(ctx context.Context, templateID int64) ([]VersionResponse, error) {
	_ = ctx
	s.templateMu.RLock()
	defer s.templateMu.RUnlock()
	entry, ok := s.promptTemplates[templateID]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}
	versions := append([]VersionResponse{}, entry.Versions...)
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version > versions[j].Version })
	return versions, nil
}

func (s *Service) PublishPromptTemplate(ctx context.Context, templateID int64) error {
	_ = ctx
	s.templateMu.Lock()
	defer s.templateMu.Unlock()
	entry, ok := s.promptTemplates[templateID]
	if !ok {
		return fmt.Errorf("template not found")
	}
	if len(entry.Versions) == 0 {
		return fmt.Errorf("no versions found")
	}

	last := entry.Versions[len(entry.Versions)-1]
	now := time.Now().Format(time.RFC3339)
	for i := range entry.Versions {
		entry.Versions[i].IsPublished = entry.Versions[i].Version == last.Version
		if entry.Versions[i].IsPublished {
			entry.Versions[i].PublishedAt = strPtr(now)
		}
	}
	entry.Template.Template = last.Template
	entry.Template.PromptTemplate = last.Template
	entry.Template.Variables = last.Variables
	entry.Template.PublishedVersion = &last.Version
	entry.Template.CurrentVersion = &last.Version
	entry.Template.UpdatedAt = now
	return nil
}

func (s *Service) RollbackPromptTemplate(ctx context.Context, templateID int64, version int) error {
	_ = ctx
	s.templateMu.Lock()
	defer s.templateMu.Unlock()
	entry, ok := s.promptTemplates[templateID]
	if !ok {
		return fmt.Errorf("template not found")
	}

	var target *VersionResponse
	for i := range entry.Versions {
		if entry.Versions[i].Version == version {
			target = &entry.Versions[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("version not found")
	}

	now := time.Now().Format(time.RFC3339)
	entry.Template.Template = target.Template
	entry.Template.PromptTemplate = target.Template
	entry.Template.Variables = target.Variables
	entry.Template.CurrentVersion = &target.Version
	entry.Template.PublishedVersion = &target.Version
	entry.Template.UpdatedAt = now

	for i := range entry.Versions {
		entry.Versions[i].IsPublished = entry.Versions[i].Version == target.Version
		if entry.Versions[i].IsPublished {
			entry.Versions[i].PublishedAt = strPtr(now)
		}
	}
	return nil
}

func (s *Service) TestPromptTemplate(ctx context.Context, templateID int64, contentOnly bool) (string, error) {
	tpl, err := s.GetPromptTemplateByID(ctx, templateID, contentOnly)
	if err != nil {
		return "", err
	}
	result := tpl.Template
	if len(tpl.Variables) > 0 {
		for _, variable := range tpl.Variables {
			result = strings.ReplaceAll(result, "{{"+variable+"}}", "test_"+variable)
			result = strings.ReplaceAll(result, "{{ "+variable+" }}", "test_"+variable)
		}
	}
	return result, nil
}

func (s *Service) PromptTemplateStats(ctx context.Context, templateID int64, contentOnly bool) (map[string]interface{}, error) {
	if contentOnly {
		tpl, err := s.GetPromptTemplateByID(ctx, templateID, true)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"template_id":     tpl.ID,
			"is_active":       tpl.IsActive,
			"is_system":       tpl.IsSystem,
			"version":         tpl.Version,
			"usage_count":     tpl.UsageCount,
			"last_updated_at": tpl.UpdatedAt,
			"variable_count":  len(tpl.Variables),
			"business_type":   tpl.BusinessType,
			"source_table":    tpl.SourceTable,
			"function_type":   tpl.FunctionType,
		}, nil
	}
	_ = ctx
	s.templateMu.RLock()
	defer s.templateMu.RUnlock()
	entry, ok := s.getTemplateMap(contentOnly)[templateID]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}
	return map[string]interface{}{
		"template_id":         templateID,
		"current_version":     entry.Template.CurrentVersion,
		"published_version":   entry.Template.PublishedVersion,
		"version_count":       len(entry.Versions),
		"is_active":           entry.Template.IsActive,
		"last_updated_at":     entry.Template.UpdatedAt,
		"variable_count":      len(entry.Template.Variables),
		"estimated_use_count": len(entry.Versions) * 3,
	}, nil
}

func (s *Service) InitializeDefaultContentTemplates(ctx context.Context, tenantID *int64, createdBy int64) (int, error) {
	_ = ctx
	defaults := []CreateTemplateRequest{
		{
			Code:         "content_article_default",
			Name:         "文章生成默认模板",
			Category:     strPtr("content_generation"),
			FunctionType: strPtr("content_article"),
			Template:     "请根据主题{{topic}}生成一篇结构化科普文章，目标读者是{{audience}}。",
			Variables:    []string{"topic", "audience"},
		},
		{
			Code:         "content_script_default",
			Name:         "脚本生成默认模板",
			Category:     strPtr("content_generation"),
			FunctionType: strPtr("content_script"),
			Template:     "请根据主题{{topic}}生成短视频口播脚本，包含开场、核心观点、结尾行动建议。",
			Variables:    []string{"topic"},
		},
		{
			Code:         "content_graphic_note_default",
			Name:         "图文生成默认模板",
			Category:     strPtr("content_generation"),
			FunctionType: strPtr("content_graphic_note"),
			Template:     "请根据主题{{topic}}生成小红书图文笔记提纲，包含封面标题、正文分段和结尾互动。",
			Variables:    []string{"topic"},
		},
	}

	created := 0
	for _, item := range defaults {
		if _, err := s.CreatePromptTemplate(ctx, tenantID, createdBy, item, true); err == nil {
			created++
		}
	}
	return created, nil
}

func (s *Service) getTemplateMap(contentOnly bool) map[int64]*runtimeTemplateEntry {
	if contentOnly {
		return s.contentTemplates
	}
	return s.promptTemplates
}

func strPtr(s string) *string {
	return &s
}

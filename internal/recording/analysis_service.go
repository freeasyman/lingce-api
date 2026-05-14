package recording

import (
	"context"
	"fmt"
	"strings"
	"time"
)

var analysisSceneNames = map[string]string{
	"post_call_analysis":  "诊后分析",
	"followup_quality":    "术后回访",
	"admission_consult":   "到院咨询",
	"frontdesk_reception": "前台接待",
}

func (s *Service) ListAnalysisRoleOptions(ctx context.Context, tenantIDs []int64) ([]*AnalysisRoleOption, error) {
	return s.store.ListAnalysisRoleOptions(ctx, tenantIDs)
}

func (s *Service) ListAnalysisPipelineOptions(ctx context.Context) ([]*AnalysisPipelineOption, error) {
	return s.store.ListAnalysisPipelineOptions(ctx)
}

func (s *Service) ListAnalysisRoutes(ctx context.Context, req AnalysisRouteListRequest) ([]*AnalysisRouteRecord, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	items, total, err := s.store.ListAnalysisRoutes(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	for _, item := range items {
		fillAnalysisRouteDerivedFields(item)
	}
	return items, total, nil
}

func (s *Service) GetAnalysisRoute(ctx context.Context, tenantIDs []int64, id int64) (*AnalysisRouteRecord, error) {
	item, err := s.store.GetAnalysisRoute(ctx, tenantIDs, id)
	if err != nil {
		return nil, err
	}
	fillAnalysisRouteDerivedFields(item)
	return item, nil
}

func (s *Service) CreateAnalysisRoute(ctx context.Context, tenantIDs []int64, userID int64, req CreateAnalysisRouteRequest) (*AnalysisRouteRecord, error) {
	if req.TenantID <= 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if !containsInt64(tenantIDs, req.TenantID) {
		return nil, fmt.Errorf("access denied")
	}
	if req.RoleID <= 0 {
		return nil, fmt.Errorf("role_id is required")
	}
	if strings.TrimSpace(req.SceneCode) == "" {
		return nil, fmt.Errorf("scene_code is required")
	}
	if strings.TrimSpace(req.PipelineCode) == "" {
		return nil, fmt.Errorf("pipeline_code is required")
	}
	if strings.TrimSpace(req.PipelineVersion) == "" {
		return nil, fmt.Errorf("pipeline_version is required")
	}
	if !isKnownAnalysisScene(req.SceneCode) {
		return nil, fmt.Errorf("invalid scene_code")
	}
	if _, err := s.store.GetAnalysisRoleOptionByID(ctx, req.TenantID, req.RoleID); err != nil {
		return nil, fmt.Errorf("role not found: %w", err)
	}
	if _, err := s.store.GetAnalysisPipelineOption(ctx, req.PipelineCode, req.PipelineVersion); err != nil {
		return nil, fmt.Errorf("pipeline not found: %w", err)
	}
	item, err := s.store.CreateAnalysisRoute(ctx, userID, req)
	if err != nil {
		return nil, err
	}
	fillAnalysisRouteDerivedFields(item)
	return item, nil
}

func (s *Service) UpdateAnalysisRoute(ctx context.Context, tenantIDs []int64, userID, id int64, req UpdateAnalysisRouteRequest) (*AnalysisRouteRecord, error) {
	existing, err := s.store.GetAnalysisRoute(ctx, tenantIDs, id)
	if err != nil {
		return nil, err
	}
	if req.RoleID != nil {
		if *req.RoleID <= 0 {
			return nil, fmt.Errorf("role_id must be positive")
		}
		if _, err := s.store.GetAnalysisRoleOptionByID(ctx, existing.TenantID, *req.RoleID); err != nil {
			return nil, fmt.Errorf("role not found: %w", err)
		}
	}
	if req.SceneCode != nil && !isKnownAnalysisScene(*req.SceneCode) {
		return nil, fmt.Errorf("invalid scene_code")
	}
	pipelineCode := existing.PipelineCode
	pipelineVersion := existing.PipelineVersion
	if req.PipelineCode != nil {
		pipelineCode = strings.TrimSpace(*req.PipelineCode)
	}
	if req.PipelineVersion != nil {
		pipelineVersion = strings.TrimSpace(*req.PipelineVersion)
	}
	if pipelineCode == "" || pipelineVersion == "" {
		return nil, fmt.Errorf("pipeline_code and pipeline_version are required")
	}
	if _, err := s.store.GetAnalysisPipelineOption(ctx, pipelineCode, pipelineVersion); err != nil {
		return nil, fmt.Errorf("pipeline not found: %w", err)
	}
	item, err := s.store.UpdateAnalysisRoute(ctx, userID, id, req)
	if err != nil {
		return nil, err
	}
	fillAnalysisRouteDerivedFields(item)
	return item, nil
}

func (s *Service) PublishAnalysisRoute(ctx context.Context, tenantIDs []int64, userID, id int64, req PublishAnalysisRouteRequest) (*AnalysisRouteRecord, error) {
	item, err := s.store.PublishAnalysisRoute(ctx, tenantIDs, userID, id, req)
	if err != nil {
		return nil, err
	}
	fillAnalysisRouteDerivedFields(item)
	return item, nil
}

func (s *Service) RollbackAnalysisRoute(ctx context.Context, tenantIDs []int64, userID, id int64) (*AnalysisRouteRecord, error) {
	item, err := s.store.RollbackAnalysisRoute(ctx, tenantIDs, userID, id)
	if err != nil {
		return nil, err
	}
	fillAnalysisRouteDerivedFields(item)
	return item, nil
}

func (s *Service) ListAnalysisRuns(ctx context.Context, req AnalysisRunListRequest) ([]*AnalysisRunRecord, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	items, total, err := s.store.ListAnalysisRuns(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	fillAnalysisRunDerivedFields(items)
	return items, total, nil
}

func (s *Service) GetAnalysisRun(ctx context.Context, tenantIDs []int64, id int64) (*AnalysisRunRecord, error) {
	item, err := s.store.GetAnalysisRun(ctx, tenantIDs, id)
	if err != nil {
		return nil, err
	}
	fillAnalysisRunDerivedFields([]*AnalysisRunRecord{item})
	return item, nil
}

func (s *Service) ListAnalysisRunSteps(ctx context.Context, tenantIDs []int64, runID int64) ([]*AnalysisStepRecord, error) {
	items, err := s.store.ListAnalysisRunSteps(ctx, tenantIDs, runID)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func fillAnalysisRouteDerivedFields(item *AnalysisRouteRecord) {
	if item == nil {
		return
	}
	item.SceneName = analysisSceneName(item.SceneCode)
	if strings.TrimSpace(item.UpdatedBy) == "" {
		item.UpdatedBy = "系统"
	}
}

func fillAnalysisRunDerivedFields(items []*AnalysisRunRecord) {
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.SceneCode != nil {
			name := analysisSceneName(*item.SceneCode)
			item.SceneName = &name
		}
		if item.StartedAt != nil && item.EndedAt != nil {
			d := item.EndedAt.Sub(*item.StartedAt).Milliseconds()
			item.DurationMs = &d
		}
		if item.SnapshotVersion == nil {
			v := item.CreatedAt.Format("20060102-150405")
			item.SnapshotVersion = &v
		}
	}
}

func analysisSceneName(code string) string {
	if name, ok := analysisSceneNames[strings.TrimSpace(code)]; ok {
		return name
	}
	return code
}

func isKnownAnalysisScene(code string) bool {
	_, ok := analysisSceneNames[strings.TrimSpace(code)]
	return ok
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func defaultRouteEffectiveAt(v *time.Time) time.Time {
	if v != nil {
		return *v
	}
	return time.Now()
}

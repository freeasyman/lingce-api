package emrcheck

import (
	"context"
	"fmt"
	"strings"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) Preview(ctx context.Context, req CheckPreviewRequest) ([]*CheckResult, string, error) {
	data := checkContext{
		RecordID:          "",
		SnapshotID:        "preview",
		TemplateVersionID: strings.TrimSpace(req.TemplateVersionID),
		VisitType:         strings.TrimSpace(req.VisitType),
		Specialty:         strings.TrimSpace(req.Specialty),
		Content:           req.Content,
		ConfirmedBy:       req.ConfirmedBy,
	}
	if data.TemplateVersionID == "" {
		return nil, "", fmt.Errorf("template_version_id is required")
	}
	bindings, err := s.store.previewBindings(ctx, req.TenantID, data.TemplateVersionID)
	if err != nil {
		return nil, "", err
	}
	trigger := strings.TrimSpace(req.TriggerAction)
	if trigger == "" {
		trigger = "手动保存"
	}
	results := make([]*CheckResult, 0, len(bindings))
	evaluated := make([]evaluatedResult, 0, len(bindings))
	for _, item := range bindings {
		result := evaluateRequirement(item, data, trigger)
		evaluated = append(evaluated, result)
		results = append(results, &CheckResult{
			QualityRequirementID:    item.QualityRequirementID,
			RequirementCode:         item.Code,
			RequirementName:         item.Name,
			ConfiguredExecutionMode: item.ExecutionMode,
			ActualExecutionMode:     result.ActualMode,
			Conclusion:              result.Conclusion,
			CheckStatus:             result.CheckStatus,
			IncompleteReason:        result.IncompleteReason,
			HandlingResult:          result.HandlingResult,
			Evidence:                result.Evidence,
			HitExplanation:          result.Explanation,
			SuggestedHandling:       result.Suggested,
		})
	}
	return results, overallResult(evaluated), nil
}

func (s *Service) Run(ctx context.Context, req CheckRequest) (*CheckRun, error) {
	return s.store.Run(ctx, req)
}

func (s *Service) RunManual(ctx context.Context, tenantID, actorID int64, recordID string) (*CheckRun, error) {
	snapshotID, err := s.store.CurrentSnapshotID(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	if snapshotID == "" {
		return nil, fmt.Errorf("record has no formal snapshot")
	}
	return s.Run(ctx, CheckRequest{TenantID: tenantID, RecordID: recordID, SnapshotID: snapshotID, TriggerAction: "人工重新检查", StartedBy: &actorID})
}

func (s *Service) ListRuns(ctx context.Context, tenantID int64, recordID string) ([]*CheckRun, error) {
	return s.store.ListRuns(ctx, tenantID, recordID)
}

func (s *Service) GetRun(ctx context.Context, tenantID int64, recordID, id string) (*CheckRun, error) {
	return s.store.GetRun(ctx, tenantID, recordID, id)
}

package emrcheck

import (
	"context"
	"fmt"
	"strings"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) Preview(ctx context.Context, req CheckPreviewRequest) ([]*CheckResult, string, error) {
	return s.preview(ctx, req, false)
}

// PreviewProgram 只执行模板绑定中明确配置为“程序判断”的规则。
//
// 第一阶段质检使用这个入口：输入已经结构化的病历，调用确定性的程序
// 规则，返回结构化结果。它不写 emr_check_runs / emr_check_results，也
// 不调用大模型，便于在正式接入完整质检链路前独立验证规则执行结果。
//
// 署名：Codex
// 时间：2026-09-14
func (s *Service) PreviewProgram(ctx context.Context, req CheckPreviewRequest) ([]*CheckResult, string, error) {
	return s.preview(ctx, req, true)
}

func (s *Service) preview(ctx context.Context, req CheckPreviewRequest, programOnly bool) ([]*CheckResult, string, error) {
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
	if len(bindings) == 0 {
		return nil, "", fmt.Errorf("模板版本没有绑定质检规则，无法执行程序质检：%s", data.TemplateVersionID)
	}
	trigger := strings.TrimSpace(req.TriggerAction)
	if trigger == "" {
		trigger = "手动保存"
	}
	results := make([]*CheckResult, 0, len(bindings))
	evaluated := make([]evaluatedResult, 0, len(bindings))
	for _, item := range bindings {
		if programOnly && item.ExecutionMode != "程序判断" {
			continue
		}
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

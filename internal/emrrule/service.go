package emrrule

import (
	"context"
	"strings"
)

type Service struct {
	store  *Store
	engine *Engine
}

func NewService(store *Store) *Service {
	return &Service{store: store, engine: NewEngine()}
}

func (s *Service) RunRecordRules(ctx context.Context, tenantID, recordID, actorID int64, req RunRequest) (*RunResult, error) {
	stage := normalizeStage(req.Stage)
	trigger := normalizeTrigger(req.TriggerSource, stage)
	record, err := s.store.LoadRecord(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	if runID, err := s.store.FindLatestSuccessfulRun(ctx, tenantID, recordID, stage, record.LatestVersionNo); err == nil && runID > 0 {
		summary, err := s.store.GetSummary(ctx, tenantID, recordID)
		if err != nil {
			return nil, err
		}
		if summary == nil {
			summary = &RuleSummary{RecordID: recordID, CanSubmit: true, CanArchive: true}
		}
		hits, err := s.store.ListRunHits(ctx, tenantID, recordID, runID)
		if err != nil {
			return nil, err
		}
		return &RunResult{Run: RuleRun{ID: runID, RecordID: recordID, Stage: stage, TriggerSource: trigger, Status: "success"}, Hits: hits, Summary: *summary}, nil
	}
	rules, err := s.store.ListExecutableRules(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	runID, err := s.store.CreateRun(ctx, record, stage, trigger, actorID)
	if err != nil {
		return nil, err
	}
	hits := s.engine.Evaluate(record, rules, stage)
	inserted, err := s.store.ReplaceRunHits(ctx, runID, record, stage, hits)
	if err != nil {
		return nil, err
	}
	summary, err := s.store.GetSummary(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		summary = &RuleSummary{RecordID: recordID, CanSubmit: true, CanArchive: true}
	}
	return &RunResult{Run: RuleRun{ID: runID, RecordID: recordID, Stage: stage, TriggerSource: trigger, Status: "success"}, Hits: inserted, Summary: *summary}, nil
}

func (s *Service) RunStageAndGetSummary(ctx context.Context, tenantID, recordID, actorID int64, stage, triggerSource string) (*RuleSummary, error) {
	result, err := s.RunRecordRules(ctx, tenantID, recordID, actorID, RunRequest{Stage: stage, TriggerSource: triggerSource})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return &result.Summary, nil
}

func (s *Service) CanSubmit(ctx context.Context, tenantID, recordID, actorID int64) (*RuleSummary, error) {
	return s.RunStageAndGetSummary(ctx, tenantID, recordID, actorID, "pre_submit", "submit")
}

func (s *Service) CanArchive(ctx context.Context, tenantID, recordID, actorID int64) (*RuleSummary, error) {
	return s.RunStageAndGetSummary(ctx, tenantID, recordID, actorID, "pre_archive", "archive")
}

func (s *Service) ListRecordHits(ctx context.Context, tenantID, recordID int64, activeOnly bool) ([]RuleHit, error) {
	return s.store.ListHits(ctx, tenantID, recordID, activeOnly)
}

func (s *Service) GetRecordSummary(ctx context.Context, tenantID, recordID int64) (*RuleSummary, error) {
	summary, err := s.store.GetSummary(ctx, tenantID, recordID)
	if err != nil {
		return nil, err
	}
	if summary != nil {
		return summary, nil
	}
	return &RuleSummary{RecordID: recordID, CanSubmit: true, CanArchive: true}, nil
}

func (s *Service) MarkDoctorAction(ctx context.Context, tenantID, recordID, hitID, actorID int64, actorRole string, req DoctorActionRequest) error {
	return s.store.MarkDoctorAction(ctx, tenantID, recordID, hitID, actorID, actorRole, strings.TrimSpace(req.ActionType), strings.TrimSpace(req.Comment))
}

func normalizeStage(stage string) string {
	switch strings.TrimSpace(stage) {
	case "realtime", "save", "pre_submit", "pre_archive", "post_archive_qc", "template_publish":
		return strings.TrimSpace(stage)
	default:
		return "save"
	}
}

func normalizeTrigger(trigger, stage string) string {
	trigger = strings.TrimSpace(trigger)
	switch trigger {
	case "user_input", "save", "submit", "archive", "batch_qc", "system_timer", "manual":
		return trigger
	}
	switch stage {
	case "realtime":
		return "user_input"
	case "pre_submit":
		return "submit"
	case "pre_archive":
		return "archive"
	case "post_archive_qc":
		return "batch_qc"
	default:
		return "save"
	}
}

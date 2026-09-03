package emrrecord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrcheck"
	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/emrprocess"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	store   *Store
	checks  *emrcheck.Service
	process *emrprocess.Service
}

func NewService(store *Store, checks *emrcheck.Service, process *emrprocess.Service) *Service {
	return &Service{store: store, checks: checks, process: process}
}

func (s *Service) List(ctx context.Context, tenantID int64, status string) ([]*Record, error) {
	return s.store.List(ctx, tenantID, strings.TrimSpace(status))
}

func (s *Service) ListScoped(ctx context.Context, tenantID int64, status string, access *emrpermission.Access) ([]*Record, error) {
	return s.store.ListScoped(ctx, tenantID, strings.TrimSpace(status), access)
}

func (s *Service) ListByPatient(ctx context.Context, tenantID, patientID int64, page, pageSize int) ([]*Record, int, error) {
	return s.store.ListByPatient(ctx, tenantID, patientID, page, pageSize)
}

func (s *Service) ListByPatientScoped(ctx context.Context, tenantID, patientID int64, page, pageSize int, access *emrpermission.Access) ([]*Record, int, error) {
	return s.store.ListByPatientScoped(ctx, tenantID, patientID, page, pageSize, access)
}

func (s *Service) Get(ctx context.Context, tenantID int64, id string) (*Record, error) {
	return s.store.Get(ctx, tenantID, id)
}

func (s *Service) ListAICandidates(ctx context.Context, tenantID int64, recordID string, activeOnly bool) ([]*AICandidate, error) {
	return s.store.ListAICandidates(ctx, tenantID, recordID, activeOnly)
}

func (s *Service) CreateAICandidate(ctx context.Context, tenantID int64, recordID string, req CreateAICandidateRequest) (*AICandidate, error) {
	if strings.TrimSpace(req.SectionCode) == "" {
		return nil, fmt.Errorf("section_code is required")
	}
	if req.Content == nil {
		return nil, fmt.Errorf("content is required")
	}
	return s.store.CreateAICandidate(ctx, tenantID, recordID, req)
}

func (s *Service) GenerateRecordingAICandidates(ctx context.Context, req GenerateAICandidatesRequest) (*GenerateAICandidatesOutcome, error) {
	if req.TenantID <= 0 || req.RecordingID <= 0 {
		return nil, fmt.Errorf("tenant_id and recording_id are required")
	}
	if strings.TrimSpace(req.GenerationKey) == "" {
		return nil, fmt.Errorf("generation_key is required")
	}
	if len(req.Candidates) == 0 {
		return nil, fmt.Errorf("candidates must not be empty")
	}
	for _, candidate := range req.Candidates {
		if strings.TrimSpace(candidate.SectionCode) == "" || candidate.Content == nil {
			return nil, fmt.Errorf("candidate section_code and content are required")
		}
	}
	outcome, created, err := s.store.GenerateRecordingAICandidates(ctx, req)
	if err != nil {
		return nil, err
	}
	if created && outcome != nil && outcome.Record != nil {
		recordID := outcome.Record.ID
		if _, err := s.process.Append(ctx, emrprocess.AppendRequest{
			TenantID: req.TenantID, RecordID: &recordID, ActionType: "生成病历", ActionResult: "成功",
			ActorType: "系统", Source: "录音分析", AfterStatus: &outcome.Record.Status,
			OutputInfo: map[string]any{"recording_id": req.RecordingID, "generation_key": req.GenerationKey},
		}); err != nil {
			return nil, fmt.Errorf("record generated but process record failed: %w", err)
		}
	}
	return outcome, nil
}

func (s *Service) GenerateRealtimeAICandidates(ctx context.Context, req GenerateRealtimeAICandidatesRequest) ([]*AICandidate, error) {
	if req.TenantID <= 0 || strings.TrimSpace(req.RecordID) == "" || req.EncounterID <= 0 {
		return nil, fmt.Errorf("tenant_id, record_id and encounter_id are required")
	}
	if strings.TrimSpace(req.GenerationKey) == "" {
		return nil, fmt.Errorf("generation_key is required")
	}
	if len(req.Candidates) == 0 {
		return nil, fmt.Errorf("candidates must not be empty")
	}
	for _, candidate := range req.Candidates {
		if strings.TrimSpace(candidate.SectionCode) == "" || candidate.Content == nil {
			return nil, fmt.Errorf("candidate section_code and content are required")
		}
	}
	return s.store.GenerateRealtimeAICandidates(ctx, req)
}

func (s *Service) HandleAICandidate(ctx context.Context, tenantID, actorID int64, recordID, candidateID, decision string, req HandleAICandidateRequest) (*AICandidate, *Record, error) {
	if decision != "已采纳" && decision != "已拒绝" {
		return nil, nil, fmt.Errorf("invalid ai candidate decision")
	}
	candidate, record, err := s.store.ApplyAICandidateDecision(ctx, tenantID, actorID, recordID, candidateID, decision, req.Content, func(ctx context.Context, tx pgx.Tx, candidate *AICandidate, record *Record, beforeContent map[string]any) error {
		changes := []any{}
		if decision == "已采纳" {
			changes = diffTopLevel(beforeContent, record.WorkingContent)
		}
		_, err := s.process.AppendTx(ctx, tx, emrprocess.AppendRequest{
			TenantID: tenantID, RecordID: &recordID, ActionType: "AI" + decision,
			ActionResult: "成功", ActorType: "人工", ActorID: &actorID, Source: "病历详情",
			ActionNote: req.Note, AICandidateID: &candidate.ID, ContentChanges: changes,
		})
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return candidate, record, nil
}

func (s *Service) Create(ctx context.Context, tenantID, actorID int64, req CreateRequest) (*Record, error) {
	if strings.TrimSpace(req.TemplateVersionID) == "" {
		return s.createFailed(ctx, tenantID, actorID, "template_version_id is required")
	}
	if req.DocumentType != "" && req.DocumentType != "门诊病历" && req.DocumentType != "急诊病历" {
		return s.createFailed(ctx, tenantID, actorID, "invalid document_type")
	}
	if req.VisitType != "初诊" && req.VisitType != "复诊" {
		return s.createFailed(ctx, tenantID, actorID, "invalid visit_type")
	}
	record, err := s.store.Create(ctx, tenantID, actorID, req)
	if err != nil {
		_ = s.createFailedRecord(ctx, tenantID, actorID, err)
		return nil, err
	}
	recordID := record.ID
	if _, err := s.process.Append(ctx, emrprocess.AppendRequest{
		TenantID: tenantID, RecordID: &recordID, ActionType: "创建", ActionResult: "成功",
		ActorType: "人工", ActorID: &actorID, Source: "外部接口", AfterStatus: &record.Status,
	}); err != nil {
		return nil, fmt.Errorf("record create succeeded but process record failed: %w", err)
	}
	return record, nil
}

func (s *Service) createFailed(ctx context.Context, tenantID, actorID int64, reason string) (*Record, error) {
	err := errors.New(reason)
	_ = s.createFailedRecord(ctx, tenantID, actorID, err)
	return nil, err
}

func (s *Service) createFailedRecord(ctx context.Context, tenantID, actorID int64, cause error) error {
	_, err := s.process.Append(ctx, emrprocess.AppendRequest{
		TenantID: tenantID, ActionType: "创建", ActionResult: "失败", ActorType: "人工", ActorID: &actorID,
		Source: "外部接口", FailureReason: cause.Error(),
	})
	return err
}

func (s *Service) AutoSave(ctx context.Context, tenantID, actorID int64, id string, content map[string]any) (*Record, error) {
	return s.store.SaveWorking(ctx, tenantID, actorID, id, content)
}

func (s *Service) ManualSave(ctx context.Context, tenantID, actorID int64, id string, content map[string]any, note string) (*WriteOutcome, error) {
	if content == nil {
		current, err := s.store.Get(ctx, tenantID, id)
		if err != nil || current == nil {
			return nil, fmt.Errorf("record not found")
		}
		content = current.WorkingContent
	}
	outcome, err := s.store.SaveFormal(ctx, tenantID, actorID, id, content)
	if err != nil {
		return nil, err
	}
	run, checkErr := s.checks.Run(ctx, emrcheck.CheckRequest{TenantID: tenantID, RecordID: id, SnapshotID: outcome.Snapshot.ID, TriggerAction: "手动保存", StartedBy: &actorID})
	if checkErr == nil {
		outcome.CheckRunID = &run.ID
		outcome.CheckRun = run
	}
	processErr := s.appendProcess(ctx, tenantID, id, actorID, "手动保存", outcome, note, run, checkErr)
	if processErr != nil {
		return nil, processErr
	}
	return outcome, nil
}

func (s *Service) Submit(ctx context.Context, tenantID, actorID int64, id string, req ActionRequest) (*WriteOutcome, error) {
	return s.checkedTransition(ctx, tenantID, actorID, id, req, "提交", "待确认", false)
}

func (s *Service) Confirm(ctx context.Context, tenantID, actorID int64, id string, req ActionRequest) (*WriteOutcome, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	if current.Status != "待确认" {
		return nil, fmt.Errorf("record status %s cannot be confirmed", current.Status)
	}
	return s.checkedTransition(ctx, tenantID, actorID, id, req, "确认", "已确认", false)
}

func (s *Service) Archive(ctx context.Context, tenantID, actorID int64, id string, req ActionRequest) (*WriteOutcome, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	if current.Status != "已确认" {
		return nil, fmt.Errorf("record status %s cannot be archived", current.Status)
	}
	return s.checkedTransition(ctx, tenantID, actorID, id, req, "归档", "已归档", false)
}

func (s *Service) checkedTransition(ctx context.Context, tenantID, actorID int64, id string, req ActionRequest, action, target string, revisionIncrement bool) (*WriteOutcome, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	if !allowedTransition(current.Status, target) {
		return nil, fmt.Errorf("record status %s cannot transition to %s", current.Status, target)
	}
	content := req.Content
	if content == nil {
		content = current.WorkingContent
	}
	var outcome *WriteOutcome
	if action == "提交" {
		outcome, err = s.store.SaveFormal(ctx, tenantID, actorID, id, content)
		if err != nil {
			return nil, err
		}
	} else {
		if current.CurrentSnapshotID == nil {
			return nil, fmt.Errorf("record has no formal snapshot")
		}
		if digest(content) != current.WorkingDigest {
			return nil, fmt.Errorf("record has unsaved content; save it before %s", action)
		}
		snapshot, snapshotErr := s.store.GetSnapshot(ctx, tenantID, id, *current.CurrentSnapshotID)
		if snapshotErr != nil || snapshot == nil {
			return nil, fmt.Errorf("record snapshot not found")
		}
		outcome = &WriteOutcome{Record: current, Snapshot: snapshot, BeforeContent: current.WorkingContent, BeforeStatus: current.Status, AfterStatus: current.Status, BeforeSnapshot: current.CurrentSnapshotID}
	}
	run, checkErr := s.checks.Run(ctx, emrcheck.CheckRequest{TenantID: tenantID, RecordID: id, SnapshotID: outcome.Snapshot.ID, TriggerAction: action, StartedBy: &actorID})
	if checkErr != nil {
		_ = s.appendProcess(ctx, tenantID, id, actorID, action, outcome, req.Note, nil, checkErr)
		return nil, checkErr
	}
	outcome.CheckRunID = &run.ID
	outcome.CheckRun = run
	if run.OverallResult == "需要处理" {
		if action == "提交" {
			updated, transitionErr := s.store.Transition(ctx, tenantID, id, "需补全", actorID, nil, nil, false)
			if transitionErr != nil {
				return nil, transitionErr
			}
			outcome.Record = updated
			outcome.AfterStatus = "需补全"
		}
		if processErr := s.appendProcess(ctx, tenantID, id, actorID, action, outcome, req.Note, run, fmt.Errorf("检查结果需要处理")); processErr != nil {
			return nil, processErr
		}
		return outcome, fmt.Errorf("病历存在需要处理的质量问题")
	}
	updated, err := s.store.Transition(ctx, tenantID, id, target, actorID, snapshotID(target, outcome.Snapshot.ID), snapshotID(target, outcome.Snapshot.ID), revisionIncrement)
	if err != nil {
		return nil, err
	}
	outcome.Record = updated
	outcome.AfterStatus = target
	if err := s.appendProcess(ctx, tenantID, id, actorID, action, outcome, req.Note, run, nil); err != nil {
		return nil, err
	}
	return outcome, nil
}

func allowedTransition(from, to string) bool {
	switch {
	case from == "草稿" && to == "待确认":
		return true
	case from == "需补全" && to == "待确认":
		return true
	case from == "退回" && to == "待确认":
		return true
	case from == "待确认" && to == "已确认":
		return true
	case from == "已确认" && to == "已归档":
		return true
	default:
		return false
	}
}

func (s *Service) Revise(ctx context.Context, tenantID, actorID int64, id, note string) (*Record, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	if current.Status != "已确认" && current.Status != "已归档" {
		return nil, fmt.Errorf("record status %s cannot be revised", current.Status)
	}
	before := current.Status
	updated, err := s.store.Transition(ctx, tenantID, id, "草稿", actorID, nil, nil, before == "已归档")
	if err != nil {
		return nil, err
	}
	actionType := "确认失效"
	if before == "已归档" {
		actionType = "受控修订"
	}
	if _, err := s.process.Append(ctx, emrprocess.AppendRequest{TenantID: tenantID, RecordID: &id, ActionType: actionType, ActionResult: "成功", ActorType: "人工", ActorID: &actorID, Source: "外部接口", BeforeStatus: &before, AfterStatus: &updated.Status, BeforeSnapshotID: current.CurrentSnapshotID, AfterSnapshotID: current.CurrentSnapshotID, ActionNote: note}); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) Void(ctx context.Context, tenantID, actorID int64, id, note string) (*Record, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	updated, err := s.store.Void(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.process.Append(ctx, emrprocess.AppendRequest{TenantID: tenantID, RecordID: &id, ActionType: "作废", ActionResult: "成功", ActorType: "人工", ActorID: &actorID, Source: "外部接口", BeforeStatus: &current.Status, AfterStatus: &updated.Status, BeforeSnapshotID: current.CurrentSnapshotID, AfterSnapshotID: current.CurrentSnapshotID, ActionNote: note}); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) Snapshots(ctx context.Context, tenantID int64, id string) ([]*Snapshot, error) {
	return s.store.ListSnapshots(ctx, tenantID, id)
}

func (s *Service) appendProcess(ctx context.Context, tenantID int64, id string, actorID int64, action string, outcome *WriteOutcome, note string, run *emrcheck.CheckRun, cause error) error {
	result := "成功"
	failure := ""
	if cause != nil {
		result = "失败"
		failure = cause.Error()
	}
	var runID *string
	if run != nil {
		runID = &run.ID
	}
	changes := diffTopLevel(outcome.BeforeContent, outcome.Record.WorkingContent)
	_, err := s.process.Append(ctx, emrprocess.AppendRequest{TenantID: tenantID, RecordID: &id, ActionType: action, ActionResult: result, ActorType: "人工", ActorID: &actorID, Source: "外部接口", BeforeStatus: stringPtr(outcome.BeforeStatus), AfterStatus: stringPtr(outcome.AfterStatus), ActionSnapshotID: snapshotPtr(outcome.Snapshot), BeforeSnapshotID: outcome.BeforeSnapshot, AfterSnapshotID: snapshotPtr(outcome.Snapshot), CheckRunID: runID, FailureReason: failure, ActionNote: note, ContentChanges: changes, OutputInfo: map[string]any{"check_run_id": runID, "failure": failure}})
	return err
}

func diffTopLevel(before, after map[string]any) []any {
	changes := make([]any, 0)
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	for key := range keys {
		oldValue, oldOK := before[key]
		newValue, newOK := after[key]
		oldRaw, _ := json.Marshal(oldValue)
		newRaw, _ := json.Marshal(newValue)
		if oldOK != newOK || string(oldRaw) != string(newRaw) {
			changes = append(changes, map[string]any{"field": key, "before": oldValue, "after": newValue})
		}
	}
	return changes
}

func snapshotID(target, id string) *string {
	if target == "已确认" || target == "已归档" {
		return &id
	}
	return &id
}
func snapshotPtr(snapshot *Snapshot) *string {
	if snapshot == nil {
		return nil
	}
	return &snapshot.ID
}
func stringPtr(value string) *string { return &value }

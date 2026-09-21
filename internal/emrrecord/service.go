package emrrecord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrcheck"
	"github.com/freeasyman/lingce-api/internal/emrinput"
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

func (s *Service) Create(ctx context.Context, tenantID, actorID int64, req CreateRequest) (*Record, error) {
	if req.EncounterID <= 0 {
		return s.createFailed(ctx, tenantID, actorID, "encounter_id is required")
	}
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

func (s *Service) CreateFromHistoricalRecording(ctx context.Context, tenantID, actorID int64, req CreateRequest, input emrinput.Input) (*Record, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if req.EncounterID <= 0 {
		if input.EncounterID == nil || *input.EncounterID <= 0 {
			return s.createFailed(ctx, tenantID, actorID, "encounter_id is required")
		}
		req.EncounterID = *input.EncounterID
	}
	if req.PatientID == nil && input.Patient != nil && input.Patient.CustomerID != nil {
		req.PatientID = input.Patient.CustomerID
	}
	if req.PatientSnapshot == nil {
		req.PatientSnapshot = map[string]any{}
	}
	if len(req.EncounterContext) == 0 {
		req.EncounterContext = input.EncounterContext()
	}
	if len(req.SourceReferences) == 0 {
		req.SourceReferences = input.SourceReferences()
	}
	req.StandardInput = input.StandardInput()
	record, err := s.store.Create(ctx, tenantID, actorID, req)
	if err != nil {
		_ = s.createFailedRecord(ctx, tenantID, actorID, err)
		return nil, err
	}
	recordID := record.ID
	if _, err := s.process.Append(ctx, emrprocess.AppendRequest{
		TenantID: tenantID, RecordID: &recordID, ActionType: "历史导入", ActionResult: "成功",
		ActorType: "人工", ActorID: &actorID, Source: "外部接口", AfterStatus: &record.Status,
	}); err != nil {
		return nil, fmt.Errorf("record historical import succeeded but process record failed: %w", err)
	}
	return record, nil
}

func (s *Service) createFailed(ctx context.Context, tenantID, actorID int64, reason string) (*Record, error) {
	err := errors.New(reason)
	_ = s.createFailedRecord(ctx, tenantID, actorID, err)
	return nil, err
}

func (s *Service) createFailedRecord(ctx context.Context, tenantID, actorID int64, cause error) error {
	if s.process == nil {
		return nil
	}
	_, err := s.process.Append(ctx, emrprocess.AppendRequest{
		TenantID: tenantID, ActionType: "创建", ActionResult: "失败", ActorType: "人工", ActorID: &actorID,
		Source: "外部接口", FailureReason: cause.Error(),
	})
	return err
}

func (s *Service) AutoSave(ctx context.Context, tenantID, actorID int64, id string, content map[string]any) (*Record, error) {
	// 自动保存只更新工作稿，不生成正式快照，也不写入正式操作留痕。
	return s.store.SaveWorking(ctx, tenantID, actorID, id, content)
}

func (s *Service) ManualSave(ctx context.Context, tenantID, actorID int64, id string, content map[string]any, note string) (*WriteOutcome, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	if content == nil {
		content = current.WorkingContent
	}
	if err := validateContentChangeAudit(actorID, current.WorkingContent, content, note); err != nil {
		return nil, err
	}
	outcome, err := s.store.SaveFormal(ctx, tenantID, actorID, id, content)
	if err != nil {
		return nil, err
	}
	run, checkErr := s.checks.StartAsync(context.WithoutCancel(ctx), emrcheck.CheckRequest{
		TenantID: tenantID, RecordID: id, SnapshotID: outcome.Snapshot.ID,
		TriggerAction: "手动保存", StartedBy: &actorID,
	})
	if checkErr != nil {
		_ = s.appendProcess(ctx, tenantID, id, actorID, "手动保存", outcome, note, nil, checkErr)
		return nil, checkErr
	}
	outcome.CheckRunID = &run.ID
	outcome.CheckRun = run
	if processErr := s.appendProcess(ctx, tenantID, id, actorID, "手动保存", outcome, note, run, nil); processErr != nil {
		return nil, processErr
	}
	return outcome, nil
}

func (s *Service) Submit(ctx context.Context, tenantID, actorID int64, id string, req ActionRequest) (*WriteOutcome, error) {
	return s.checkedTransition(ctx, tenantID, actorID, id, req, "提交", "待确认", false)
}

// Complete performs the doctor-facing "完成病历" action.
// The internal audit trail remains two records: 提交 followed by 确认.
func (s *Service) Complete(ctx context.Context, tenantID, actorID int64, id string, req ActionRequest) (*WriteOutcome, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	if !allowedTransition(current.Status, "待确认") {
		return nil, fmt.Errorf("record status %s cannot be completed", current.Status)
	}
	content := req.Content
	if content == nil {
		content = current.WorkingContent
	}
	if err := validateContentChangeAudit(actorID, current.WorkingContent, content, req.Note); err != nil {
		return nil, err
	}
	outcome, err := s.store.SaveFormal(ctx, tenantID, actorID, id, content)
	if err != nil {
		return nil, err
	}

	checkCtx, cancel := qualityCheckContext(ctx)
	defer cancel()
	run, checkErr := s.checks.Run(checkCtx, emrcheck.CheckRequest{
		TenantID: tenantID, RecordID: id, SnapshotID: outcome.Snapshot.ID,
		TriggerAction: "提交", StartedBy: &actorID,
	})
	if checkErr != nil {
		_ = s.appendProcess(checkCtx, tenantID, id, actorID, "提交", outcome, req.Note, nil, checkErr)
		return nil, checkErr
	}
	outcome.CheckRunID = &run.ID
	outcome.CheckRun = run
	if run.OverallResult == "需要处理" {
		var updated *Record
		if transitionErr := s.store.WithTx(ctx, func(tx pgx.Tx) error {
			var err error
			updated, err = transitionTx(ctx, tx, tenantID, id, "需补全", actorID, nil, nil, false)
			if err != nil {
				return err
			}
			return s.appendProcessResultTx(ctx, tx, tenantID, id, actorID, "提交", outcome, req.Note, run, "阻断", "检查结果需要处理")
		}); transitionErr != nil {
			return nil, transitionErr
		}
		outcome.Record = updated
		outcome.AfterStatus = "需补全"
		return outcome, fmt.Errorf("病历存在需要处理的质量问题")
	}

	snapshotID := outcome.Snapshot.ID
	var updated *Record
	if err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		updated, err = transitionTx(ctx, tx, tenantID, id, "待确认", actorID, nil, nil, false)
		if err != nil {
			return err
		}
		outcome.Record = updated
		outcome.AfterStatus = "待确认"
		if err := s.appendProcessResultTx(ctx, tx, tenantID, id, actorID, "提交", outcome, req.Note, run, "成功", ""); err != nil {
			return err
		}
		updated, err = transitionTx(ctx, tx, tenantID, id, "已确认", actorID, &snapshotID, &snapshotID, false)
		if err != nil {
			return err
		}
		outcome.Record = updated
		outcome.AfterStatus = "已确认"
		confirmOutcome := *outcome
		confirmOutcome.BeforeContent = updated.WorkingContent
		confirmOutcome.BeforeStatus = "待确认"
		confirmOutcome.AfterStatus = "已确认"
		confirmOutcome.BeforeSnapshot = &snapshotID
		return s.appendProcessResultTx(ctx, tx, tenantID, id, actorID, "确认", &confirmOutcome, "", run, "成功", "")
	}); err != nil {
		return nil, err
	}
	outcome.Record = updated
	outcome.AfterStatus = "已确认"
	return outcome, nil
}

func (s *Service) Confirm(ctx context.Context, tenantID, actorID int64, id string, req ActionRequest) (*WriteOutcome, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	if current.Status != "待确认" {
		return nil, fmt.Errorf("record status %s cannot be confirmed", current.Status)
	}
	return s.confirmOrArchive(ctx, tenantID, actorID, current, "确认", "已确认", false)
}

func (s *Service) Archive(ctx context.Context, tenantID, actorID int64, id string, req ActionRequest) (*WriteOutcome, error) {
	current, err := s.store.Get(ctx, tenantID, id)
	if err != nil || current == nil {
		return nil, fmt.Errorf("record not found")
	}
	if current.Status != "已确认" {
		return nil, fmt.Errorf("record status %s cannot be archived", current.Status)
	}
	return s.confirmOrArchive(ctx, tenantID, actorID, current, "归档", "已归档", false)
}

func (s *Service) confirmOrArchive(ctx context.Context, tenantID, actorID int64, current *Record, action, target string, revisionIncrement bool) (*WriteOutcome, error) {
	if current.CurrentSnapshotID == nil || strings.TrimSpace(*current.CurrentSnapshotID) == "" {
		return nil, fmt.Errorf("病历还没有可确认的正式版本，请先保存并提交质检")
	}
	snapshot, err := s.store.GetSnapshot(ctx, tenantID, current.ID, *current.CurrentSnapshotID)
	if err != nil || snapshot == nil {
		return nil, fmt.Errorf("找不到当前病历版本，无法%s", action)
	}
	if snapshotDigest(current, current.WorkingContent) != snapshot.ContentDigest {
		return nil, fmt.Errorf("病历内容在质检后发生了修改，请先保存并重新提交质检")
	}
	run, err := s.checks.LatestCompletedForSnapshot(ctx, tenantID, current.ID, snapshot.ID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("当前病历版本尚未完成质检，暂不能%s", action)
	}
	if run.OverallResult == "需要处理" || run.OverallResult == "无法完成" {
		return nil, fmt.Errorf("当前病历版本的质检尚未通过，暂不能%s", action)
	}
	for _, result := range run.Results {
		if result != nil && result.HandlingResult == "阻断" {
			return nil, fmt.Errorf("当前病历版本仍有需要处理的质检问题，暂不能%s", action)
		}
	}
	snapshotID := snapshot.ID
	updated, err := s.store.Transition(ctx, tenantID, current.ID, target, actorID, &snapshotID, &snapshotID, revisionIncrement)
	if err != nil {
		return nil, err
	}
	outcome := &WriteOutcome{
		Record: updated, Snapshot: snapshot, BeforeContent: current.WorkingContent,
		BeforeStatus: current.Status, AfterStatus: target,
		BeforeSnapshot: current.CurrentSnapshotID,
		CheckRunID:     &run.ID, CheckRun: run,
	}
	if err := s.appendProcess(ctx, tenantID, current.ID, actorID, action, outcome, "", run, nil); err != nil {
		return nil, err
	}
	return outcome, nil
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
	if err := validateContentChangeAudit(actorID, current.WorkingContent, content, req.Note); err != nil {
		return nil, err
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
	checkCtx, cancel := qualityCheckContext(ctx)
	defer cancel()
	run, checkErr := s.checks.Run(checkCtx, emrcheck.CheckRequest{TenantID: tenantID, RecordID: id, SnapshotID: outcome.Snapshot.ID, TriggerAction: action, StartedBy: &actorID})
	if checkErr != nil {
		_ = s.appendProcess(checkCtx, tenantID, id, actorID, action, outcome, req.Note, nil, checkErr)
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
		if processErr := s.appendProcessResult(checkCtx, tenantID, id, actorID, action, outcome, req.Note, run, "阻断", "检查结果需要处理"); processErr != nil {
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
	if err := s.appendProcess(checkCtx, tenantID, id, actorID, action, outcome, req.Note, run, nil); err != nil {
		return nil, err
	}
	return outcome, nil
}

func qualityCheckContext(ctx context.Context) (context.Context, context.CancelFunc) {
	// Quality checks may outlive the browser request. Keep the formal run
	// durable while retaining a bounded upper limit for model calls and writes.
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute+30*time.Second)
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
	return s.appendProcessResult(ctx, tenantID, id, actorID, action, outcome, note, run, result, failure)
}

func (s *Service) appendProcessResult(ctx context.Context, tenantID int64, id string, actorID int64, action string, outcome *WriteOutcome, note string, run *emrcheck.CheckRun, result, failure string) error {
	return s.appendProcessResultWithAppender(ctx, tenantID, id, actorID, action, outcome, note, run, result, failure, func(req emrprocess.AppendRequest) error {
		_, err := s.process.Append(ctx, req)
		return err
	})
}

func (s *Service) appendProcessResultTx(ctx context.Context, tx pgx.Tx, tenantID int64, id string, actorID int64, action string, outcome *WriteOutcome, note string, run *emrcheck.CheckRun, result, failure string) error {
	return s.appendProcessResultWithAppender(ctx, tenantID, id, actorID, action, outcome, note, run, result, failure, func(req emrprocess.AppendRequest) error {
		_, err := s.process.AppendTx(ctx, tx, req)
		return err
	})
}

func (s *Service) appendProcessResultWithAppender(ctx context.Context, tenantID int64, id string, actorID int64, action string, outcome *WriteOutcome, note string, run *emrcheck.CheckRun, result, failure string, appendRecord func(emrprocess.AppendRequest) error) error {
	var runID *string
	if run != nil {
		runID = &run.ID
	}
	changes := diffTopLevel(outcome.BeforeContent, outcome.Record.WorkingContent)
	if err := validateAuditTrail(actorID, note, outcome, changes); err != nil {
		return err
	}
	return appendRecord(emrprocess.AppendRequest{TenantID: tenantID, RecordID: &id, ActionType: action, ActionResult: result, ActorType: "人工", ActorID: &actorID, Source: "外部接口", BeforeStatus: stringPtr(outcome.BeforeStatus), AfterStatus: stringPtr(outcome.AfterStatus), ActionSnapshotID: snapshotPtr(outcome.Snapshot), BeforeSnapshotID: outcome.BeforeSnapshot, AfterSnapshotID: snapshotPtr(outcome.Snapshot), CheckRunID: runID, FailureReason: failure, ActionNote: note, ContentChanges: changes, OutputInfo: map[string]any{"check_run_id": runID, "failure": failure}})
}

func diffTopLevel(before, after map[string]any) []any {
	changes := make([]any, 0)
	keys := make(map[string]bool, len(before)+len(after))
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)
	for _, key := range orderedKeys {
		oldValue, oldOK := before[key]
		newValue, newOK := after[key]
		oldRaw, _ := json.Marshal(oldValue)
		newRaw, _ := json.Marshal(newValue)
		if oldOK && newOK && string(oldRaw) == string(newRaw) {
			continue
		}
		changeType := "修改"
		if !oldOK {
			changeType = "新增"
		} else if !newOK {
			changeType = "删除"
		}
		var beforeValue, afterValue any
		if oldOK {
			beforeValue = oldValue
		}
		if newOK {
			afterValue = newValue
		}
		changes = append(changes, map[string]any{
			"section_code": key,
			"change_type":  changeType,
			"before":       beforeValue,
			"after":        afterValue,
		})
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

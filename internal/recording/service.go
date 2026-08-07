package recording

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/employee"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	authpkg "github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
)

type OpportunityAlertDispatcher interface {
	CreateFromRecording(ctx context.Context, recordingID int64, triggerSource string) error
}

type Service struct {
	store                      *Store
	employeeStore              *employee.Store
	workerURL                  string
	workerToken                string
	lingceWorkerURL            string
	lingceWorkerToken          string
	resetCodeDictionaryPath    string
	httpClient                 *http.Client
	llmClient                  *llmgateway.Client
	opportunityAlertDispatcher OpportunityAlertDispatcher
}

type workerUnavailableError struct {
	cause error
}

func (e *workerUnavailableError) Error() string {
	if e == nil || e.cause == nil {
		return "recording worker unavailable"
	}
	return fmt.Sprintf("recording worker unavailable: %v", e.cause)
}

func (e *workerUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type recordingValidationError struct {
	code    string
	message string
}

func (e *recordingValidationError) Error() string {
	if e == nil {
		return "recording validation failed"
	}
	return e.message
}

func (e *recordingValidationError) Code() string {
	if e == nil || strings.TrimSpace(e.code) == "" {
		return "BAD_REQUEST"
	}
	return e.code
}

func IsRecordingValidationError(err error) bool {
	var validationErr *recordingValidationError
	return errors.As(err, &validationErr)
}

// IsWorkerUnavailable indicates whether an error is caused by recording-worker unavailability.
func IsWorkerUnavailable(err error) bool {
	var unavailable *workerUnavailableError
	return errors.As(err, &unavailable)
}

func NewService(store *Store, employeeStore *employee.Store, workerURL, workerToken, lingceWorkerURL, lingceWorkerToken, resetCodeDictionaryPath string, llmClient *llmgateway.Client, opportunityAlertDispatcher OpportunityAlertDispatcher) *Service {
	return &Service{
		store:                      store,
		employeeStore:              employeeStore,
		workerURL:                  strings.TrimRight(workerURL, "/"),
		workerToken:                workerToken,
		lingceWorkerURL:            strings.TrimRight(lingceWorkerURL, "/"),
		lingceWorkerToken:          lingceWorkerToken,
		resetCodeDictionaryPath:    strings.TrimSpace(resetCodeDictionaryPath),
		llmClient:                  llmClient,
		opportunityAlertDispatcher: opportunityAlertDispatcher,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *Service) IngestOwnedAudioAndEnqueue(ctx context.Context, req OwnedAudioIngestRequest) (*RecordingResponse, bool, error) {
	if err := tenancy.RequirePositiveID("tenant_id", req.TenantID); err != nil {
		return nil, false, err
	}
	if err := tenancy.RequirePositiveID("employee_id", req.EmployeeID); err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(req.FileURL) == "" {
		return nil, false, fmt.Errorf("file_url is required")
	}
	if strings.TrimSpace(req.BusinessScope) == "" {
		scope, err := s.store.ResolveEmployeeBusinessScope(ctx, req.TenantID, req.EmployeeID)
		if err != nil {
			return nil, false, err
		}
		req.BusinessScope = scope
	}
	if strings.TrimSpace(req.Scene) == "" {
		req.Scene = "consultation"
	}
	if strings.TrimSpace(req.Source) == "" {
		req.Source = "manual"
	}
	if strings.TrimSpace(req.MIMEType) == "" {
		req.MIMEType = "audio/mpeg"
	}
	if strings.TrimSpace(req.FileName) == "" {
		req.FileName = "recording.audio"
	}

	recordingID, created, err := s.store.CreateOwnedAudioRecording(ctx, req)
	if err != nil {
		return nil, false, err
	}
	recording, err := s.store.GetRecordingByID(ctx, recordingID)
	if err != nil {
		return nil, created, err
	}
	resp := toRecordingResponse(recording)
	if created {
		triggerSource := strings.TrimSpace(req.TriggerSource)
		if triggerSource == "" {
			triggerSource = "external_audio_ingest"
		}
		if _, err := s.enqueueLingceWorkerJob(ctx, recordingID, "transcribe", triggerSource); err != nil {
			return resp, true, err
		}
	}
	return resp, created, nil
}

func recordingScopeMenuCode(scope RecordingScope) string {
	switch scope {
	case RecordingScopeDoctor:
		return "doctor_recordings"
	case RecordingScopeConsultant:
		return "consultant_recordings"
	case RecordingScopeFrontdesk:
		return "frontdesk_recordings"
	case RecordingScopeTherapist:
		return "therapist_recordings"
	default:
		return ""
	}
}

func businessScopeMenuCode(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "doctor":
		return "doctor_recordings"
	case "consultant":
		return "consultant_recordings"
	case "frontdesk", "reception", "receptionist", "customer_service", "service":
		return "frontdesk_recordings"
	case "therapist":
		return "therapist_recordings"
	default:
		return ""
	}
}

func (s *Service) ValidateRecordingScopeAccess(ctx context.Context, userType authpkg.UserType, userID int64, scope *RecordingScope) error {
	if scope == nil {
		return nil
	}
	if userType != authpkg.UserTypeEmployee && userType != authpkg.UserTypeMobile {
		return nil
	}

	menuCode := recordingScopeMenuCode(*scope)
	if menuCode == "" {
		return nil
	}

	allowed, err := s.store.EmployeeHasInstitutionMenuAccess(ctx, userID, menuCode)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("recording scope %s requires menu %s", *scope, menuCode)
	}
	return nil
}

func (s *Service) ValidateBusinessScopeAccess(ctx context.Context, userType authpkg.UserType, userID int64, businessScope string) error {
	if userType != authpkg.UserTypeEmployee && userType != authpkg.UserTypeMobile {
		return nil
	}

	menuCode := businessScopeMenuCode(businessScope)
	if menuCode == "" {
		return nil
	}

	allowed, err := s.store.EmployeeHasInstitutionMenuAccess(ctx, userID, menuCode)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("business scope %s requires menu %s", strings.TrimSpace(businessScope), menuCode)
	}
	return nil
}

// ListRecordings retrieves a paginated list of medical recordings
func (s *Service) ListRecordings(ctx context.Context, req RecordingListRequest) ([]*RecordingResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	recordings, total, err := s.store.ListRecordings(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*RecordingResponse, len(recordings))
	for i, r := range recordings {
		resp := toRecordingResponse(r)
		responses[i] = resp
	}
	if err := s.backfillListChiefComplaintFromEMR(ctx, responses); err != nil {
		slog.Warn("failed to backfill chief complaint for recording list", "error", err)
	}
	for i := range responses {
		compactRecordingListItem(responses[i])
	}

	return responses, total, nil
}

func (s *Service) backfillListChiefComplaintFromEMR(ctx context.Context, responses []*RecordingResponse) error {
	if len(responses) == 0 {
		return nil
	}
	needIDs := make([]int64, 0, len(responses))
	for _, resp := range responses {
		if resp == nil || resp.ID <= 0 {
			continue
		}
		if resp.ChiefComplaint != nil && strings.TrimSpace(*resp.ChiefComplaint) != "" {
			continue
		}
		needIDs = append(needIDs, resp.ID)
	}
	if len(needIDs) == 0 {
		return nil
	}

	rows, err := s.store.pool.Query(ctx, `
		SELECT recording_id, COALESCE(emr_content::jsonb, '{}'::jsonb)
		FROM recording_emr_drafts
		WHERE recording_id = ANY($1::bigint[])
	`, needIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	byID := make(map[int64]map[string]interface{}, len(needIDs))
	for rows.Next() {
		var recordingID int64
		var emrContent JSONObject
		if scanErr := rows.Scan(&recordingID, &emrContent); scanErr != nil {
			return scanErr
		}
		byID[recordingID] = map[string]interface{}(emrContent)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, resp := range responses {
		if resp == nil || resp.ID <= 0 {
			continue
		}
		if resp.ChiefComplaint != nil && strings.TrimSpace(*resp.ChiefComplaint) != "" {
			continue
		}
		content, ok := byID[resp.ID]
		if !ok || content == nil {
			continue
		}
		chiefComplaint := pickString(content, "chief_complaint")
		if chiefComplaint == "" {
			chiefComplaint = pickString(content, "chiefComplaint")
		}
		if chiefComplaint != "" {
			resp.ChiefComplaint = literalStringPtr(chiefComplaint)
		}
	}

	return nil
}

func compactRecordingListItem(resp *RecordingResponse) {
	if resp == nil {
		return
	}
	// Preserve summary from analysis_result before clearing it.
	if resp.AnalysisResult != nil {
		if s, ok := resp.AnalysisResult["summary"].(string); ok && s != "" && resp.ConversationSummary == nil {
			resp.ConversationSummary = &s
		}
	}
	resp.AnalysisResult = compactRecordingListAnalysisResult(resp.AnalysisResult)
	resp.TranscriptText = nil
	resp.DoctorSummary = nil
	resp.TherapistSummary = nil
	resp.ConsultantSummary = nil
	resp.AnalysisSummary = nil
	resp.AnalysisDisplay = nil
	resp.StructuredTranscript = nil
	resp.TimelineTranscript = nil
	resp.ContentSeeds = nil
	resp.RouteReview = nil
	resp.EMRDraft = nil
	resp.ConsultationRecord = nil
	resp.DealOutcome = nil
	resp.SuggestedTask = nil
}

func compactRecordingListAnalysisResult(source map[string]interface{}) map[string]interface{} {
	if len(source) == 0 {
		return nil
	}

	out := make(map[string]interface{})
	copyStringField := func(key string) {
		if value := pickString(source, key); value != "" {
			out[key] = value
		}
	}
	copyAnyField := func(key string) {
		if value, ok := source[key]; ok && value != nil {
			out[key] = value
		}
	}

	copyStringField("summary")
	copyStringField("conversation_summary")
	copyStringField("status_summary")
	copyStringField("relationship_frame")
	copyStringField("scene_type_label")
	copyStringField("intent_amount")
	copyStringField("intent_project")
	if doctorPatientView := pickMap(source, "doctor_patient_view"); len(doctorPatientView) > 0 {
		slimDoctorPatientView := make(map[string]interface{})
		if value := pickString(doctorPatientView, "relationship_frame"); value != "" {
			slimDoctorPatientView["relationship_frame"] = value
		}
		if value := pickString(doctorPatientView, "summary"); value != "" {
			slimDoctorPatientView["summary"] = value
		}
		if len(slimDoctorPatientView) > 0 {
			out["doctor_patient_view"] = slimDoctorPatientView
		}
	}

	copyAnyField("visit_outcome")
	copyAnyField("decision_status")
	copyAnyField("visit_outcome_status")

	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) ListManagementEvents(
	ctx context.Context,
	tenantID int64,
	roleType string,
	dimensionCode string,
	period string,
	includeFuture bool,
) ([]ManagementEvent, error) {
	var startDate *time.Time
	var endDate *time.Time
	now := time.Now()
	period = strings.TrimSpace(period)
	switch period {
	case "1m":
		d := now.AddDate(0, -1, 0)
		startDate = &d
		if !includeFuture {
			endDate = &now
		}
	case "6m":
		d := now.AddDate(0, -6, 0)
		startDate = &d
		if !includeFuture {
			endDate = &now
		}
	case "3m", "":
		d := now.AddDate(0, -3, 0)
		startDate = &d
		if !includeFuture {
			endDate = &now
		}
	default:
		// fallback to 3m for unknown values
		d := now.AddDate(0, -3, 0)
		startDate = &d
		if !includeFuture {
			endDate = &now
		}
	}
	return s.store.ListManagementEvents(ctx, tenantID, roleType, dimensionCode, startDate, endDate)
}

func (s *Service) CreateManagementEvent(ctx context.Context, tenantID int64, createdBy int64, req CreateManagementEventRequest) (*ManagementEvent, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.EventDate) == "" {
		return nil, fmt.Errorf("event_date is required")
	}
	if strings.TrimSpace(req.EventType) == "" {
		return nil, fmt.Errorf("event_type is required")
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	if strings.TrimSpace(req.RoleType) == "" {
		return nil, fmt.Errorf("role_type is required")
	}
	if strings.TrimSpace(req.DimensionCode) == "" {
		return nil, fmt.Errorf("dimension_code is required")
	}
	return s.store.CreateManagementEvent(ctx, tenantID, createdBy, req)
}

func (s *Service) GetManagementRisks(ctx context.Context, tenantID int64, period string, status string) (*ManagementRisksResponse, error) {
	startAt, endAt, normalizedPeriod := parseManagementPeriod(period)
	handledRiskIDs, _ := s.listHandledRiskIDs(ctx, tenantID)
	showHandledOnly := strings.EqualFold(strings.TrimSpace(status), "handled")
	showAll := strings.EqualFold(strings.TrimSpace(status), "all")

	sections := []ManagementRiskSection{
		{Key: "high_risk", Title: "高风险", Items: []ManagementRiskCard{}},
		{Key: "attention", Title: "需关注", Items: []ManagementRiskCard{}},
		{Key: "ops_health", Title: "运营健康度", Items: []ManagementRiskCard{}},
		{Key: "positive", Title: "正向变化", Items: []ManagementRiskCard{}},
		{Key: "benchmark_discovery", Title: "标杆发现", Items: []ManagementRiskCard{}},
	}

	// 1) 高风险：前台录音命中风险关键词
	frontdeskRows, err := s.listFrontdeskRiskRows(ctx, tenantID, startAt, endAt, 30)
	if err != nil {
		return nil, err
	}
	frontdeskRows = dedupeFrontdeskRiskRows(frontdeskRows)
	for _, row := range frontdeskRows {
		_, isHandled := handledRiskIDs[row.RiskID]
		if showHandledOnly && !isHandled {
			continue
		}
		if !showHandledOnly && !showAll && isHandled {
			continue
		}
		if row.RiskLevel != "high" && row.RiskLevel != "medium" {
			continue
		}
		priority := 10
		titlePrefix := "前台录音命中合规高风险"
		confidence := "medium"
		if row.RiskLevel == "medium" {
			priority = 5
			titlePrefix = "可疑风险（待复核）"
			confidence = "low"
		}
		evidence := []string{fmt.Sprintf("命中词：%s", row.HitKeyword), row.Snippet}
		description := fmt.Sprintf("%s · 前台 %s · 录音 #%d", row.RecordedAt, row.EmployeeName, row.RecordingID)
		if row.DuplicateCount > 1 {
			description = fmt.Sprintf("%s · 涉及 %d 条相似录音", description, row.DuplicateCount)
			evidence = append(evidence, fmt.Sprintf("已合并同类事件，示例录音：#%d", row.RecordingID))
		}
		cardTitle := fmt.Sprintf("%s：%s", titlePrefix, row.RiskLabel)
		if isHandled {
			cardTitle = "已处理 · " + cardTitle
		}
		sections[0].Items = append(sections[0].Items, ManagementRiskCard{
			ID:              row.RiskID,
			Type:            "high_risk",
			Priority:        priority,
			Title:           cardTitle,
			Description:     description,
			Evidence:        evidence,
			RoleCode:        "frontdesk",
			EmployeeID:      row.EmployeeID,
			EmployeeName:    row.EmployeeName,
			RecordingID:     row.RecordingID,
			Confidence:      confidence,
			SuggestedAction: "查看录音",
			SecondaryAction: "标记已处理",
			CreatedAt:       row.RecordedAt,
		})
	}

	// 2) 需关注：能力（均值-1σ且连续2周） + 任务超时
	abilityAlerts, err := s.detectAbilityAttentionBySigma(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, alert := range abilityAlerts {
		gap := roundRisk(alert.CurrentMean - alert.CurrentScore)
		if gap < 0 {
			gap = 0
		}
		benchmarkRecordingID := int64(0)
		accepted, _, _ := s.store.ListBenchmarkClips(ctx, tenantID, "accepted", "", alert.RoleCode, alert.DimensionCode, "", 1, 1)
		if len(accepted) > 0 {
			benchmarkRecordingID = accepted[0].RecordingID
		}
		suggestText := "建议：安排1对1辅导"
		if benchmarkRecordingID > 0 {
			suggestText = fmt.Sprintf("建议：安排1对1辅导，用标杆录音 #%d 做示范", benchmarkRecordingID)
		}
		sections[1].Items = append(sections[1].Items, ManagementRiskCard{
			ID:              fmt.Sprintf("ability:%s:%d:%s", alert.RoleCode, alert.EmployeeID, alert.DimensionCode),
			Type:            "attention_ability",
			Priority:        8,
			Title:           fmt.Sprintf("%s（%s）本周%s均分 %.1f，低于团队均值 %.1f", alert.EmployeeName, roleLabelForRisk(alert.RoleCode), alert.DimensionLabel, alert.CurrentScore, gap),
			Description:     "连续2周未改善",
			Evidence:        []string{suggestText},
			RoleCode:        alert.RoleCode,
			EmployeeID:      alert.EmployeeID,
			EmployeeName:    alert.EmployeeName,
			DimensionCode:   alert.DimensionCode,
			Score:           alert.CurrentScore,
			Confidence:      "",
			SuggestedAction: "查看她的录音",
			SecondaryAction: "创建辅导任务",
			TertiaryAction:  "查看标杆",
		})
	}
	taskStats, _ := s.GetTaskStats(ctx, tenantID, nil, nil)
	overdueDetails, _ := s.listOverdueTaskAttentionRows(ctx, tenantID, 3)
	if taskStats != nil && taskStats.OverdueTasks > 0 {
		evidence := []string{"存在超时未执行任务，可能导致跟进节奏断裂"}
		for _, d := range overdueDetails {
			evidence = append(evidence, fmt.Sprintf("患者%s → %s · 已超时%d小时", d.CustomerName, d.AssigneeName, d.OverdueHours))
		}
		sections[1].Items = append(sections[1].Items, ManagementRiskCard{
			ID:              "task-overdue",
			Type:            "attention_task",
			Priority:        7,
			Title:           fmt.Sprintf("跟进任务超时 %d 条", taskStats.OverdueTasks),
			Description:     fmt.Sprintf("待处理 %d 条，已完成 %d 条", taskStats.PendingTasks, taskStats.CompletedTasks),
			Evidence:        evidence,
			Confidence:      confidenceBySamples(taskStats.TotalTasks),
			SuggestedAction: "查看任务详情",
		})
	}

	// 3) 运营健康度：未关联/未执行/未标记比例
	totalCount, linkedCount, dealTaggedCount, err := s.getOpsHealthCounts(ctx, tenantID, startAt, endAt)
	if err != nil {
		return nil, err
	}
	unlinkedCount := totalCount - linkedCount
	if unlinkedCount < 0 {
		unlinkedCount = 0
	}
	unlinkedRate := 0.0
	dealUnmarkedRate := 0.0
	if totalCount > 0 {
		unlinkedRate = roundRisk(float64(unlinkedCount) / float64(totalCount) * 100)
		dealUnmarkedRate = roundRisk(float64(totalCount-dealTaggedCount) / float64(totalCount) * 100)
	}
	periodLabel := "本周"
	if normalizedPeriod == "2w" {
		periodLabel = "近2周"
	} else if normalizedPeriod == "4w" {
		periodLabel = "近4周"
	}
	dealPeriodLabel := "本月"
	sections[2].Items = append(sections[2].Items, ManagementRiskCard{
		ID:          "ops-health-customer",
		Type:        "ops_health",
		Priority:    6,
		Title:       fmt.Sprintf("%s %d 条录音未关联客户（占比 %.1f%%）", periodLabel, unlinkedCount, unlinkedRate),
		Description: "无法追踪转化结果，建议要求相关员工当天完成关联",
		Evidence:    []string{fmt.Sprintf("%s录音 %d 条，已关联 %d 条", periodLabel, totalCount, linkedCount)},
		Confidence:  confidenceBySamples(totalCount),
	})
	if taskStats != nil {
		overdueRate := 0.0
		if taskStats.TotalTasks > 0 {
			overdueRate = roundRisk(float64(taskStats.OverdueTasks) / float64(taskStats.TotalTasks) * 100)
		}
		sections[2].Items = append(sections[2].Items, ManagementRiskCard{
			ID:          "ops-health-task",
			Type:        "ops_health",
			Priority:    6,
			Title:       fmt.Sprintf("%s %d 条跟进任务未执行（超时率 %.1f%%）", periodLabel, taskStats.OverdueTasks, overdueRate),
			Description: "跟进节奏断裂，建议在早会上通报",
			Evidence:    []string{fmt.Sprintf("%s总任务 %d，超时 %d，已完成 %d", periodLabel, taskStats.TotalTasks, taskStats.OverdueTasks, taskStats.CompletedTasks)},
			Confidence:  confidenceBySamples(taskStats.TotalTasks),
		})
	}
	unmarkedDeals := totalCount - dealTaggedCount
	if unmarkedDeals < 0 {
		unmarkedDeals = 0
	}
	sections[2].Items = append(sections[2].Items, ManagementRiskCard{
		ID:          "ops-health-deal",
		Type:        "ops_health",
		Priority:    6,
		Title:       fmt.Sprintf("%s %d 条成交未标记（占比 %.1f%%）", dealPeriodLabel, unmarkedDeals, dealUnmarkedRate),
		Description: "无法计算转化率，建议建立成交登记流程",
		Evidence:    []string{fmt.Sprintf("%s录音 %d 条，已标记成交结果 %d 条", dealPeriodLabel, totalCount, dealTaggedCount)},
		Confidence:  confidenceBySamples(totalCount),
	})

	// 4) 正向变化：某人某维度本周 > 上周 + 0.3（按周聚合）
	positiveAlerts, _ := s.detectPositiveChangesByWeek(ctx, tenantID, 5)
	for _, item := range positiveAlerts {
		sections[3].Items = append(sections[3].Items, ManagementRiskCard{
			ID:            fmt.Sprintf("positive:%s:%d:%s", item.RoleCode, item.EmployeeID, item.DimensionCode),
			Type:          "positive_change",
			Priority:      4,
			Title:         fmt.Sprintf("%s（%s）%s本周 %s，较上周 %s", item.EmployeeName, roleLabelForRisk(item.RoleCode), item.DimensionLabel, positiveScoreText(item.RoleCode, item.CurrentScore), positiveDeltaText(item.RoleCode, item.Delta)),
			Description:   "",
			Evidence:      []string{fmt.Sprintf("上周 %s → 本周 %s", positiveScoreText(item.RoleCode, item.PreviousScore), positiveScoreText(item.RoleCode, item.CurrentScore))},
			RoleCode:      item.RoleCode,
			EmployeeID:    item.EmployeeID,
			EmployeeName:  item.EmployeeName,
			DimensionCode: item.DimensionCode,
			Score:         item.CurrentScore,
			Confidence:    "medium",
		})
	}

	// 5) 标杆发现：按页面时间范围筛选新出现的高分片段
	pending, _, _ := s.store.ListBenchmarkClips(ctx, tenantID, "pending", "auto", "", "", "", 1, 200)
	discoveryItems := make([]BenchmarkClip, 0, 32)
	for _, item := range pending {
		createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.CreatedAt))
		if err != nil {
			continue
		}
		if createdAt.Before(startAt) || !createdAt.Before(endAt) {
			continue
		}
		if !isDiscoveryScoreQualified(item.RoleCode, item.Score) {
			continue
		}
		discoveryItems = append(discoveryItems, item)
	}
	discoveryPendingTotal := len(discoveryItems)
	discoveryHandledTotal := 0
	acceptedAuto, _, _ := s.store.ListBenchmarkClips(ctx, tenantID, "accepted", "auto", "", "", "", 1, 200)
	for _, item := range acceptedAuto {
		createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.CreatedAt))
		if err != nil || createdAt.Before(startAt) || !createdAt.Before(endAt) {
			continue
		}
		if !isDiscoveryScoreQualified(item.RoleCode, item.Score) {
			continue
		}
		discoveryHandledTotal++
	}
	rejectedAuto, _, _ := s.store.ListBenchmarkClips(ctx, tenantID, "rejected", "auto", "", "", "", 1, 200)
	for _, item := range rejectedAuto {
		createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(item.CreatedAt))
		if err != nil || createdAt.Before(startAt) || !createdAt.Before(endAt) {
			continue
		}
		if !isDiscoveryScoreQualified(item.RoleCode, item.Score) {
			continue
		}
		discoveryHandledTotal++
	}
	sort.Slice(discoveryItems, func(i, j int) bool {
		li, lj := discoveryItems[i], discoveryItems[j]
		ti, ei := time.Parse(time.RFC3339, strings.TrimSpace(li.CreatedAt))
		tj, ej := time.Parse(time.RFC3339, strings.TrimSpace(lj.CreatedAt))
		if ei == nil && ej == nil && !ti.Equal(tj) {
			return ti.After(tj)
		}
		return li.ID > lj.ID
	})
	if len(discoveryItems) > 5 {
		discoveryItems = discoveryItems[:5]
	}
	for _, item := range discoveryItems {
		dimensionLabel := displayDimensionLabel(item.RoleCode, item.Dimension)
		if strings.TrimSpace(item.RoleCode) == string(RecordingScopeDoctor) || strings.TrimSpace(item.RoleCode) == "doctor" {
			dimensionLabel = fmt.Sprintf("%s%s", strings.TrimSpace(item.Dimension), discoveryDoctorShortLabel(strings.TrimSpace(item.Dimension)))
		}
		reason := buildDiscoveryReason(item.RoleCode, dimensionLabel, item.Score)
		if rec, recErr := s.store.GetRecordingByID(ctx, item.RecordingID); recErr == nil && rec != nil {
			analysis := map[string]interface{}(rec.AnalysisResult)
			evidenceList := extractDimensionEvidence(analysis, item.RoleCode, item.Dimension)
			reason = buildDiscoveryReasonFromEvidence(item.RoleCode, dimensionLabel, item.Score, evidenceList, item.ClipText)
		}
		sections[4].Items = append(sections[4].Items, ManagementRiskCard{
			ID:              fmt.Sprintf("benchmark-discovery-%d", item.ID),
			Type:            "benchmark_discovery",
			Priority:        3,
			Title:           fmt.Sprintf("录音 #%d %s %s %s", item.RecordingID, strings.TrimSpace(item.EmployeeName), strings.TrimSpace(dimensionLabel), discoveryScoreText(item.RoleCode, item.Score)),
			Description:     "",
			Evidence:        []string{fmt.Sprintf("推荐理由：%s", reason), fmt.Sprintf("“%s”", truncateText(item.ClipText, 56))},
			RoleCode:        item.RoleCode,
			EmployeeID:      item.EmployeeID,
			EmployeeName:    item.EmployeeName,
			RecordingID:     item.RecordingID,
			DimensionCode:   strings.TrimSpace(item.Dimension),
			Score:           item.Score,
			Confidence:      "medium",
			SuggestedAction: "加入标杆库",
			SecondaryAction: "在早会上讲评",
			TertiaryAction:  "忽略",
		})
	}
	sections[4].Title = fmt.Sprintf("标杆发现（待处理 %d 条，已处理 %d 条）", discoveryPendingTotal, discoveryHandledTotal)

	return &ManagementRisksResponse{
		Period:   normalizedPeriod,
		Sections: sections,
	}, nil
}

type frontdeskRiskRow struct {
	RiskID         string
	RecordingID    int64
	EmployeeID     int64
	EmployeeName   string
	RecordedAt     string
	Snippet        string
	RiskLabel      string
	RiskLevel      string
	HitKeyword     string
	Fingerprint    string
	DuplicateCount int
}

func (s *Service) listFrontdeskRiskRows(ctx context.Context, tenantID int64, startAt, endAt time.Time, limit int) ([]frontdeskRiskRow, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT r.id,
		       COALESCE(r.employee_id, 0),
		       COALESCE(e.name, '未命名员工'),
		       to_char(COALESCE(r.recorded_at, r.created_at), 'YYYY-MM-DD HH24:MI'),
		       COALESCE(
		         CASE
		           WHEN r.cleaned_transcription IS NULL THEN ''
		           WHEN jsonb_typeof(r.cleaned_transcription) = 'string' THEN trim(both '"' from r.cleaned_transcription::text)
		           WHEN jsonb_typeof(r.cleaned_transcription) = 'object' THEN COALESCE(r.cleaned_transcription->>'full_text', r.cleaned_transcription->>'text', '')
		           ELSE ''
		         END,
		         COALESCE(r.transcription_text, '')
		       )
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND r.business_scope = 'frontdesk'
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		  AND COALESCE(
		        CASE
		          WHEN r.cleaned_transcription IS NULL THEN ''
		          WHEN jsonb_typeof(r.cleaned_transcription) = 'string' THEN trim(both '"' from r.cleaned_transcription::text)
		          WHEN jsonb_typeof(r.cleaned_transcription) = 'object' THEN COALESCE(r.cleaned_transcription->>'full_text', r.cleaned_transcription->>'text', '')
		          ELSE ''
		        END,
		        COALESCE(r.transcription_text, '')
		      ) <> ''
		ORDER BY COALESCE(r.recorded_at, r.created_at) DESC
		LIMIT $4
	`, tenantID, startAt, endAt, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]frontdeskRiskRow, 0, limit)
	for rows.Next() {
		var it frontdeskRiskRow
		var fullText string
		if err := rows.Scan(&it.RecordingID, &it.EmployeeID, &it.EmployeeName, &it.RecordedAt, &fullText); err != nil {
			return out, err
		}
		level, label, hit := classifyFrontdeskRiskText(fullText)
		if level == "" {
			continue
		}
		it.RiskLevel = level
		it.RiskLabel = label
		it.HitKeyword = hit
		it.Snippet = extractRiskSnippet(fullText, hit, 72)
		it.Fingerprint = riskFingerprint(fullText, hit, level)
		it.DuplicateCount = 1
		it.RiskID = fmt.Sprintf("frontdesk:%d:%s", it.RecordingID, hit)
		out = append(out, it)
	}
	return out, rows.Err()
}

func dedupeFrontdeskRiskRows(rows []frontdeskRiskRow) []frontdeskRiskRow {
	if len(rows) <= 1 {
		return rows
	}
	type agg struct {
		item frontdeskRiskRow
	}
	order := make([]string, 0, len(rows))
	byKey := make(map[string]*agg, len(rows))
	for _, r := range rows {
		key := strings.TrimSpace(r.RiskLevel) + "|" + strings.TrimSpace(r.HitKeyword) + "|" + strings.TrimSpace(r.Fingerprint)
		if key == "||" {
			key = fmt.Sprintf("fallback:%d:%s", r.RecordingID, r.HitKeyword)
		}
		if hit, ok := byKey[key]; ok {
			hit.item.DuplicateCount++
			continue
		}
		cp := r
		byKey[key] = &agg{item: cp}
		order = append(order, key)
	}
	out := make([]frontdeskRiskRow, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k].item)
	}
	return out
}

func riskFingerprint(fullText, hit, level string) string {
	src := strings.ToLower(strings.TrimSpace(fullText))
	src = strings.ReplaceAll(src, " ", "")
	src = strings.ReplaceAll(src, "\n", "")
	src = strings.ReplaceAll(src, "\t", "")
	if len([]rune(src)) > 180 {
		src = string([]rune(src)[:180])
	}
	raw := level + "|" + hit + "|" + src
	h := fnv.New64a()
	_, _ = h.Write([]byte(raw))
	return fmt.Sprintf("%x", h.Sum64())
}

type roleDimensionScorePoint struct {
	RoleCode     string
	EmployeeID   int64
	EmployeeName string
	Dimension    string
	Score        float64
	TS           time.Time
}

type weeklySigmaAlert struct {
	RoleCode            string
	EmployeeID          int64
	EmployeeName        string
	DimensionCode       string
	DimensionLabel      string
	CurrentScore        float64
	CurrentMean         float64
	CurrentThreshold    float64
	PreviousScore       float64
	PreviousMean        float64
	PreviousThreshold   float64
	CurrentSampleCount  int64
	PreviousSampleCount int64
}

type weeklyPositiveAlert struct {
	RoleCode       string
	EmployeeID     int64
	EmployeeName   string
	DimensionCode  string
	DimensionLabel string
	CurrentScore   float64
	PreviousScore  float64
	Delta          float64
}

type weekStat struct {
	sum   float64
	count int64
}

type overdueTaskAttentionRow struct {
	TaskID       int64
	CustomerName string
	AssigneeName string
	OverdueHours int64
}

func (s *Service) detectAbilityAttentionBySigma(ctx context.Context, tenantID int64) ([]weeklySigmaAlert, error) {
	now := time.Now()
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -int(now.Weekday())+1)
	if now.Weekday() == time.Sunday {
		monday = monday.AddDate(0, 0, -6)
	}
	prevStart := monday.AddDate(0, 0, -7)
	prev2Start := monday.AddDate(0, 0, -14)
	weekEnd := monday.AddDate(0, 0, 7)

	points, err := s.loadRoleDimensionScorePoints(ctx, tenantID, prev2Start, weekEnd)
	if err != nil {
		return nil, err
	}
	type weekKey struct {
		role      string
		dimension string
		weekStart string
	}
	empWeek := make(map[weekKey]map[int64]*weekStat)
	empName := make(map[int64]string)
	for _, p := range points {
		ws := mondayOf(p.TS).Format("2006-01-02")
		k := weekKey{role: p.RoleCode, dimension: p.Dimension, weekStart: ws}
		if _, ok := empWeek[k]; !ok {
			empWeek[k] = make(map[int64]*weekStat)
		}
		if _, ok := empWeek[k][p.EmployeeID]; !ok {
			empWeek[k][p.EmployeeID] = &weekStat{}
		}
		empWeek[k][p.EmployeeID].sum += p.Score
		empWeek[k][p.EmployeeID].count++
		if p.EmployeeName != "" {
			empName[p.EmployeeID] = p.EmployeeName
		}
	}

	curWeek := monday.Format("2006-01-02")
	prevWeek := prevStart.Format("2006-01-02")
	alerts := make([]weeklySigmaAlert, 0)
	dimOrder := []struct{ role, dim string }{
		{"consultant", "开场建立权威"}, {"consultant", "需求探索"}, {"consultant", "问题放大"}, {"consultant", "专业呈现"}, {"consultant", "方案定制"}, {"consultant", "异议化解"}, {"consultant", "成交促成"},
		{"doctor", "G1"}, {"doctor", "G2"}, {"doctor", "G3"}, {"doctor", "G4"}, {"doctor", "G5"}, {"doctor", "G6"},
		{"therapist", "D1"}, {"therapist", "D2"}, {"therapist", "D3"}, {"therapist", "D4"}, {"therapist", "D5"},
	}
	for _, item := range dimOrder {
		curK := weekKey{role: item.role, dimension: item.dim, weekStart: curWeek}
		prevK := weekKey{role: item.role, dimension: item.dim, weekStart: prevWeek}
		curMap := empWeek[curK]
		prevMap := empWeek[prevK]
		if len(curMap) < 2 || len(prevMap) < 2 {
			continue
		}
		curAvgByEmp, curMean, curStd := buildWeekStats(curMap)
		prevAvgByEmp, prevMean, prevStd := buildWeekStats(prevMap)
		curThreshold := curMean - curStd
		prevThreshold := prevMean - prevStd
		for empID, curVal := range curAvgByEmp {
			prevVal, ok := prevAvgByEmp[empID]
			if !ok {
				continue
			}
			if curVal < curThreshold && prevVal < prevThreshold {
				alerts = append(alerts, weeklySigmaAlert{
					RoleCode:            item.role,
					EmployeeID:          empID,
					EmployeeName:        empName[empID],
					DimensionCode:       item.dim,
					DimensionLabel:      displayDimensionLabel(item.role, item.dim),
					CurrentScore:        roundRisk(curVal),
					CurrentMean:         roundRisk(curMean),
					CurrentThreshold:    roundRisk(curThreshold),
					PreviousScore:       roundRisk(prevVal),
					PreviousMean:        roundRisk(prevMean),
					PreviousThreshold:   roundRisk(prevThreshold),
					CurrentSampleCount:  int64(len(curMap)),
					PreviousSampleCount: int64(len(prevMap)),
				})
			}
		}
	}
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].RoleCode != alerts[j].RoleCode {
			return alerts[i].RoleCode < alerts[j].RoleCode
		}
		if alerts[i].DimensionCode != alerts[j].DimensionCode {
			return alerts[i].DimensionCode < alerts[j].DimensionCode
		}
		return alerts[i].EmployeeID < alerts[j].EmployeeID
	})
	return alerts, nil
}

func (s *Service) detectPositiveChangesByWeek(ctx context.Context, tenantID int64, limit int) ([]weeklyPositiveAlert, error) {
	if limit <= 0 {
		limit = 5
	}
	now := time.Now()
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -int(now.Weekday())+1)
	if now.Weekday() == time.Sunday {
		monday = monday.AddDate(0, 0, -6)
	}
	prevStart := monday.AddDate(0, 0, -7)
	weekEnd := monday.AddDate(0, 0, 7)

	points, err := s.loadRoleDimensionScorePoints(ctx, tenantID, prevStart, weekEnd)
	if err != nil {
		return nil, err
	}
	type weekKey struct {
		role      string
		dimension string
		weekStart string
	}
	empWeek := make(map[weekKey]map[int64]*weekStat)
	empName := make(map[int64]string)
	for _, p := range points {
		ws := mondayOf(p.TS).Format("2006-01-02")
		k := weekKey{role: p.RoleCode, dimension: p.Dimension, weekStart: ws}
		if _, ok := empWeek[k]; !ok {
			empWeek[k] = make(map[int64]*weekStat)
		}
		if _, ok := empWeek[k][p.EmployeeID]; !ok {
			empWeek[k][p.EmployeeID] = &weekStat{}
		}
		empWeek[k][p.EmployeeID].sum += p.Score
		empWeek[k][p.EmployeeID].count++
		if p.EmployeeName != "" {
			empName[p.EmployeeID] = p.EmployeeName
		}
	}

	curWeek := monday.Format("2006-01-02")
	prevWeek := prevStart.Format("2006-01-02")
	dimOrder := []struct{ role, dim string }{
		{"consultant", "开场建立权威"}, {"consultant", "需求探索"}, {"consultant", "问题放大"}, {"consultant", "专业呈现"}, {"consultant", "方案定制"}, {"consultant", "异议化解"}, {"consultant", "成交促成"},
		{"doctor", "G1"}, {"doctor", "G2"}, {"doctor", "G3"}, {"doctor", "G4"}, {"doctor", "G5"}, {"doctor", "G6"},
		{"therapist", "D1"}, {"therapist", "D2"}, {"therapist", "D3"}, {"therapist", "D4"}, {"therapist", "D5"},
	}
	alerts := make([]weeklyPositiveAlert, 0, 64)
	for _, item := range dimOrder {
		curK := weekKey{role: item.role, dimension: item.dim, weekStart: curWeek}
		prevK := weekKey{role: item.role, dimension: item.dim, weekStart: prevWeek}
		curMap := empWeek[curK]
		prevMap := empWeek[prevK]
		if len(curMap) == 0 || len(prevMap) == 0 {
			continue
		}
		for empID, curSt := range curMap {
			prevSt, ok := prevMap[empID]
			if !ok || curSt == nil || prevSt == nil || curSt.count <= 0 || prevSt.count <= 0 {
				continue
			}
			curAvg := curSt.sum / float64(curSt.count)
			prevAvg := prevSt.sum / float64(prevSt.count)
			delta := curAvg - prevAvg
			if delta <= 0.3 {
				continue
			}
			alerts = append(alerts, weeklyPositiveAlert{
				RoleCode:       item.role,
				EmployeeID:     empID,
				EmployeeName:   empName[empID],
				DimensionCode:  item.dim,
				DimensionLabel: positiveDimensionLabel(item.role, item.dim),
				CurrentScore:   roundRisk(curAvg),
				PreviousScore:  roundRisk(prevAvg),
				Delta:          roundRisk(delta),
			})
		}
	}
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Delta != alerts[j].Delta {
			return alerts[i].Delta > alerts[j].Delta
		}
		if alerts[i].RoleCode != alerts[j].RoleCode {
			return alerts[i].RoleCode < alerts[j].RoleCode
		}
		if alerts[i].DimensionCode != alerts[j].DimensionCode {
			return alerts[i].DimensionCode < alerts[j].DimensionCode
		}
		return alerts[i].EmployeeID < alerts[j].EmployeeID
	})
	if len(alerts) > limit {
		alerts = alerts[:limit]
	}
	return alerts, nil
}

func (s *Service) loadRoleDimensionScorePoints(ctx context.Context, tenantID int64, startAt, endAt time.Time) ([]roleDimensionScorePoint, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未命名员工') AS employee_name,
		       r.business_scope,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		  AND r.business_scope IN ('consultant', 'doctor', 'therapist')
	`, tenantID, startAt, endAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make([]roleDimensionScorePoint, 0, 4096)
	for rows.Next() {
		var employeeID int64
		var employeeName, roleCode string
		var analysis map[string]interface{}
		var ts time.Time
		if scanErr := rows.Scan(&employeeID, &employeeName, &roleCode, &analysis, &ts); scanErr != nil {
			return nil, scanErr
		}
		dims := extractRoleDimensionScores(roleCode, analysis)
		for dim, score := range dims {
			if score <= 0 {
				continue
			}
			points = append(points, roleDimensionScorePoint{
				RoleCode:     roleCode,
				EmployeeID:   employeeID,
				EmployeeName: employeeName,
				Dimension:    dim,
				Score:        score,
				TS:           ts,
			})
		}
	}
	return points, rows.Err()
}

func extractRoleDimensionScores(roleCode string, analysis map[string]interface{}) map[string]float64 {
	role := strings.TrimSpace(roleCode)
	switch role {
	case "doctor":
		return extractDoctorDimensionScores(analysis)
	case "therapist":
		out := map[string]float64{"D1": 0, "D2": 0, "D3": 0, "D4": 0, "D5": 0}
		for dim, raw := range toScoreMap(pickMap(analysis, "dimension_scores")) {
			code := normalizeTherapistDimensionCode(dim)
			if _, ok := out[code]; !ok {
				continue
			}
			out[code] = normalizeScoreToHundredLocal(raw)
		}
		return out
	default:
		out := map[string]float64{
			"开场建立权威": 0, "需求探索": 0, "问题放大": 0, "专业呈现": 0, "方案定制": 0, "异议化解": 0, "成交促成": 0,
		}
		quality := pickMap(analysis, "quality_score")
		stages := pickArray(quality, "stages")
		for _, item := range stages {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name := strings.TrimSpace(firstNonEmptyText(m["name"], m["group"]))
			if name == "促成与收尾" {
				name = "成交促成"
			}
			if _, ok := out[name]; !ok {
				continue
			}
			if score, ok := toFloat(m["score"]); ok && score > 0 {
				out[name] = score
			}
		}
		return out
	}
}

func mondayOf(t time.Time) time.Time {
	base := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	offset := int(base.Weekday()) - 1
	if base.Weekday() == time.Sunday {
		offset = 6
	}
	return base.AddDate(0, 0, -offset)
}

func buildWeekStats(input map[int64]*weekStat) (map[int64]float64, float64, float64) {
	avgByEmp := make(map[int64]float64, len(input))
	values := make([]float64, 0, len(input))
	for empID, st := range input {
		if st == nil || st.count <= 0 {
			continue
		}
		avg := st.sum / float64(st.count)
		avgByEmp[empID] = avg
		values = append(values, avg)
	}
	if len(values) == 0 {
		return avgByEmp, 0, 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var varSum float64
	for _, v := range values {
		diff := v - mean
		varSum += diff * diff
	}
	std := 0.0
	if len(values) > 1 {
		std = math.Sqrt(varSum / float64(len(values)))
	}
	return avgByEmp, mean, std
}

func displayDimensionLabel(roleCode, dim string) string {
	if roleCode == "doctor" {
		switch dim {
		case "G1":
			return "建立接诊阶段"
		case "G2":
			return "引出信息阶段"
		case "G3":
			return "给予信息阶段"
		case "G4":
			return "理解患者视角"
		case "G5":
			return "结束接诊"
		case "G6":
			return "治疗/预防计划"
		}
	}
	if roleCode == "therapist" {
		switch dim {
		case "D1":
			return "治疗铺垫"
		case "D2":
			return "互动评估"
		case "D3":
			return "专业操作"
		case "D4":
			return "顾虑处理"
		case "D5":
			return "方案闭环"
		}
	}
	return dim
}

func (s *Service) listOverdueTaskAttentionRows(ctx context.Context, tenantID int64, limit int) ([]overdueTaskAttentionRow, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT
			t.id,
			COALESCE(NULLIF(t.customer_name, ''), '未命名患者') AS customer_name,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), NULLIF(e.username, ''), '未分配') AS assignee_name,
			GREATEST(1, FLOOR(EXTRACT(EPOCH FROM (NOW() - t.due_at))/3600))::bigint AS overdue_hours
		FROM recording_tasks t
		LEFT JOIN employees e ON e.id = t.assigned_to
		WHERE t.tenant_id = $1
		  AND t.status IN ('pending', 'assigned')
		  AND t.due_at < NOW()
		ORDER BY t.due_at ASC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]overdueTaskAttentionRow, 0, limit)
	for rows.Next() {
		var item overdueTaskAttentionRow
		if err := rows.Scan(&item.TaskID, &item.CustomerName, &item.AssigneeName, &item.OverdueHours); err != nil {
			return out, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func classifyFrontdeskRiskText(text string) (level string, label string, hit string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", "", ""
	}
	strongKeywords := []string{"私人微信收款", "代开药", "绕开处方", "微信转账给我", "药品直接邮寄"}
	weakKeywords := []string{"转账", "邮寄", "微信", "药品"}
	for _, k := range strongKeywords {
		if strings.Contains(t, k) {
			return "high", "私域合规违规", k
		}
	}
	for _, k := range weakKeywords {
		if strings.Contains(t, k) {
			return "medium", "可疑合规表达", k
		}
	}
	return "", "", ""
}

func extractRiskSnippet(text, keyword string, maxLen int) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return ""
	}
	if keyword == "" {
		return truncateText(t, maxLen)
	}
	idx := strings.Index(t, keyword)
	if idx < 0 {
		return truncateText(t, maxLen)
	}
	runes := []rune(t)
	pos := len([]rune(t[:idx]))
	start := pos - (maxLen / 2)
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(runes) {
		end = len(runes)
	}
	return strings.TrimSpace(string(runes[start:end]))
}

func (s *Service) listHandledRiskIDs(ctx context.Context, tenantID int64) (map[string]struct{}, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT risk_id
		FROM management_risk_handled
		WHERE tenant_id = $1
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var riskID string
		if err := rows.Scan(&riskID); err != nil {
			return out, err
		}
		out[riskID] = struct{}{}
	}
	return out, rows.Err()
}

func (s *Service) MarkManagementRiskHandled(ctx context.Context, tenantID, handledBy int64, riskID string) error {
	_, err := s.store.pool.Exec(ctx, `
		INSERT INTO management_risk_handled (tenant_id, risk_id, handled_by, handled_at, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW(), NOW())
		ON CONFLICT (tenant_id, risk_id)
		DO UPDATE SET handled_by = EXCLUDED.handled_by, handled_at = EXCLUDED.handled_at, updated_at = NOW()
	`, tenantID, strings.TrimSpace(riskID), handledBy)
	return err
}

func (s *Service) getOpsHealthCounts(ctx context.Context, tenantID int64, startAt, endAt time.Time) (int64, int64, int64, error) {
	row := s.store.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total_count,
			COUNT(*) FILTER (WHERE r.customer_id IS NOT NULL) AS linked_count,
			COUNT(*) FILTER (
				WHERE COALESCE(NULLIF(r.analysis_display->>'visit_outcome', ''), NULLIF(r.analysis_result->>'visit_outcome', ''), NULLIF(r.analysis_display->>'decision_status', ''), '') <> ''
			) AS deal_tagged_count
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		  AND r.business_scope IN ('consultant', 'doctor', 'therapist')
	`, tenantID, startAt, endAt)
	var totalCount, linkedCount, dealTaggedCount int64
	if err := row.Scan(&totalCount, &linkedCount, &dealTaggedCount); err != nil {
		return 0, 0, 0, err
	}
	return totalCount, linkedCount, dealTaggedCount, nil
}

func parseManagementPeriod(period string) (time.Time, time.Time, string) {
	now := time.Now()
	switch strings.TrimSpace(period) {
	case "2w":
		return now.AddDate(0, 0, -14), now, "近2周"
	case "4w":
		return now.AddDate(0, 0, -28), now, "近4周"
	case "1w", "":
		fallthrough
	default:
		return now.AddDate(0, 0, -7), now, "本周"
	}
}

func confidenceBySamples(n int64) string {
	if n >= 10 {
		return "high"
	}
	if n >= 5 {
		return "medium"
	}
	return "low"
}

func roundRisk(v float64) float64 {
	return math.Round(v*10) / 10
}

func scoreTextForRisk(v float64, roleCode string) string {
	if roleCode == string(RecordingScopeConsultant) || roleCode == "consultant" {
		return fmt.Sprintf("%.1f/5分", math.Round((v/20.0)*10.0)/10.0)
	}
	return fmt.Sprintf("%.1f%%", roundRisk(v))
}

func positiveScoreText(roleCode string, score float64) string {
	if strings.TrimSpace(roleCode) == string(RecordingScopeConsultant) || strings.TrimSpace(roleCode) == "consultant" {
		return fmt.Sprintf("%.1f", roundRisk(score))
	}
	return fmt.Sprintf("%.1f%%", roundRisk(score))
}

func positiveDeltaText(roleCode string, delta float64) string {
	if strings.TrimSpace(roleCode) == string(RecordingScopeConsultant) || strings.TrimSpace(roleCode) == "consultant" {
		return fmt.Sprintf("+%.1f", roundRisk(delta))
	}
	return fmt.Sprintf("+%.1f%%", roundRisk(delta))
}

func roleLabelForRisk(roleCode string) string {
	switch strings.TrimSpace(roleCode) {
	case string(RecordingScopeConsultant):
		return "咨询师"
	case string(RecordingScopeDoctor):
		return "医生"
	case string(RecordingScopeTherapist):
		return "康复师"
	case string(RecordingScopeFrontdesk):
		return "前台"
	default:
		return roleCode
	}
}

func positiveDimensionLabel(roleCode, dim string) string {
	d := strings.TrimSpace(dim)
	label := displayDimensionLabel(roleCode, d)
	if strings.TrimSpace(roleCode) == string(RecordingScopeDoctor) || strings.TrimSpace(roleCode) == "doctor" {
		short := map[string]string{
			"G1": "开场",
			"G2": "信息引出",
			"G3": "信息给予",
			"G4": "同理沟通",
			"G5": "结束接诊",
			"G6": "治疗计划",
		}
		if s, ok := short[d]; ok {
			return fmt.Sprintf("%s%s", d, s)
		}
		return d
	}
	if strings.TrimSpace(roleCode) == string(RecordingScopeTherapist) || strings.TrimSpace(roleCode) == "therapist" {
		return fmt.Sprintf("%s%s", d, label)
	}
	return label
}

func isDiscoveryScoreQualified(roleCode string, score float64) bool {
	role := strings.TrimSpace(roleCode)
	if role == string(RecordingScopeConsultant) || role == "consultant" {
		normalized := math.Round((score/20.0)*10.0) / 10.0
		return normalized >= 4.0
	}
	return roundRisk(score) >= 80.0
}

func discoveryScoreText(roleCode string, score float64) string {
	role := strings.TrimSpace(roleCode)
	if role == string(RecordingScopeConsultant) || role == "consultant" {
		normalized := math.Round((score/20.0)*10.0) / 10.0
		if normalized >= 5.0 {
			return "5分满分"
		}
		return fmt.Sprintf("%.1f分", normalized)
	}
	return fmt.Sprintf("%.1f%%", roundRisk(score))
}

func discoveryDoctorShortLabel(code string) string {
	short := map[string]string{
		"G1": "开场",
		"G2": "信息引出",
		"G3": "信息给予",
		"G4": "同理沟通",
		"G5": "结束接诊",
		"G6": "治疗计划",
	}
	if s, ok := short[strings.TrimSpace(code)]; ok {
		return s
	}
	return strings.TrimSpace(code)
}

func buildDiscoveryReason(roleCode, dimensionLabel string, score float64) string {
	role := strings.TrimSpace(roleCode)
	if role == string(RecordingScopeConsultant) || role == "consultant" {
		return fmt.Sprintf("%s达到高分阈值（%s）。", strings.TrimSpace(dimensionLabel), discoveryScoreText(roleCode, score))
	}
	return fmt.Sprintf("%s达到高分阈值（%s）。", strings.TrimSpace(dimensionLabel), discoveryScoreText(roleCode, score))
}

func buildDiscoveryReasonFromEvidence(roleCode, dimensionLabel string, score float64, evidenceList []string, clipText string) string {
	summary := buildDiscoveryInsightSummary(roleCode, dimensionLabel, evidenceList, clipText)
	if strings.TrimSpace(summary) != "" {
		return summary
	}
	scorePart := fmt.Sprintf("%s达到高分阈值（%s）", strings.TrimSpace(dimensionLabel), discoveryScoreText(roleCode, score))
	clip := strings.TrimSpace(truncateText(strings.TrimSpace(clipText), 30))
	if clip == "" {
		return scorePart
	}
	return fmt.Sprintf("%s；原文体现为“%s”。", scorePart, clip)
}

func buildDiscoveryInsightSummary(roleCode, dimensionLabel string, evidenceList []string, clipText string) string {
	role := strings.TrimSpace(roleCode)
	dim := strings.TrimSpace(dimensionLabel)
	src := strings.ToLower(strings.Join(append(append([]string{}, evidenceList...), clipText), " "))
	if role == string(RecordingScopeConsultant) || role == "consultant" {
		switch {
		case strings.Contains(dim, "异议化解"):
			if strings.Contains(src, "案例") || strings.Contains(src, "对比") || strings.Contains(src, "别人") {
				return "用同类案例对比化解顾虑，降低患者对方案的心理阻力。"
			}
			return "针对患者顾虑做有针对性的回应，推动对话从犹豫走向决策。"
		case strings.Contains(dim, "需求探索"):
			return "通过连续追问澄清真实诉求，避免基于模糊信息给出方案。"
		case strings.Contains(dim, "问题放大"):
			return "把风险后果具体化，帮助患者形成及时行动的紧迫感。"
		case strings.Contains(dim, "专业呈现"):
			return "用可验证的专业信息建立信任，提升方案接受度。"
		}
	}
	if role == string(RecordingScopeDoctor) || role == "doctor" {
		switch {
		case strings.Contains(dim, "G1"):
			return "开场先设定议程与时间预期，帮助患者快速进入沟通节奏。"
		case strings.Contains(dim, "G4"):
			return "先回应患者视角再给建议，减少沟通对抗并提升配合度。"
		default:
			return "该段对话完整覆盖关键接诊动作，适合作为团队示范片段。"
		}
	}
	if role == string(RecordingScopeTherapist) || role == "therapist" {
		switch {
		case strings.Contains(dim, "互动评估"):
			return "通过持续确认即时体感反馈，动态评估治疗反应并及时校正操作。"
		case strings.Contains(dim, "治疗铺垫"):
			return "治疗前先确认症状与影响场景，为后续干预建立清晰起点。"
		case strings.Contains(dim, "专业操作"):
			return "操作过程中同步解释与确认关键点，提升患者理解和配合度。"
		default:
			return "该片段体现了标准化治疗沟通动作，便于团队复盘复用。"
		}
	}
	return ""
}

func truncateText(s string, n int) string {
	t := strings.TrimSpace(s)
	if n <= 0 || len([]rune(t)) <= n {
		return t
	}
	rs := []rune(t)
	return string(rs[:n]) + "..."
}

func (s *Service) ListBenchmarkClips(
	ctx context.Context,
	tenantID int64,
	status string,
	source string,
	roleCode string,
	dimension string,
	keyword string,
	page int,
	pageSize int,
) (*BenchmarkClipListResponse, error) {
	if strings.TrimSpace(status) == "pending" && strings.TrimSpace(source) == "" {
		source = "auto"
	}
	items, total, err := s.store.ListBenchmarkClips(ctx, tenantID, status, source, roleCode, dimension, keyword, page, pageSize)
	if err != nil {
		return nil, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return &BenchmarkClipListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *Service) GenerateBenchmarkCandidates(ctx context.Context, tenantID int64) (int, error) {
	rows, err := s.store.ListBenchmarkSourceRows(ctx, tenantID, 30)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, row := range rows {
		candidates := deriveBenchmarkCandidatesFromAnalysis(row)
		for _, c := range candidates {
			c.TenantID = tenantID
			c.RecordingID = row.RecordingID
			c.EmployeeID = row.EmployeeID
			inserted, insErr := s.store.InsertBenchmarkClipIfNotExists(ctx, c)
			if insErr != nil {
				continue
			}
			if inserted {
				created++
			}
		}
	}
	return created, nil
}

func (s *Service) AcceptBenchmarkClip(ctx context.Context, tenantID int64, id int64, req UpdateBenchmarkClipStatusRequest) (*BenchmarkClip, error) {
	comment := strings.TrimSpace(req.AIComment)
	points := req.LearningPoints
	if comment == "" || len(points) == 0 {
		item, err := s.store.GetBenchmarkClipByID(ctx, tenantID, id)
		if err == nil && item != nil {
			autoComment, autoPoints := s.generateBenchmarkCommentAndPoints(ctx, tenantID, item)
			if comment == "" {
				comment = autoComment
			}
			if len(points) == 0 {
				points = autoPoints
			}
		}
	}
	return s.store.UpdateBenchmarkClipStatus(ctx, tenantID, id, "accepted", comment, points)
}

func (s *Service) RejectBenchmarkClip(ctx context.Context, tenantID int64, id int64) (*BenchmarkClip, error) {
	return s.store.UpdateBenchmarkClipStatus(ctx, tenantID, id, "rejected", "", nil)
}

func (s *Service) CreateManualBenchmarkClip(ctx context.Context, tenantID int64, recordingID int64, req CreateManualBenchmarkClipRequest) (*BenchmarkClip, error) {
	rec, err := s.store.GetRecordingByID(ctx, recordingID)
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.TenantID != tenantID {
		return nil, fmt.Errorf("recording not found")
	}
	if strings.TrimSpace(req.RoleCode) == "" || strings.TrimSpace(req.Dimension) == "" || strings.TrimSpace(req.ClipText) == "" {
		return nil, fmt.Errorf("role_code, dimension, clip_text are required")
	}
	item, createErr := s.store.CreateManualBenchmarkClip(ctx, tenantID, recordingID, rec.EmployeeID, req)
	if createErr != nil {
		return nil, createErr
	}
	comment, points := s.generateBenchmarkCommentAndPoints(ctx, tenantID, item)
	updated, updErr := s.store.UpdateBenchmarkClipStatus(ctx, tenantID, item.ID, "accepted", comment, points)
	if updErr != nil {
		return item, nil
	}
	return updated, nil
}

func (s *Service) MarkBenchmarkUsedInMeeting(ctx context.Context, tenantID int64, req MarkBenchmarkMeetingUsedRequest) (*BenchmarkClip, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("recording_id", req.RecordingID); err != nil {
		return nil, err
	}
	return s.store.MarkBenchmarkUsedInMeeting(ctx, tenantID, req.RecordingID, req.RoleCode)
}

func (s *Service) PushBenchmarkClip(ctx context.Context, tenantID int64, clipID int64, pushedBy int64, req PushBenchmarkClipRequest) (int64, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return 0, err
	}
	if err := tenancy.RequirePositiveID("clip_id", clipID); err != nil {
		return 0, err
	}
	if len(req.TargetEmployeeIDs) == 0 {
		return 0, fmt.Errorf("target_employee_ids is required")
	}
	return s.store.CreateBenchmarkClipPushes(ctx, tenantID, clipID, pushedBy, req.TargetEmployeeIDs, req.Note)
}

func (s *Service) ListBenchmarkClipPushes(ctx context.Context, tenantID int64, clipID int64) ([]BenchmarkClipPushRecord, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return []BenchmarkClipPushRecord{}, nil
	}
	if err := tenancy.RequirePositiveID("clip_id", clipID); err != nil {
		return []BenchmarkClipPushRecord{}, nil
	}
	return s.store.ListBenchmarkClipPushes(ctx, tenantID, clipID)
}

func (s *Service) GetBenchmarkClipPushStatistics(ctx context.Context, tenantID int64, clipID int64) (BenchmarkClipPushStatistics, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return BenchmarkClipPushStatistics{}, nil
	}
	if err := tenancy.RequirePositiveID("clip_id", clipID); err != nil {
		return BenchmarkClipPushStatistics{}, nil
	}
	return s.store.GetBenchmarkClipPushStatistics(ctx, tenantID, clipID)
}

func (s *Service) AckBenchmarkClipPush(ctx context.Context, tenantID int64, pushID int64) (*BenchmarkClipPushRecord, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("push_id", pushID); err != nil {
		return nil, err
	}
	return s.store.AckBenchmarkClipPush(ctx, tenantID, pushID)
}

func (s *Service) ListMyLearningTasks(ctx context.Context, tenantID int64, employeeID int64, status string, page int, pageSize int) (*EmployeeLearningTaskListResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
		return nil, err
	}
	items, total, err := s.store.ListEmployeeLearningTasks(ctx, tenantID, employeeID, status, page, pageSize)
	if err != nil {
		return nil, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return &EmployeeLearningTaskListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *Service) AckMyLearningTask(ctx context.Context, tenantID int64, employeeID int64, pushID int64) (*BenchmarkClipPushRecord, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("push_id", pushID); err != nil {
		return nil, err
	}
	return s.store.AckEmployeeLearningTask(ctx, tenantID, employeeID, pushID)
}

func deriveBenchmarkCandidatesFromAnalysis(row benchmarkSourceRow) []benchmarkCandidateInput {
	if len(row.AnalysisRaw) == 0 {
		return nil
	}
	var analysis map[string]interface{}
	if err := json.Unmarshal(row.AnalysisRaw, &analysis); err != nil {
		return nil
	}
	var structured map[string]interface{}
	if len(row.StructuredRaw) > 0 {
		_ = json.Unmarshal(row.StructuredRaw, &structured)
	}
	role := strings.TrimSpace(row.RoleCode)
	out := make([]benchmarkCandidateInput, 0)

	switch role {
	case "consultant":
		qs := pickMap(analysis, "quality_score")
		stages := pickArray(qs, "stages")
		highlights := pickStringArray(analysis, "highlights")
		for _, st := range stages {
			m, ok := st.(map[string]interface{})
			if !ok {
				continue
			}
			scorePtr := pickFloat(m, "score")
			if scorePtr == nil || *scorePtr < 4 {
				continue
			}
			score := *scorePtr
			name := pickString(m, "name")
			if name == "" {
				name = pickString(m, "group")
			}
			text := pickString(m, "evidence")
			if text == "" {
				text = pickFirstPositiveText(highlights)
			}
			if text == "" {
				text = fmt.Sprintf("该片段在%s维度达到高分，表达方式具备复制价值。", name)
			}
			if looksNegativeText(text) {
				text = fmt.Sprintf("该片段在%s维度达到高分，表达方式具备复制价值。", name)
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, benchmarkCandidateInput{
				RoleCode:   role,
				Dimension:  name,
				Score:      score * 20,
				ClipText:   text,
				Confidence: "medium",
				Source:     "auto",
			})
		}
	case "doctor":
		segue := pickMap(analysis, "segue_detail")
		groupScores := pickMap(segue, "group_scores")
		if len(groupScores) == 0 {
			groupScores = pickMap(analysis, "segue_scores")
		}
		if len(groupScores) == 0 {
			groupScores = pickMap(pickMap(structured, "segue"), "group_scores")
		}
		items := pickArray(segue, "items")
		if len(items) == 0 {
			items = pickArray(pickMap(pickMap(analysis, "raw"), "doctor_segue_structured"), "items")
		}
		if len(items) == 0 {
			items = pickArray(pickMap(structured, "segue"), "items")
		}
		highlights := pickStringArray(analysis, "highlights")
		for k, v := range groupScores {
			scoreMap := toScoreMap(map[string]interface{}{"score": v})
			f, ok := scoreMap["score"]
			if !ok {
				continue
			}
			score := normalizeScoreToHundredLocal(f)
			if score < 80 {
				continue
			}
			text := pickPositiveEvidenceForDimension(items, k)
			if text == "" {
				text = pickFirstPositiveText(highlights)
			}
			if text == "" || looksNegativeText(text) {
				text = fmt.Sprintf("该片段在%s维度表现稳定且具备可复制性，适合用于团队讲评。", k)
			}
			out = append(out, benchmarkCandidateInput{
				RoleCode:   role,
				Dimension:  k,
				Score:      score,
				ClipText:   text,
				Confidence: "medium",
				Source:     "auto",
			})
		}
	case "therapist":
		dimensionScores := toScoreMap(pickMap(analysis, "dimension_scores"))
		resetItems := pickArray(pickMap(pickMap(pickMap(analysis, "raw"), "therapist_reset_analysis"), "reset"), "items")
		highlights := pickStringArray(analysis, "highlights")
		for dim, rawScore := range dimensionScores {
			score := normalizeScoreToHundredLocal(rawScore)
			if score < 80 {
				continue
			}
			text := pickPositiveResetEvidenceForDimension(resetItems, dim)
			if text == "" {
				text = pickFirstPositiveText(highlights)
			}
			if text == "" || looksNegativeText(text) {
				text = fmt.Sprintf("该片段在%s维度表现稳定且具备可复制性，适合用于团队讲评。", dim)
			}
			out = append(out, benchmarkCandidateInput{
				RoleCode:   role,
				Dimension:  dim,
				Score:      score,
				ClipText:   text,
				Confidence: "medium",
				Source:     "auto",
			})
		}
	}
	return out
}

func buildBenchmarkCommentAndPoints(item *BenchmarkClip) (string, []string) {
	dim := strings.TrimSpace(item.Dimension)
	if dim == "" {
		dim = "关键沟通"
	}
	role := normalizeMorningMeetingRole(item.RoleCode)
	switch role {
	case "consultant":
		switch dim {
		case "问题放大":
			return "把潜在风险和后果说具体，让客户从“知道问题”走向“感到需要现在处理”。", []string{
				"用生活场景或未来后果把风险说具体，不只停留在抽象判断。",
				"把放大点和客户自己的年龄、职业或当前困扰绑定起来。",
				"放大后顺势收回到行动建议，避免只制造压力不承接。",
			}
		case "需求探索":
			return "通过连续追问把客户真实诉求问透，为后续方案推荐建立清晰依据。", []string{
				"先问需求和顾虑，再进入方案介绍。",
				"围绕客户背景连续追问，不满足于一句笼统回答。",
				"把问到的信息及时收束成后续推荐依据。",
			}
		case "异议化解":
			return "先接住顾虑，再用具体对比或案例回应，让客户更容易继续往下决策。", []string{
				"先复述顾虑，避免一上来反驳。",
				"多用案例、对比和具体场景，而不是空泛保证。",
				"回应后重新拉回客户自己的决策条件。",
			}
		case "专业呈现":
			return "用客户听得懂的专业表达建立信任，而不是堆砌术语和参数。", []string{
				"优先讲客户能理解的专业依据。",
				"把专业点和客户当下问题直接关联起来。",
				"避免只有结论，没有解释过程。",
			}
		case "方案定制":
			return "方案不是平铺选项，而是基于客户情况做针对性匹配。", []string{
				"先明确客户条件，再给个性化推荐。",
				"说明为什么这个方案更适合她，而不是罗列全部方案。",
				"把推荐理由说到客户能复述出来的程度。",
			}
		case "成交促成", "促成与收尾":
			return "在客户已被说服的基础上，顺势把决策往前推一步，而不是停在模糊态。", []string{
				"识别客户已经松动的信号，及时收口。",
				"给出明确下一步，而不是把决定悬空。",
				"收尾时同步确认时间、流程或动作。",
			}
		default:
			return fmt.Sprintf("该片段在%s维度有较强可复制性，适合拆成团队可复用的话术动作。", dim), []string{
				"先识别这段对话里最关键的高分动作。",
				"把动作拆成团队可模仿的表达方式。",
				"复盘时重点讲“为什么有效”，不是只讲“说了什么”。",
			}
		}
	case "doctor":
		return "这段接诊对话覆盖了清晰的关键动作，适合拿来示范医生如何稳定推进沟通节奏。", []string{
			"按接诊阶段推进，不随意跳步。",
			"先回应患者视角，再进入专业建议。",
			"把关键解释说到患者能听懂、能配合。",
		}
	case "therapist":
		switch dim {
		case "互动评估":
			return "治疗过程中持续确认即时体感反馈，让评估和操作形成同步闭环。", []string{
				"边操作边确认患者体感，不等结束后再问。",
				"把反馈立即用于调整动作和判断反应。",
				"让患者知道每一步为什么这样做。",
			}
		case "治疗铺垫":
			return "治疗前先把症状、影响和预期讲清楚，为后续干预建立共同起点。", []string{
				"先确认主诉和影响场景，再进入操作。",
				"提前说明接下来会做什么、为什么做。",
				"让患者在开始前就建立清晰预期。",
			}
		default:
			return fmt.Sprintf("该片段在%s维度体现了标准化治疗沟通动作，适合团队复盘复用。", dim), []string{
				"把关键动作说清楚，让患者跟得上治疗过程。",
				"在治疗中持续确认理解和感受。",
				"把这类动作沉淀成团队标准流程。",
			}
		}
	default:
		return fmt.Sprintf("该片段在%s维度表现较好，适合用于团队复盘讲评。", dim), []string{
			"先明确对方具体情境，再给针对性回应。",
			"使用容易理解的表述，减少抽象术语。",
			"结尾给出明确下一步，形成沟通闭环。",
		}
	}
}

const (
	benchmarkReviewPromptCode = "benchmark_clip_review_v1"
	morningMeetingPromptCode  = "morning_meeting_review_v1"
)

func newRecordingBillingMetadata(recordingID int64, subject, scene string) *llmgateway.BillingMetadata {
	return &llmgateway.BillingMetadata{
		BusinessDomain:     "recording",
		BusinessObjectType: "recording",
		BusinessObjectID:   recordingID,
		BillingSubject:     subject,
		BillingScene:       scene,
		BillingRuleVersion: "v1",
		RecordingID:        recordingID,
	}
}

func (s *Service) generateBenchmarkCommentAndPoints(ctx context.Context, tenantID int64, item *BenchmarkClip) (string, []string) {
	// 兜底策略：任一步失败都回退模板，保证收录流程可用。
	fallbackComment, fallbackPoints := buildBenchmarkCommentAndPoints(item)
	if s.llmClient == nil || item == nil {
		return fallbackComment, fallbackPoints
	}
	llmCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	systemPrompt, userPrompt, err := s.loadBenchmarkReviewPrompt(llmCtx, tenantID)
	if err != nil || strings.TrimSpace(userPrompt) == "" {
		return fallbackComment, fallbackPoints
	}
	model, err := s.resolveBenchmarkLLMModelConfig(llmCtx, tenantID)
	if err != nil {
		return fallbackComment, fallbackPoints
	}
	pkg, err := s.buildBenchmarkLLMContextPackage(llmCtx, item)
	if err != nil {
		return fallbackComment, fallbackPoints
	}
	pkgJSON, _ := json.MarshalIndent(pkg, "", "  ")
	renderedUserPrompt := strings.ReplaceAll(userPrompt, "{{benchmark_context_json}}", string(pkgJSON))
	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "recording.benchmark",
		FunctionType:  model.FunctionType,
		Provider:      model.Provider,
		ModelCode:     model.ModelCode,
		Billing:       newRecordingBillingMetadata(item.RecordingID, "benchmark_review", "recording_benchmark"),
		Messages: []llmgateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: renderedUserPrompt},
		},
		Params: &llmgateway.Params{
			Temperature:    0.2,
			MaxTokens:      900,
			TimeoutSeconds: 2,
			ResponseFormat: "json",
		},
	}
	applyModelParamsToBenchmarkLLMRequest(&llmReq, model.ModelParams)
	resp, err := s.llmClient.TextInference(llmCtx, llmReq)
	if err != nil || strings.TrimSpace(resp.Content) == "" {
		return fallbackComment, fallbackPoints
	}
	aiComment, learningPoints, ok := parseBenchmarkReviewLLMOutput(resp.Content)
	if !ok {
		return fallbackComment, fallbackPoints
	}
	return aiComment, learningPoints
}

type benchmarkLLMModelSelection struct {
	FunctionType string
	Provider     string
	ModelCode    string
	ModelParams  JSONObject
}

func (s *Service) resolveBenchmarkLLMModelConfig(ctx context.Context, tenantID int64) (*benchmarkLLMModelSelection, error) {
	candidates := []string{"recording_benchmark_comment", "chat"}
	for _, functionType := range candidates {
		var out benchmarkLLMModelSelection
		err := s.store.pool.QueryRow(ctx, `
			SELECT
				COALESCE(function_type, ''),
				COALESCE(provider, ''),
				COALESCE(model_code, ''),
				COALESCE(model_params, extra_params, '{}'::json)
			FROM llm_model_configs
			WHERE deleted_at IS NULL
			  AND COALESCE(is_active, true) = true
			  AND function_type = $1
			  AND tenant_id IN ($2, 0)
			ORDER BY
			  CASE WHEN tenant_id = $2 THEN 0 ELSE 1 END,
			  CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
			  updated_at DESC,
			  id DESC
			LIMIT 1
		`, functionType, tenantID).Scan(&out.FunctionType, &out.Provider, &out.ModelCode, &out.ModelParams)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, err
		}
		if strings.TrimSpace(out.ModelCode) == "" {
			continue
		}
		if strings.TrimSpace(out.FunctionType) == "" {
			out.FunctionType = functionType
		}
		return &out, nil
	}
	return nil, fmt.Errorf("no llm model config for benchmark review")
}

func (s *Service) loadBenchmarkReviewPrompt(ctx context.Context, tenantID int64) (string, string, error) {
	var tenantPrompt string
	_ = s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(custom_user_prompt_template, '')
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1
		  AND prompt_code = $2
		  AND COALESCE(is_enabled, true) = true
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, tenantID, benchmarkReviewPromptCode).Scan(&tenantPrompt)
	basePrompt, err := s.store.GetRecordingPromptByCode(ctx, benchmarkReviewPromptCode)
	if err != nil {
		return "", "", err
	}
	systemPrompt := strings.TrimSpace(basePrompt.SystemPrompt)
	userPrompt := strings.TrimSpace(basePrompt.PromptText)
	if strings.TrimSpace(tenantPrompt) != "" {
		userPrompt = strings.TrimSpace(tenantPrompt)
	}
	if systemPrompt == "" {
		systemPrompt = "你是一名医疗场景培训教练，负责基于证据产出可复用点评。"
	}
	return systemPrompt, userPrompt, nil
}

func (s *Service) buildBenchmarkLLMContextPackage(ctx context.Context, item *BenchmarkClip) (map[string]interface{}, error) {
	rec, err := s.store.GetRecordingByID(ctx, item.RecordingID)
	if err != nil || rec == nil {
		return nil, fmt.Errorf("recording not found")
	}
	scene := ""
	if rec.Scene != nil {
		scene = string(*rec.Scene)
	}
	analysis := map[string]interface{}(rec.AnalysisResult)
	contextLines := extractTranscriptContextLines(analysis, item.ClipText, 4)
	dimensionEvidence := extractDimensionEvidence(analysis, item.RoleCode, item.Dimension)
	summary := extractRoleSummaries(rec)
	return map[string]interface{}{
		"role_code":          strings.TrimSpace(item.RoleCode),
		"dimension":          strings.TrimSpace(item.Dimension),
		"score":              item.Score,
		"scene_type":         scene,
		"recorded_at":        rec.CreatedAt.Format("2006-01-02 15:04:05"),
		"employee_name":      strings.TrimSpace(rec.EmployeeName),
		"clip_text":          strings.TrimSpace(item.ClipText),
		"context_lines":      contextLines,
		"dimension_evidence": dimensionEvidence,
		"role_summaries":     summary,
	}, nil
}

func extractRoleSummaries(rec *MedicalRecording) []string {
	out := make([]string, 0, 2)
	appendIf := func(v *string) {
		if v == nil {
			return
		}
		s := strings.TrimSpace(*v)
		if s != "" {
			if len([]rune(s)) > 120 {
				s = string([]rune(s)[:120])
			}
			out = append(out, s)
		}
	}
	appendIf(rec.DoctorSummary)
	appendIf(rec.TherapistSummary)
	appendIf(rec.ConsultantSummary)
	if len(out) > 2 {
		return out[:2]
	}
	return out
}

func extractTranscriptContextLines(analysis map[string]interface{}, clipText string, maxLines int) []string {
	lines := make([]string, 0, maxLines)
	if strings.TrimSpace(clipText) != "" {
		lines = append(lines, strings.TrimSpace(clipText))
	}
	segments := pickArray(analysis, "timeline_transcript")
	if len(segments) == 0 {
		segments = pickArray(analysis, "structured_transcript")
	}
	for _, seg := range segments {
		if len(lines) >= maxLines {
			break
		}
		m, ok := seg.(map[string]interface{})
		if !ok {
			continue
		}
		text := strings.TrimSpace(pickString(m, "text"))
		if text == "" {
			text = strings.TrimSpace(pickString(m, "content"))
		}
		if text == "" {
			continue
		}
		if containsLine(lines, text) {
			continue
		}
		lines = append(lines, text)
	}
	return lines
}

func containsLine(lines []string, target string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}

func extractDimensionEvidence(analysis map[string]interface{}, roleCode, dimension string) []string {
	out := make([]string, 0, 3)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || looksNegativeText(v) {
			return
		}
		for _, existing := range out {
			if existing == v {
				return
			}
		}
		out = append(out, v)
	}
	role := strings.TrimSpace(strings.ToLower(roleCode))
	dim := strings.TrimSpace(dimension)
	raw := pickMap(analysis, "raw")
	switch role {
	case "doctor":
		items := pickArray(pickMap(raw, "doctor_segue_structured"), "items")
		prefix := normalizeDoctorDimensionCode(dim) + "-"
		for _, it := range items {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			code := strings.TrimSpace(pickString(m, "code"))
			if !strings.HasPrefix(code, prefix) {
				continue
			}
			if strings.ToUpper(strings.TrimSpace(pickString(m, "result"))) != "Y" {
				continue
			}
			add(pickString(m, "evidence"))
		}
		if len(out) == 0 {
			for _, text := range pickStringSlice(pickArray(analysis, "highlights")) {
				add(text)
			}
			add(pickString(analysis, "critical_summary"))
			add(pickString(analysis, "report"))
		}
	case "therapist":
		items := pickArray(pickMap(pickMap(raw, "therapist_reset_analysis"), "reset"), "items")
		prefix := normalizeTherapistDimensionCode(dim) + "-"
		for _, it := range items {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			code := strings.TrimSpace(pickString(m, "code"))
			if !strings.HasPrefix(code, prefix) {
				continue
			}
			result := strings.ToUpper(strings.TrimSpace(pickString(m, "result")))
			if result != "Y" && result != "U" {
				continue
			}
			add(pickString(m, "evidence"))
		}
		if len(out) == 0 {
			for _, text := range pickStringSlice(pickArray(analysis, "highlights")) {
				add(text)
			}
			add(pickString(analysis, "critical_summary"))
			add(pickString(analysis, "report"))
		}
	default:
		stages := pickArray(analysis, "stages")
		if len(stages) == 0 {
			stages = pickArray(pickMap(analysis, "quality_score"), "stages")
		}
		for _, st := range stages {
			m, ok := st.(map[string]interface{})
			if !ok {
				continue
			}
			name := strings.TrimSpace(pickString(m, "name"))
			if name != dim {
				continue
			}
			add(pickString(m, "evidence"))
			add(pickString(m, "highlight"))
			add(pickString(m, "highlight_narrative"))
		}
		if len(out) == 0 {
			for _, text := range pickStringSlice(pickArray(analysis, "key_quotes")) {
				add(text)
			}
			add(pickString(analysis, "report"))
		}
	}
	return out
}

func normalizeDoctorDimensionCode(dim string) string {
	d := strings.ToUpper(strings.TrimSpace(dim))
	if strings.HasPrefix(d, "G") {
		return d
	}
	switch d {
	case "建立接诊阶段":
		return "G1"
	case "引出信息阶段":
		return "G2"
	case "给予信息阶段":
		return "G3"
	case "理解患者视角":
		return "G4"
	case "结束接诊":
		return "G5"
	case "治疗/预防计划":
		return "G6"
	default:
		return d
	}
}

func normalizeTherapistDimensionCode(dim string) string {
	d := strings.ToUpper(strings.TrimSpace(dim))
	if strings.HasPrefix(d, "D") {
		return d
	}
	switch strings.TrimSpace(dim) {
	case "治疗铺垫":
		return "D1"
	case "互动评估":
		return "D2"
	case "专业操作":
		return "D3"
	case "顾虑处理":
		return "D4"
	case "方案闭环":
		return "D5"
	default:
		return d
	}
}

func applyModelParamsToBenchmarkLLMRequest(req *llmgateway.TextInferenceRequest, modelParams JSONObject) {
	if req == nil || len(modelParams) == 0 {
		return
	}
	if req.Params == nil {
		req.Params = &llmgateway.Params{}
	}
	if value, ok := modelParams["temperature"]; ok {
		if v, ok := toFloat64(value); ok {
			req.Params.Temperature = v
		}
	}
	if value, ok := modelParams["max_tokens"]; ok {
		if v, ok := toInt(value); ok && v > 0 {
			req.Params.MaxTokens = v
		}
	}
	if value, ok := modelParams["timeout_seconds"]; ok {
		if v, ok := toInt(value); ok && v > 0 {
			req.Params.TimeoutSeconds = v
		}
	}
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i), true
		}
		f, ferr := n.Float64()
		if ferr != nil {
			return 0, false
		}
		return int(f), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func parseBenchmarkReviewLLMOutput(text string) (string, []string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil, false
	}
	parse := func(raw string) (string, []string, bool) {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			return "", nil, false
		}
		comment := strings.TrimSpace(pickString(obj, "ai_comment"))
		pointsRaw := pickArray(obj, "learning_points")
		points := make([]string, 0, len(pointsRaw))
		for _, p := range pointsRaw {
			if s, ok := p.(string); ok && strings.TrimSpace(s) != "" {
				points = append(points, strings.TrimSpace(s))
			}
		}
		if comment == "" || len(points) == 0 {
			return "", nil, false
		}
		if len(points) > 5 {
			points = points[:5]
		}
		return comment, points, true
	}
	if c, p, ok := parse(trimmed); ok {
		return c, p, true
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return parse(trimmed[start : end+1])
	}
	return "", nil, false
}

func pickPositiveEvidenceForDimension(items []interface{}, groupCode string) string {
	prefix := strings.TrimSpace(groupCode) + "-"
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		code := strings.TrimSpace(pickString(m, "code"))
		if !strings.HasPrefix(code, prefix) {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(pickString(m, "result"))) != "Y" {
			continue
		}
		ev := strings.TrimSpace(pickString(m, "evidence"))
		if ev != "" && !looksNegativeText(ev) {
			return ev
		}
	}
	return ""
}

func pickPositiveResetEvidenceForDimension(items []interface{}, groupCode string) string {
	prefix := strings.TrimSpace(groupCode) + "-"
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		code := strings.TrimSpace(pickString(m, "code"))
		if !strings.HasPrefix(code, prefix) {
			continue
		}
		result := strings.ToUpper(strings.TrimSpace(pickString(m, "result")))
		if result != "Y" && result != "U" {
			continue
		}
		ev := strings.TrimSpace(pickString(m, "evidence"))
		if ev != "" && !looksNegativeText(ev) {
			return ev
		}
	}
	return ""
}

func pickStringArray(source map[string]interface{}, key string) []string {
	arr := pickArray(source, key)
	if len(arr) == 0 {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func pickFirstPositiveText(values []string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" && !looksNegativeText(v) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func looksNegativeText(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	patterns := []string{"缺失", "不足", "未说明", "未解释", "未设", "未鼓励", "风险", "问题", "下降", "薄弱", "混乱", "依赖", "犹豫", "受限"}
	for _, p := range patterns {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

func normalizeScoreToHundredLocal(raw float64) float64 {
	if raw <= 0 {
		return 0
	}
	if raw <= 1 {
		return raw * 100
	}
	if raw <= 5 {
		return raw * 20
	}
	return raw
}

// GetRecording retrieves a medical recording by ID
func (s *Service) GetRecording(ctx context.Context, id int64) (*RecordingResponse, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return nil, err
	}

	resp := toRecordingResponse(recording)
	if err := s.enrichRecordingResponse(ctx, id, resp); err != nil {
		return nil, err
	}
	if err := s.rebuildRecordingAnalysisPayload(ctx, id, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *Service) GetTherapistReset(ctx context.Context, id int64) (*TherapistResetResponse, error) {
	resp, err := s.GetRecording(ctx, id)
	if err != nil {
		return nil, err
	}
	analysis := resp.AnalysisResult
	if analysis == nil {
		analysis = map[string]interface{}{}
	}
	raw := pickMap(analysis, "raw")
	tra := pickMap(raw, "therapist_reset_analysis")
	resetNode := pickMap(tra, "reset")
	items := toMapSlice(firstNonEmptyArray(
		pickArray(resetNode, "items"),
		pickArray(analysis, "reset_items"),
		pickArray(pickMap(analysis, "reset"), "items"),
	))
	criticalMissing := pickStringSlice(firstNonEmptyArray(pickArray(analysis, "critical_missing_items"), pickArray(tra, "critical_missing_items")))
	criticalDetail := make([]ResetCodeInfo, 0, len(criticalMissing))
	if details := firstNonEmptyArray(
		pickArray(analysis, "critical_missing_details"),
		pickArray(tra, "critical_missing_details"),
		pickArray(pickMap(analysis, "reset"), "critical_missing_details"),
	); len(details) > 0 {
		for _, raw := range details {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			code := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", item["code"])))
			base := s.describeResetCode(code)
			reason := strings.TrimSpace(fmt.Sprintf("%v", item["reason"]))
			action := strings.TrimSpace(fmt.Sprintf("%v", item["action"]))
			if reason == "" || reason == "<nil>" {
				reason = base.RiskIfMissing
			}
			if action == "" || action == "<nil>" {
				action = base.CoachAction
			}
			title := strings.TrimSpace(fmt.Sprintf("%v", item["title"]))
			if title == "" || title == "<nil>" {
				title = base.Title
			}
			criticalDetail = append(criticalDetail, ResetCodeInfo{
				Code:          code,
				Title:         title,
				WhyItMatters:  base.WhyItMatters,
				RiskIfMissing: base.RiskIfMissing,
				CoachAction:   base.CoachAction,
				Reason:        reason,
				Action:        action,
			})
		}
	}
	if len(criticalDetail) == 0 {
		for _, code := range criticalMissing {
			criticalDetail = append(criticalDetail, s.describeResetCode(code))
		}
	}
	for _, item := range items {
		code := strings.TrimSpace(fmt.Sprintf("%v", item["code"]))
		if code == "" {
			continue
		}
		desc := s.describeResetCode(code)
		if _, ok := item["title"]; !ok {
			item["title"] = desc.Title
		}
		if _, ok := item["why_it_matters"]; !ok {
			item["why_it_matters"] = desc.WhyItMatters
		}
		if _, ok := item["risk_if_missing"]; !ok {
			item["risk_if_missing"] = desc.RiskIfMissing
		}
		if _, ok := item["coach_action"]; !ok {
			item["coach_action"] = desc.CoachAction
		}
	}
	return &TherapistResetResponse{
		RecordingID:           id,
		DimensionScores:       toScoreMap(firstNonEmptyMap(pickMap(analysis, "dimension_scores"), pickMap(tra, "dimension_scores"))),
		ResetPercent:          valueOrZeroFloat(pickFloat(analysis, "reset_percent")),
		CriticalGap:           valueOrFalseBool(pickBool(analysis, "critical_gap")),
		CriticalMissingItems:  criticalMissing,
		CriticalMissingDetail: criticalDetail,
		Highlights:            pickStringSlice(firstNonEmptyArray(pickArray(analysis, "highlights"), pickArray(tra, "highlights"))),
		ImprovementPriorities: pickStringSlice(firstNonEmptyArray(pickArray(analysis, "improvement_priorities"), pickArray(tra, "improvement_priorities"))),
		RecommendedActions:    pickStringSlice(firstNonEmptyArray(pickArray(analysis, "recommended_actions"), pickArray(tra, "recommended_actions"))),
		Items:                 items,
	}, nil
}

func (s *Service) describeResetCode(code string) ResetCodeInfo {
	trimmed := strings.ToUpper(strings.TrimSpace(code))
	dict := s.getResetCodeDictionaryMap()
	if v, ok := dict[trimmed]; ok {
		return v
	}
	dimTitle := "通用质检"
	switch {
	case strings.HasPrefix(trimmed, "D1-"):
		dimTitle = "铺垫"
	case strings.HasPrefix(trimmed, "D2-"):
		dimTitle = "互动"
	case strings.HasPrefix(trimmed, "D3-"):
		dimTitle = "专业"
	case strings.HasPrefix(trimmed, "D4-"):
		dimTitle = "顾虑"
	case strings.HasPrefix(trimmed, "D5-"):
		dimTitle = "闭环"
	}
	return ResetCodeInfo{
		Code:          trimmed,
		Title:         fmt.Sprintf("%s检查项", dimTitle),
		WhyItMatters:  "该项影响治疗体验与转化结果。",
		RiskIfMissing: "缺失会降低患者信任与后续配合。",
		CoachAction:   "请在该环节补一句目标说明和下一步安排。",
	}
}

var defaultResetCodeDictionary = map[string]ResetCodeInfo{
	"D1-2":  {Code: "D1-2", Title: "开场目标说明", WhyItMatters: "让患者知道本次要解决什么，建立预期和配合。", RiskIfMissing: "患者容易感觉流程混乱，信任下降。", CoachAction: "开场30秒说清：本次目标、步骤、预期感受。"},
	"D2-10": {Code: "D2-10", Title: "治疗中风险沟通", WhyItMatters: "提前解释疼痛/不适可降低紧张和抵触。", RiskIfMissing: "患者出现疼痛时会被动中断，体验变差。", CoachAction: "操作前先提示：可能有酸痛，若超阈值立即反馈。"},
	"D5-21": {Code: "D5-21", Title: "疗程与复诊建议", WhyItMatters: "明确后续路径，提升持续治疗与结果达成。", RiskIfMissing: "患者结束即流失，效果难以巩固。", CoachAction: "结束前给出疗程建议：频次、周期、下次目标。"},
	"D5-24": {Code: "D5-24", Title: "家庭训练与注意事项", WhyItMatters: "院外执行决定效果保持与复发控制。", RiskIfMissing: "回家后无执行标准，改善难以延续。", CoachAction: "给3条家庭动作/禁忌，并确认患者复述。"},
}

func (s *Service) GetResetCodeDictionary() []ResetCodeInfo {
	dict := s.getResetCodeDictionaryMap()
	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ResetCodeInfo, 0, len(keys))
	for _, k := range keys {
		out = append(out, dict[k])
	}
	return out
}

func (s *Service) getResetCodeDictionaryMap() map[string]ResetCodeInfo {
	merged := make(map[string]ResetCodeInfo, len(defaultResetCodeDictionary))
	for k, v := range defaultResetCodeDictionary {
		merged[k] = v
	}
	path := strings.TrimSpace(s.resetCodeDictionaryPath)
	if path == "" {
		path = "configs/reset_code_dictionary.json"
	}
	bytes, err := os.ReadFile(path)
	if err != nil || len(bytes) == 0 {
		return merged
	}
	var fileItems []ResetCodeInfo
	if unmarshalErr := json.Unmarshal(bytes, &fileItems); unmarshalErr != nil {
		return merged
	}
	for _, item := range fileItems {
		code := strings.ToUpper(strings.TrimSpace(item.Code))
		if code == "" {
			continue
		}
		item.Code = code
		merged[code] = item
	}
	return merged
}

func valueOrZeroFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func valueOrFalseBool(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

// CreateRecording creates a new medical recording
func (s *Service) CreateRecording(ctx context.Context, req CreateRecordingRequest) (*RecordingResponse, error) {
	// Validate request
	if err := tenancy.RequirePositiveID("tenant_id", req.TenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("employee_id", req.EmployeeID); err != nil {
		return nil, err
	}
	if req.PatientName == "" {
		return nil, fmt.Errorf("patient_name is required")
	}
	if req.RecordingURL == "" {
		return nil, fmt.Errorf("recording_url is required")
	}
	if req.BusinessScope != nil {
		scope := strings.ToLower(strings.TrimSpace(*req.BusinessScope))
		if scope != "" && scope != "doctor" && scope != "consultant" && scope != "frontdesk" && scope != "therapist" {
			return nil, fmt.Errorf("business_scope is invalid")
		}
	}

	recording, err := s.store.CreateRecording(ctx, req)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
}

// UpdateRecording updates a medical recording
func (s *Service) UpdateRecording(ctx context.Context, id int64, req UpdateRecordingRequest) (*RecordingResponse, error) {
	before, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return nil, err
	}

	recording, err := s.store.UpdateRecording(ctx, id, req)
	if err != nil {
		return nil, err
	}

	if req.Status != nil && *req.Status == StatusCompleted && s.opportunityAlertDispatcher != nil {
		_ = s.opportunityAlertDispatcher.CreateFromRecording(ctx, id, "recording_completed")
	}

	if req.CustomerID != nil && recording.CustomerID != nil && customerChanged(before.CustomerID, recording.CustomerID) {
		_ = s.backfillEMRDraftCustomerID(ctx, id, *recording.CustomerID)
		_ = s.rebindOpenTasksCustomer(ctx, id, *recording.CustomerID)
		_ = s.dispatchMedicalFollowUpTasksIfPossible(ctx, id, "manual_link")
		_ = s.appendCustomerInteraction(ctx, recording.TenantID, *recording.CustomerID, recording.EmployeeID, "recording_linked", "system", id, "录音关联客户")
		if refreshed, refreshErr := s.store.GetRecordingByID(ctx, id); refreshErr == nil {
			recording = refreshed
		}
	}

	return toRecordingResponse(recording), nil
}

func customerChanged(before *int64, after *int64) bool {
	if before == nil && after == nil {
		return false
	}
	if before == nil || after == nil {
		return true
	}
	return *before != *after
}

func (s *Service) backfillEMRDraftCustomerID(ctx context.Context, recordingID, customerID int64) error {
	_, err := s.store.pool.Exec(ctx, `
		UPDATE recording_emr_drafts
		SET customer_id = $1
		WHERE recording_id = $2 AND customer_id IS NULL
	`, customerID, recordingID)
	return err
}

func (s *Service) rebindOpenTasksCustomer(ctx context.Context, recordingID, customerID int64) error {
	var customerName string
	var customerPhone string
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(name, ''), COALESCE(phone, '')
		FROM customers
		WHERE id = $1
	`, customerID).Scan(&customerName, &customerPhone); err != nil {
		return err
	}

	_, err := s.store.pool.Exec(ctx, `
		UPDATE recording_tasks
		SET customer_id = $1,
			customer_name = $2,
			customer_phone = $3,
			updated_at = NOW()
		WHERE recording_id = $4
		  AND status IN ('pending', 'assigned')
		  AND COALESCE(source_type, '') IN ('follow_up', 'ai')
	`, customerID, customerName, customerPhone, recordingID)
	return err
}

func (s *Service) dispatchMedicalFollowUpTasksIfPossible(ctx context.Context, recordingID int64, triggerSource string) error {
	rec, err := s.store.GetRecordingByID(ctx, recordingID)
	if err != nil {
		return err
	}

	if !isMedicalRecording(rec.AnalysisResult) {
		return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
			"eligible":       false,
			"reasons":        []string{"NON_MEDICAL_RECORDING"},
			"created_count":  0,
			"trigger_source": triggerSource,
			"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	followUpTasks := extractFollowUpTasks(rec.AnalysisResult)
	if len(followUpTasks) == 0 {
		return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
			"eligible":       false,
			"reasons":        []string{"NO_FOLLOWUP_TASKS"},
			"created_count":  0,
			"trigger_source": triggerSource,
			"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	if rec.CustomerID == nil || *rec.CustomerID <= 0 {
		return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
			"eligible":       false,
			"reasons":        []string{"MISSING_CUSTOMER"},
			"created_count":  0,
			"trigger_source": triggerSource,
			"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	var customerName string
	var customerPhone string
	customerQueryErr := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(name, ''), COALESCE(phone, '')
		FROM customers
		WHERE id = $1
	`, *rec.CustomerID).Scan(&customerName, &customerPhone)
	if customerQueryErr != nil {
		return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
			"eligible":       false,
			"reasons":        []string{"MISSING_CUSTOMER"},
			"created_count":  0,
			"trigger_source": triggerSource,
			"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
		})
	}

	var existingCount int
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recording_tasks
		WHERE tenant_id = $1 AND recording_id = $2
	`, rec.TenantID, rec.ID).Scan(&existingCount); err != nil {
		return err
	}

	createdCount := 0
	assigneeID := s.resolveRecordingTaskAssignee(ctx, rec.TenantID, rec.EmployeeID)
	for _, task := range followUpTasks {
		title := firstNonEmptyText(task["title"], task["name"], task["action"])
		if title == "" {
			continue
		}

		var duplicateCount int
		if err := s.store.pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM recording_tasks
			WHERE tenant_id = $1
			  AND recording_id = $2
			  AND source_type = 'follow_up'
			  AND title = $3
		`, rec.TenantID, rec.ID, title).Scan(&duplicateCount); err != nil {
			return err
		}
		if duplicateCount > 0 {
			continue
		}

		description := firstNonEmptyText(task["description"], task["reason"], task["note"])
		script := firstNonEmptyText(task["script"], task["call_script"])
		channel := firstNonEmptyText(task["channel"])
		priority := normalizeTaskPriority(firstNonEmptyText(task["priority"]))
		contactReason := description
		if contactReason == "" {
			contactReason = title
		}
		timingHours := toPositiveInt(task["timing_hours"])
		if timingHours <= 0 {
			timingHours = 24
		}
		dueAt := time.Now().Add(time.Duration(timingHours) * time.Hour)

		_, err := s.store.pool.Exec(ctx, `
			INSERT INTO recording_tasks (
				tenant_id,
				recording_id,
				customer_id,
				customer_name,
				customer_phone,
				title,
				description,
				priority,
				script,
				contact_reason,
				assigned_to,
				status,
				source_type,
				source_detail,
				due_at,
				created_at,
				updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, NULLIF($9, ''), NULLIF($10, ''), $11, 'pending', 'follow_up', NULLIF($12, ''), $13, NOW(), NOW())
		`, rec.TenantID, rec.ID, *rec.CustomerID, customerName, customerPhone, title, description, priority, script, contactReason, assigneeID, channel, dueAt)
		if err != nil {
			return err
		}
		createdCount++
	}

	var totalCount int
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recording_tasks
		WHERE tenant_id = $1 AND recording_id = $2
	`, rec.TenantID, rec.ID).Scan(&totalCount); err != nil {
		return err
	}

	alreadyDispatched := createdCount == 0 && existingCount > 0 && totalCount >= existingCount
	eligible := createdCount > 0 || alreadyDispatched
	reasons := []string{}
	if !eligible {
		reasons = []string{"NO_TASKS_CREATED"}
	}

	return s.persistFollowupDispatch(ctx, rec, map[string]interface{}{
		"eligible":       eligible,
		"reasons":        reasons,
		"created_count":  createdCount,
		"existing_count": totalCount,
		"trigger_source": triggerSource,
		"dispatched_at":  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Service) resolveRecordingTaskAssignee(ctx context.Context, tenantID, ownerEmployeeID int64) *int64 {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil
	}
	if err := tenancy.RequirePositiveID("employee_id", ownerEmployeeID); err != nil {
		return nil
	}

	var assignee int64
	err := s.store.pool.QueryRow(ctx, `
		SELECT ep.partner_employee_id
		FROM employee_partnerships ep
		INNER JOIN employees e
		  ON e.id = ep.partner_employee_id
		 AND e.tenant_id = ep.tenant_id
		 AND COALESCE(e.is_active, true) = true
		WHERE ep.tenant_id = $1
		  AND ep.primary_employee_id = $2
		  AND COALESCE(ep.is_active, true) = true
		ORDER BY COALESCE(ep.is_primary, false) DESC, ep.id DESC
		LIMIT 1
	`, tenantID, ownerEmployeeID).Scan(&assignee)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return nil
	}
	if assignee <= 0 {
		return nil
	}
	return &assignee
}

func isMedicalRecording(analysisResult map[string]interface{}) bool {
	if analysisResult == nil {
		return false
	}
	if b := pickBool(analysisResult, "is_medical_consultation"); b != nil && *b {
		return true
	}
	if b := pickBool(pickMap(analysisResult, "doctor_routing"), "is_medical_consultation"); b != nil && *b {
		return true
	}
	role := strings.ToLower(firstNonEmptyText(analysisResult["role"]))
	return role == "doctor" || role == "medical"
}

func extractFollowUpTasks(analysisResult map[string]interface{}) []map[string]interface{} {
	if analysisResult == nil {
		return nil
	}
	candidates := []interface{}{
		analysisResult["follow_up_tasks"],
		pickMap(analysisResult, "analysis_summary")["follow_up_tasks"],
		pickMap(pickMap(analysisResult, "doctor_segue_narrative"), "final")["follow_up_tasks"],
	}
	for _, candidate := range candidates {
		items := toMapSliceFromUnknown(candidate)
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

func toMapSliceFromUnknown(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]interface{}:
		return v
	default:
		return nil
	}
}

func normalizeTaskPriority(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high", "urgent", "高":
		return "high"
	case "low", "低":
		return "low"
	case "medium", "中":
		return "medium"
	default:
		return "medium"
	}
}

func toPositiveInt(raw interface{}) int {
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case float32:
		if v > 0 {
			return int(v)
		}
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0
		}
		if parsed, err := strconv.Atoi(trimmed); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func (s *Service) persistFollowupDispatch(ctx context.Context, rec *MedicalRecording, dispatch map[string]interface{}) error {
	analysis := map[string]interface{}{}
	if rec.AnalysisResult != nil {
		analysis = map[string]interface{}(rec.AnalysisResult)
	}
	analysis["followup_dispatch"] = dispatch
	payload, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	_, err = s.store.pool.Exec(ctx, `
		UPDATE recordings
		SET analysis_result = $1::jsonb, updated_at = NOW()
		WHERE id = $2
	`, payload, rec.ID)
	return err
}

// DeleteRecording deletes a medical recording
func (s *Service) DeleteRecording(ctx context.Context, id int64) error {
	return s.store.DeleteRecording(ctx, id)
}

// toRecordingResponse converts a MedicalRecording to RecordingResponse
func toRecordingResponse(r *MedicalRecording) *RecordingResponse {
	analysisStatus := strings.TrimSpace(string(r.Status))
	if r.AnalysisStatus != nil && strings.TrimSpace(*r.AnalysisStatus) != "" {
		analysisStatus = strings.TrimSpace(*r.AnalysisStatus)
	}
	resp := &RecordingResponse{
		ID:                r.ID,
		TenantID:          r.TenantID,
		TenantName:        r.TenantName,
		EmployeeID:        r.EmployeeID,
		EmployeeName:      r.EmployeeName,
		DepartmentName:    r.DepartmentName,
		DeviceNo:          r.DeviceNo,
		CustomerID:        r.CustomerID,
		CustomerName:      r.CustomerName,
		PatientName:       r.PatientName,
		PatientAge:        r.PatientAge,
		PatientGender:     r.PatientGender,
		PatientPhone:      r.PatientPhone,
		RecordingURL:      r.RecordingURL,
		RecordingDuration: r.RecordingDuration,
		Duration:          r.RecordingDuration,
		TranscriptText:    r.TranscriptText,
		DoctorSummary:     r.DoctorSummary,
		TherapistSummary:  r.TherapistSummary,
		ConsultantSummary: r.ConsultantSummary,
		BusinessScope:     strings.TrimSpace(r.BusinessScope),
		AnalysisStatus:    pickStringPtr(analysisStatus),
		ContentSeedsTypes: []string{},
		Status:            string(r.Status),
		ProcessingError:   r.ProcessingError,
		CreatedAt:         formatDBLocalTime(r.CreatedAt),
		UpdatedAt:         formatDBLocalTime(r.UpdatedAt),
	}
	if len(r.AnalysisResult) > 0 {
		resp.AnalysisResult = map[string]interface{}(r.AnalysisResult)
	}
	if len(r.AnalysisDisplay) > 0 {
		resp.AnalysisDisplay = map[string]interface{}(r.AnalysisDisplay)
	}

	if len(r.AnalysisDisplay) > 0 {
		analysisDisplay := map[string]interface{}(r.AnalysisDisplay)
		if nested := pickMap(analysisDisplay, "analysis_result"); nested != nil {
			if resp.AnalysisResult == nil {
				resp.AnalysisResult = nested
			} else {
				// Keep fields already persisted in recordings.analysis_result
				// (e.g. therapist RESET aggregate), and only fill missing keys from display payload.
				mergeMap(resp.AnalysisResult, nested)
			}
		}
		if resp.AnalysisResult == nil && looksLikeAnalysisResult(analysisDisplay) {
			resp.AnalysisResult = analysisDisplay
		}
		resp.AnalysisSummary = pickMap(resp.AnalysisResult, "analysis_summary")
		if resp.AnalysisSummary == nil {
			resp.AnalysisSummary = pickMap(analysisDisplay, "analysis_summary")
		}
		resp.RouteReview = pickMap(analysisDisplay, "route_review")
		resp.EMRDraft = pickMap(analysisDisplay, "emr_draft")
		resp.SceneType = pickStringPtr(
			pickString(resp.AnalysisResult, "scene_type"),
			pickString(resp.AnalysisResult, "scene"),
			pickString(analysisDisplay, "scene_type"),
		)
		resp.VisitOutcome = pickStringPtr(
			pickString(resp.AnalysisResult, "visit_outcome"),
			pickString(resp.AnalysisResult, "decision_status"),
			pickString(analysisDisplay, "visit_outcome"),
		)
		resp.SubjectiveSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "subjective_summary"),
			pickString(resp.AnalysisResult, "conversation_summary"),
			pickString(resp.AnalysisResult, "summary"),
			pickString(analysisDisplay, "subjective_summary"),
		)
		resp.ConversationSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "conversation_summary"),
			pickString(analysisDisplay, "conversation_summary"),
			pickString(resp.AnalysisResult, "subjective_summary"),
			pickString(analysisDisplay, "subjective_summary"),
			pickString(resp.AnalysisResult, "summary"),
		)
		resp.KeyQuotes = pickStringSlice(
			pickArray(resp.AnalysisResult, "key_quotes"),
			pickArray(resp.AnalysisResult, "highlights"),
			pickArray(analysisDisplay, "key_quotes"),
			pickArray(analysisDisplay, "highlights"),
			pickArray(resp.AnalysisSummary, "highlights"),
		)
		resp.QualityScore = pickFloatPtr(
			pickFloat(resp.AnalysisResult, "quality_score"),
			pickFloat(resp.AnalysisResult, "quality"),
			pickFloat(analysisDisplay, "quality_score"),
		)
		resp.SegueScore = pickFloatPtr(
			pickFloat(resp.AnalysisResult, "segue_percent"),
			pickFloat(resp.AnalysisResult, "segue_score"),
			pickFloat(resp.AnalysisResult, "communication_score"),
			pickFloat(pickMap(resp.AnalysisResult, "segue"), "overall_score"),
			pickFloat(analysisDisplay, "segue_score"),
		)
		resp.CriticalGap = pickBoolPtr(
			pickBool(resp.AnalysisResult, "critical_gap"),
			pickBool(analysisDisplay, "critical_gap"),
		)
		resp.AnalysisSummary = normalizeInsightSummary(resp.AnalysisSummary, resp.AnalysisResult)
	}

	if r.RecordingStartedAt != nil {
		formatted := formatDBLocalTime(*r.RecordingStartedAt)
		resp.RecordingStartedAt = &formatted
		resp.RecordedAt = &formatted
	}

	if r.RecordingEndedAt != nil {
		formatted := formatDBLocalTime(*r.RecordingEndedAt)
		resp.RecordingEndedAt = &formatted
	}

	if r.ProcessedAt != nil {
		formatted := formatDBLocalTime(*r.ProcessedAt)
		resp.ProcessedAt = &formatted
	}

	populateCompatibilityFields(resp)
	return resp
}

func formatDBLocalTime(t time.Time) string {
	// DB uses timestamp without timezone; keep wall-clock semantics in API output.
	localWallClock := time.Date(
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(),
		time.Local,
	)
	return localWallClock.Format("2006-01-02T15:04:05Z07:00")
}

func (s *Service) enrichRecordingResponse(ctx context.Context, recordingID int64, resp *RecordingResponse) error {
	if resp == nil {
		return nil
	}

	// Expose raw source payloads as-is for all roles so every page reads the same
	// authoritative transcript inputs and avoids per-page reconstruction drift.
	if cleaned, err := s.loadCleanedTranscriptionSegments(ctx, recordingID); err == nil && len(cleaned) > 0 {
		resp.CleanedTranscription = cleaned
	}
	if segments, err := s.loadRawTranscriptionSegments(ctx, recordingID); err == nil && len(segments) > 0 {
		resp.TranscriptionSegments = segments
	}

	// Frontdesk recordings: the worker pipeline saves a complete aggregate
	// to recordings.analysis_result with role="frontdesk". Use it directly
	// instead of overriding with individual prompt-step results.
	if resp.AnalysisResult != nil {
		if role, _ := resp.AnalysisResult["role"].(string); role == "frontdesk" {
			if resp.AnalysisSummary == nil {
				resp.AnalysisSummary = pickMap(resp.AnalysisResult, "analysis_summary")
			}
			if resp.ConversationSummary == nil {
				resp.ConversationSummary = pickStringPtr(pickString(resp.AnalysisResult, "summary"))
			}
			// Load cleaned transcription segments for structured display
			if cleaned, err := s.loadCleanedTranscriptionSegments(ctx, recordingID); err == nil && len(cleaned) > 0 {
				resp.StructuredTranscript = cleaned
			}
			return nil
		}
	}

	type analysisRow struct {
		PromptCode string
		ResultData map[string]interface{}
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT prompt_code, COALESCE(result_data, '{}'::json)
		FROM recording_analysis_results
		WHERE recording_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 12
	`, recordingID)
	if err != nil {
		return fmt.Errorf("failed to query analysis result: %w", err)
	}
	defer rows.Close()

	analysisRows := make([]analysisRow, 0, 12)
	for rows.Next() {
		var row analysisRow
		if scanErr := rows.Scan(&row.PromptCode, &row.ResultData); scanErr != nil {
			return fmt.Errorf("failed to scan analysis result: %w", scanErr)
		}
		analysisRows = append(analysisRows, row)
	}

	var (
		latestAny          map[string]interface{}
		latestConsultation map[string]interface{}
		latestStructured   map[string]interface{}
		latestNarrative    map[string]interface{}
		latestContentGen   map[string]interface{}
		latestOpsPlan      map[string]interface{}
	)
	for _, row := range analysisRows {
		code := strings.ToLower(strings.TrimSpace(row.PromptCode))
		data := row.ResultData
		if data == nil {
			continue
		}
		if latestAny == nil {
			latestAny = data
		}
		switch {
		case strings.Contains(code, "consultation_analysis"):
			if latestConsultation == nil {
				latestConsultation = data
			}
		case strings.Contains(code, "structured"):
			if latestStructured == nil {
				latestStructured = data
			}
		case strings.Contains(code, "narrative"):
			if latestNarrative == nil {
				latestNarrative = data
			}
		case strings.Contains(code, "content_gen"):
			if latestContentGen == nil {
				latestContentGen = data
			}
		case strings.Contains(code, "customer_operations_plan"):
			if latestOpsPlan == nil {
				latestOpsPlan = data
			}
		}
	}

	var analysisResult map[string]interface{}
	var analysisSummary map[string]interface{}
	switch {
	case latestConsultation != nil:
		analysisResult = latestConsultation
	case latestStructured != nil:
		analysisResult = latestStructured
	case latestNarrative != nil:
		analysisResult = latestNarrative
	default:
		analysisResult = latestAny
	}
	if summary := pickMap(analysisResult, "analysis_summary"); summary != nil {
		analysisSummary = summary
	}
	if summary := pickMap(latestConsultation, "analysis_summary"); summary != nil {
		analysisSummary = summary
	}
	if final := pickMap(latestNarrative, "final"); final != nil {
		if analysisSummary == nil {
			analysisSummary = map[string]interface{}{}
		}
		mergeMap(analysisSummary, final)
	}
	if summary := pickMap(latestOpsPlan, "analysis_summary"); summary != nil {
		if analysisSummary == nil {
			analysisSummary = map[string]interface{}{}
		}
		mergeMap(analysisSummary, summary)
	}

	if latestContentGen != nil && resp.EMRDraft == nil {
		if emr := pickMap(latestContentGen, "emr"); emr != nil {
			resp.EMRDraft = map[string]interface{}{
				"emr_content": emr,
			}
			if conf := pickString(emr, "confidence"); conf != "" {
				resp.EMRDraft["confidence"] = conf
			}
			if missing, ok := emr["missing_fields"]; ok {
				resp.EMRDraft["missing_fields"] = missing
			}
		}
	}
	if latestContentGen != nil && len(resp.ContentSeeds) == 0 {
		resp.ContentSeeds = toMapSlice(pickArray(latestContentGen, "content_seeds"))
	}
	if len(resp.ContentSeeds) == 0 {
		resp.ContentSeeds = toMapSlice(firstNonEmptyArray(
			pickArray(latestConsultation, "content_seeds"),
			pickArray(analysisResult, "content_seeds"),
			pickArray(analysisSummary, "content_seeds"),
		))
	}

	if analysisResult != nil {
		if resp.AnalysisResult == nil {
			resp.AnalysisResult = analysisResult
		} else {
			// Do not overwrite already persisted aggregate fields with step-level rows.
			mergeMap(resp.AnalysisResult, analysisResult)
		}
	}
	if analysisSummary != nil {
		resp.AnalysisSummary = analysisSummary
	}
	resp.AnalysisSummary = normalizeInsightSummary(resp.AnalysisSummary, resp.AnalysisResult)

	if resp.SceneType == nil {
		resp.SceneType = pickStringPtr(
			pickString(resp.AnalysisResult, "scene_type"),
			pickString(resp.AnalysisResult, "scene"),
			pickString(resp.AnalysisSummary, "scene_type"),
		)
	}
	if resp.VisitOutcome == nil {
		resp.VisitOutcome = pickStringPtr(
			pickString(resp.AnalysisResult, "visit_outcome"),
			pickString(resp.AnalysisResult, "decision_status"),
			pickNestedStatus(resp.AnalysisSummary, "visit_outcome"),
			pickString(resp.AnalysisSummary, "visit_outcome"),
			pickString(resp.AnalysisSummary, "status"),
		)
	}
	if resp.SubjectiveSummary == nil {
		resp.SubjectiveSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "subjective_summary"),
			pickString(resp.AnalysisResult, "conversation_summary"),
			pickString(resp.AnalysisSummary, "summary"),
			pickString(resp.AnalysisSummary, "critical_summary"),
		)
	}
	if resp.ConversationSummary == nil {
		resp.ConversationSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "conversation_summary"),
			pickString(resp.AnalysisDisplay, "conversation_summary"),
			pickString(resp.AnalysisResult, "subjective_summary"),
			pickString(resp.AnalysisSummary, "summary"),
			pickString(resp.AnalysisSummary, "critical_summary"),
		)
	}
	if resp.StatusSummary == nil {
		resp.StatusSummary = pickStringPtr(
			pickString(resp.AnalysisResult, "status_summary"),
			pickString(resp.AnalysisSummary, "current_state"),
			pickString(resp.AnalysisSummary, "critical_summary"),
		)
	}
	if len(resp.KeyQuotes) == 0 {
		resp.KeyQuotes = pickStringSlice(
			pickArray(resp.AnalysisResult, "key_quotes"),
			pickArray(resp.AnalysisResult, "highlights"),
			pickArray(resp.AnalysisDisplay, "key_quotes"),
			pickArray(resp.AnalysisDisplay, "highlights"),
			pickArray(resp.AnalysisSummary, "highlights"),
		)
	}
	if resp.DealOutcome == nil {
		resp.DealOutcome = pickMap(resp.AnalysisResult, "deal_outcome")
	}
	if resp.SuggestedTask == nil {
		resp.SuggestedTask = pickMap(resp.AnalysisResult, "suggested_task")
	}
	if resp.ConsultationRecord == nil {
		resp.ConsultationRecord = pickMap(resp.AnalysisResult, "consultation_record")
	}
	if resp.Report == nil {
		resp.Report = pickStringPtr(
			pickString(resp.AnalysisResult, "report"),
			pickString(resp.AnalysisResult, "feedback_report"),
			pickString(resp.AnalysisSummary, "feedback_report"),
			pickString(resp.AnalysisSummary, "critical_summary"),
		)
	}
	if resp.QualityScore == nil {
		resp.QualityScore = pickFloatPtr(
			pickFloat(resp.AnalysisResult, "quality_score"),
			pickFloat(resp.AnalysisResult, "quality"),
		)
	}
	if resp.SegueScore == nil {
		resp.SegueScore = pickFloatPtr(
			pickFloat(resp.AnalysisResult, "segue_score"),
			pickFloat(resp.AnalysisResult, "overall_score"),
			pickFloat(pickMap(resp.AnalysisResult, "segue"), "overall_score"),
		)
	}
	if resp.CriticalGap == nil {
		resp.CriticalGap = pickBoolPtr(
			pickBool(resp.AnalysisSummary, "critical_gap"),
			pickBool(resp.AnalysisResult, "critical_gap"),
		)
	}
	if resp.RouteReview == nil {
		resp.RouteReview = pickMap(resp.AnalysisResult, "route_review")
		if resp.RouteReview == nil {
			resp.RouteReview = pickMap(resp.AnalysisSummary, "route_review")
		}
	}
	if len(resp.ContentSeeds) == 0 {
		contentSeedsRaw := pickArray(resp.AnalysisResult, "content_seeds")
		if len(contentSeedsRaw) == 0 {
			contentSeedsRaw = pickArray(resp.AnalysisSummary, "content_seeds")
		}
		resp.ContentSeeds = toMapSlice(contentSeedsRaw)
	}
	if len(resp.StructuredTranscript) == 0 {
		resp.StructuredTranscript = toMapSlice(
			firstNonEmptyArray(
				pickArray(resp.AnalysisResult, "annotated_transcription"),
				pickArray(resp.AnalysisResult, "structured_transcript"),
				pickArray(resp.AnalysisResult, "structured_transcription"),
				pickArray(resp.AnalysisResult, "transcript_structured"),
				pickArray(latestConsultation, "annotated_transcription"),
				pickArray(resp.AnalysisSummary, "structured_transcript"),
				pickArray(resp.AnalysisSummary, "structured_transcription"),
			),
		)
	}
	if len(resp.TimelineTranscript) == 0 {
		resp.TimelineTranscript = toMapSlice(
			firstNonEmptyArray(
				pickArray(resp.AnalysisResult, "timeline_transcript"),
				pickArray(resp.AnalysisResult, "timeline_transcription"),
				pickArray(resp.AnalysisResult, "transcript_timeline"),
				pickArray(resp.AnalysisSummary, "timeline_transcript"),
				pickArray(resp.AnalysisSummary, "timeline_transcription"),
			),
		)
	}
	if len(resp.StructuredTranscript) == 0 || len(resp.TimelineTranscript) == 0 {
		if cleaned := resp.CleanedTranscription; len(cleaned) > 0 {
			structured, timeline := buildTranscriptFromSegments(cleaned)
			if len(resp.StructuredTranscript) == 0 {
				resp.StructuredTranscript = structured
			}
			if len(resp.TimelineTranscript) == 0 {
				resp.TimelineTranscript = timeline
			}
		}
	}
	if len(resp.StructuredTranscript) == 0 || len(resp.TimelineTranscript) == 0 {
		if segments := resp.TranscriptionSegments; len(segments) > 0 {
			structured, timeline := buildTranscriptFromSegments(segments)
			if len(resp.StructuredTranscript) == 0 {
				resp.StructuredTranscript = structured
			}
			if len(resp.TimelineTranscript) == 0 {
				resp.TimelineTranscript = timeline
			}
		}
	}
	if len(resp.StructuredTranscript) == 0 && len(resp.TimelineTranscript) == 0 && resp.TranscriptText != nil {
		resp.StructuredTranscript, resp.TimelineTranscript = buildTranscriptFallback(*resp.TranscriptText)
	}

	if resp.EMRDraft == nil {
		var (
			patientID            *int64
			patientNameExtracted *string
			patientMatchSource   *string
			emrContent           map[string]interface{}
			confidence           *string
			missingFields        interface{}
			isConfirmed          *bool
			confirmedAt          interface{}
		)
		err = s.store.pool.QueryRow(ctx, `
			SELECT
				patient_id,
				NULLIF(patient_name_extracted, ''),
				NULLIF(patient_match_source, ''),
				COALESCE(emr_content::jsonb, '{}'::jsonb),
				NULLIF(confidence, ''),
				COALESCE(missing_fields::jsonb, '[]'::jsonb),
				is_confirmed,
				confirmed_at
			FROM recording_emr_drafts
			WHERE recording_id = $1
			ORDER BY generated_at DESC NULLS LAST, id DESC
			LIMIT 1
		`, recordingID).Scan(
			&patientID,
			&patientNameExtracted,
			&patientMatchSource,
			&emrContent,
			&confidence,
			&missingFields,
			&isConfirmed,
			&confirmedAt,
		)
		if err == nil {
			draft := map[string]interface{}{
				"emr_content": emrContent,
			}
			if patientID != nil {
				draft["patient_id"] = *patientID
			}
			if patientNameExtracted != nil {
				draft["patient_name_extracted"] = *patientNameExtracted
			}
			if patientMatchSource != nil {
				draft["patient_match_source"] = *patientMatchSource
			}
			if confidence != nil {
				draft["confidence"] = *confidence
			}
			if missingFields != nil {
				draft["missing_fields"] = missingFields
			}
			if isConfirmed != nil {
				draft["is_confirmed"] = *isConfirmed
			}
			if confirmedAt != nil {
				draft["confirmed_at"] = confirmedAt
			}
			resp.EMRDraft = draft
		}
	}
	if resp.AnalysisDisplay == nil {
		resp.AnalysisDisplay = map[string]interface{}{}
	}
	if resp.AnalysisResult != nil {
		resp.AnalysisDisplay["analysis_result"] = resp.AnalysisResult
	}
	if resp.AnalysisSummary != nil {
		resp.AnalysisDisplay["analysis_summary"] = resp.AnalysisSummary
	}
	if resp.RouteReview != nil {
		resp.AnalysisDisplay["route_review"] = resp.RouteReview
	}
	if resp.EMRDraft != nil {
		resp.AnalysisDisplay["emr_draft"] = resp.EMRDraft
	}
	if len(resp.ContentSeeds) > 0 {
		resp.AnalysisDisplay["content_seeds"] = resp.ContentSeeds
		if resp.AnalysisResult != nil {
			if _, exists := resp.AnalysisResult["content_seeds"]; !exists {
				resp.AnalysisResult["content_seeds"] = resp.ContentSeeds
			}
		}
	}
	if len(resp.StructuredTranscript) > 0 {
		resp.AnalysisDisplay["structured_transcript"] = resp.StructuredTranscript
		resp.AnalysisDisplay["structured_transcription"] = resp.StructuredTranscript
		if resp.AnalysisResult != nil {
			if _, exists := resp.AnalysisResult["structured_transcript"]; !exists {
				resp.AnalysisResult["structured_transcript"] = resp.StructuredTranscript
			}
			if _, exists := resp.AnalysisResult["structured_transcription"]; !exists {
				resp.AnalysisResult["structured_transcription"] = resp.StructuredTranscript
			}
		}
	}
	if len(resp.TimelineTranscript) > 0 {
		resp.AnalysisDisplay["timeline_transcript"] = resp.TimelineTranscript
		resp.AnalysisDisplay["timeline_transcription"] = resp.TimelineTranscript
		if resp.AnalysisResult != nil {
			if _, exists := resp.AnalysisResult["timeline_transcript"]; !exists {
				resp.AnalysisResult["timeline_transcript"] = resp.TimelineTranscript
			}
			if _, exists := resp.AnalysisResult["timeline_transcription"]; !exists {
				resp.AnalysisResult["timeline_transcription"] = resp.TimelineTranscript
			}
		}
	}

	populateCompatibilityFields(resp)
	return nil
}

func mergeMap(dst map[string]interface{}, src map[string]interface{}) {
	if dst == nil || src == nil {
		return
	}
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (s *Service) rebuildRecordingAnalysisPayload(ctx context.Context, recordingID int64, resp *RecordingResponse) error {
	if resp == nil {
		return nil
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT prompt_code, COALESCE(result_data, '{}'::json), COALESCE(is_active, false), created_at, id
		FROM recording_analysis_results
		WHERE recording_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 32
	`, recordingID)
	if err != nil {
		return fmt.Errorf("failed to query analysis rows: %w", err)
	}
	defer rows.Close()

	type analysisRow struct {
		promptCode string
		resultData map[string]interface{}
		isActive   bool
		createdAt  time.Time
		id         int64
	}
	collected := make([]analysisRow, 0, 32)
	for rows.Next() {
		var row analysisRow
		if scanErr := rows.Scan(&row.promptCode, &row.resultData, &row.isActive, &row.createdAt, &row.id); scanErr != nil {
			return fmt.Errorf("failed to scan analysis row: %w", scanErr)
		}
		collected = append(collected, row)
	}
	if rows.Err() != nil {
		return fmt.Errorf("failed to iterate analysis rows: %w", rows.Err())
	}

	analysisResults := make([]map[string]interface{}, 0, len(collected))
	rebuilt := cloneMap(resp.AnalysisResult)
	for _, row := range collected {
		item := map[string]interface{}{
			"prompt_code":   row.promptCode,
			"is_active":     row.isActive,
			"created_at":    row.createdAt.Format(time.RFC3339),
			"id":            row.id,
			"analysis_json": row.resultData,
		}
		analysisResults = append(analysisResults, item)
		if rebuilt == nil {
			rebuilt = cloneMap(row.resultData)
			continue
		}
		mergeMap(rebuilt, row.resultData)
	}
	if rebuilt != nil {
		hydrateTherapistResetFields(rebuilt, analysisResults)
	}

	if rebuilt != nil {
		resp.AnalysisResult = rebuilt
		if resp.AnalysisDisplay == nil {
			resp.AnalysisDisplay = map[string]interface{}{}
		}
		resp.AnalysisDisplay["analysis_result"] = rebuilt
	}
	if len(analysisResults) > 0 {
		resp.AnalysisResults = analysisResults
		if resp.AnalysisDisplay == nil {
			resp.AnalysisDisplay = map[string]interface{}{}
		}
		resp.AnalysisDisplay["analysis_results"] = analysisResults
	}
	return nil
}

func hydrateTherapistResetFields(rebuilt map[string]interface{}, analysisResults []map[string]interface{}) {
	if rebuilt == nil {
		return
	}
	ensureRaw := func() map[string]interface{} {
		raw := pickMap(rebuilt, "raw")
		if raw == nil {
			raw = map[string]interface{}{}
			rebuilt["raw"] = raw
		}
		return raw
	}
	ensureTherapistReset := func(raw map[string]interface{}) map[string]interface{} {
		tra := pickMap(raw, "therapist_reset_analysis")
		if tra == nil {
			tra = map[string]interface{}{}
			raw["therapist_reset_analysis"] = tra
		}
		return tra
	}
	setIfMissing := func(key string, value interface{}) {
		if value == nil {
			return
		}
		if _, ok := rebuilt[key]; !ok {
			rebuilt[key] = value
		}
	}

	for _, row := range analysisResults {
		code := strings.ToLower(strings.TrimSpace(pickString(row, "prompt_code")))
		if !strings.Contains(code, "therapist_reset") && !strings.Contains(code, "reset_analysis") {
			continue
		}
		payload := pickMap(row, "analysis_json")
		if payload == nil {
			continue
		}
		normalized := payload
		if nested := pickMap(payload, "analysis_result"); nested != nil {
			normalized = nested
		}
		if nested := pickMap(payload, "data"); nested != nil && len(normalized) == 0 {
			normalized = nested
		}

		raw := ensureRaw()
		tra := ensureTherapistReset(raw)
		mergeMap(tra, normalized)

		setIfMissing("dimension_scores", pickMap(tra, "dimension_scores"))
		if rp, ok := tra["reset_percent"]; ok {
			setIfMissing("reset_percent", rp)
		}
		if cg, ok := tra["critical_gap"]; ok {
			setIfMissing("critical_gap", cg)
		}
		if cmi, ok := tra["critical_missing_items"]; ok {
			setIfMissing("critical_missing_items", cmi)
		}
		if hl, ok := tra["highlights"]; ok {
			setIfMissing("highlights", hl)
		}
		if ip, ok := tra["improvement_priorities"]; ok {
			setIfMissing("improvement_priorities", ip)
		}
		if ra, ok := tra["recommended_actions"]; ok {
			setIfMissing("recommended_actions", ra)
		}

		resetNode := pickMap(tra, "reset")
		if resetNode != nil {
			if items, ok := resetNode["items"]; ok {
				setIfMissing("reset_items", items)
			}
			if _, ok := rebuilt["reset"]; !ok {
				rebuilt["reset"] = resetNode
			}
		}
	}
}

func normalizeInsightSummary(summary map[string]interface{}, result map[string]interface{}) map[string]interface{} {
	if result == nil {
		return summary
	}
	out := summary
	if out == nil {
		out = map[string]interface{}{}
	}

	if _, exists := out["patient_mindset"]; !exists {
		if patientMindset := pickMap(result, "patient_mindset"); patientMindset != nil {
			out["patient_mindset"] = patientMindset
		}
	}

	business := pickMap(result, "business")
	if business != nil {
		if _, exists := out["care_progress"]; !exists {
			if careProgress := pickMap(business, "care_progress"); careProgress != nil {
				out["care_progress"] = careProgress
			}
		}
		if _, exists := out["visit_outcome"]; !exists {
			if visitOutcome := pickMap(business, "visit_outcome"); visitOutcome != nil {
				out["visit_outcome"] = visitOutcome
			}
		}
		for _, key := range []string{"core_blockers", "conversion_opportunity", "current_state"} {
			if _, exists := out[key]; exists {
				continue
			}
			if value, ok := business[key]; ok && value != nil {
				out[key] = value
			}
		}
	}

	return out
}

func looksLikeAnalysisResult(source map[string]interface{}) bool {
	if source == nil {
		return false
	}
	for _, key := range []string{
		"scene_type", "scene", "segue_percent", "segue_score", "critical_gap",
		"business", "patient_mindset", "analysis_summary", "visit_outcome",
		"chief_complaint", "diagnosis", "consultation_record", "decision_status",
	} {
		if _, ok := source[key]; ok {
			return true
		}
	}
	return false
}

func populateCompatibilityFields(resp *RecordingResponse) {
	if resp == nil {
		return
	}
	if resp.Duration == nil {
		resp.Duration = resp.RecordingDuration
	}
	if resp.AnalysisStatus == nil || strings.TrimSpace(*resp.AnalysisStatus) == "" {
		resp.AnalysisStatus = pickStringPtr(resp.Status)
	}
	if resp.RecordedAt == nil {
		resp.RecordedAt = resp.RecordingStartedAt
	}

	analysisDisplay := resp.AnalysisDisplay
	analysisResult := resp.AnalysisResult
	analysisSummary := resp.AnalysisSummary
	if analysisSummary == nil {
		analysisSummary = pickMap(analysisResult, "analysis_summary")
	}
	analysisResultBusiness := pickMap(analysisResult, "business")
	analysisSummaryBusiness := pickMap(analysisSummary, "business")
	analysisDisplayBusiness := pickMap(analysisDisplay, "business")
	analysisResultDoctorContentGen := pickMap(analysisResult, "doctor_content_gen")
	analysisResultRawDoctorContentGen := pickMap(pickMap(analysisResult, "raw"), "doctor_content_gen")
	emrDraft := firstNonNilMap(resp.EMRDraft, pickMap(analysisDisplay, "emr_draft"), pickMap(analysisResult, "emr_draft"))
	emrContent := firstNonNilMap(
		pickMap(analysisResult, "emr"),
		pickMap(analysisResultDoctorContentGen, "emr"),
		pickMap(analysisResultRawDoctorContentGen, "emr"),
		pickMap(emrDraft, "emr_content"),
		pickMap(pickMap(analysisResult, "emr_draft"), "emr_content"),
		pickMap(pickMap(analysisDisplay, "emr_draft"), "emr_content"),
	)

	if resp.ConversationSummary == nil {
		resp.ConversationSummary = pickStringPtr(
			pickString(analysisResult, "conversation_summary"),
			pickString(analysisDisplay, "conversation_summary"),
			pickString(analysisResult, "subjective_summary"),
			pickString(analysisSummary, "summary"),
			pickString(analysisSummary, "critical_summary"),
			pickString(analysisResult, "status_summary"),
			pickString(analysisSummary, "current_state"),
			pickString(analysisDisplay, "status_summary"),
			pickString(analysisDisplay, "chief_complaint"),
		)
	}
	if resp.StatusSummary == nil {
		resp.StatusSummary = pickStringPtr(
			pickString(analysisResult, "status_summary"),
			pickString(analysisSummary, "current_state"),
			pickString(analysisSummary, "critical_summary"),
			pickString(analysisDisplay, "status_summary"),
		)
	}
	if len(resp.KeyQuotes) == 0 {
		resp.KeyQuotes = pickStringSlice(
			pickArray(analysisResult, "key_quotes"),
			pickArray(analysisResult, "highlights"),
			pickArray(analysisDisplay, "key_quotes"),
			pickArray(analysisDisplay, "highlights"),
			pickArray(analysisSummary, "highlights"),
		)
	}

	if resp.SeguePercent == nil {
		resp.SeguePercent = pickFloatPtr(
			pickFloat(analysisResult, "segue_percent"),
			pickFloat(analysisResult, "segue_score"),
			pickFloat(analysisResult, "communication_score"),
			pickFloat(pickMap(analysisResult, "segue"), "overall_score"),
			pickFloat(pickMap(analysisResult, "segue"), "segue_percent"),
			pickFloat(analysisDisplay, "segue_percent"),
			pickFloat(analysisDisplay, "segue_score"),
			pickFloat(pickMap(analysisDisplay, "segue_scores"), "overall"),
		)
	}
	if resp.SegueScore == nil {
		resp.SegueScore = resp.SeguePercent
	}
	if resp.CriticalGap == nil {
		resp.CriticalGap = pickBoolPtr(
			pickBool(analysisResult, "critical_gap"),
			pickBool(analysisSummary, "critical_gap"),
			pickBool(analysisDisplay, "critical_gap"),
		)
	}

	visitOutcomeStatus := pickStringPtr(
		pickNestedStatus(analysisResult, "visit_outcome"),
		pickNestedStatus(analysisResultBusiness, "visit_outcome"),
		pickNestedStatus(analysisSummary, "visit_outcome"),
		pickNestedStatus(analysisSummaryBusiness, "visit_outcome"),
		pickNestedStatus(analysisDisplay, "visit_outcome"),
		pickNestedStatus(analysisDisplayBusiness, "visit_outcome"),
		pickString(analysisResult, "visit_outcome_status"),
		pickString(analysisResult, "visit_outcome"),
		pickString(analysisResult, "decision_status"),
		pickString(analysisSummary, "visit_outcome_status"),
		pickString(analysisSummary, "visit_outcome"),
		pickString(analysisDisplay, "visit_outcome_status"),
		pickString(analysisDisplay, "visit_outcome"),
	)
	resp.VisitOutcomeStatus = visitOutcomeStatus
	if resp.VisitOutcome == nil {
		resp.VisitOutcome = visitOutcomeStatus
	}

	resp.DecisionStatus = pickStringPtr(
		pickString(analysisResult, "decision_status"),
		pickString(analysisSummary, "decision_status"),
		pickString(analysisDisplay, "decision_status"),
	)
	resp.SoapSubjectiveSummary = pickStringPtr(
		pickString(analysisResult, "subjective_summary"),
		pickString(analysisResult, "soap_subjective_summary"),
		pickString(analysisResult, "summary"),
		pickString(analysisSummary, "subjective_summary"),
		pickString(analysisDisplay, "subjective_summary"),
	)
	resp.ChiefComplaint = pickStringPtr(
		pickString(analysisResult, "chief_complaint"),
		pickString(emrContent, "chief_complaint"),
		pickString(pickMap(analysisResult, "consultation_record"), "chief_complaint"),
		pickString(pickMap(analysisResult, "emr_draft"), "chief_complaint"),
		pickString(pickMap(pickMap(analysisResult, "emr_draft"), "emr_content"), "chief_complaint"),
		pickString(pickMap(pickMap(analysisDisplay, "emr_draft"), "emr_content"), "chief_complaint"),
		pickString(pickMap(resp.EMRDraft, "emr_content"), "chief_complaint"),
	)
	resp.Diagnosis = pickStringPtr(
		pickString(analysisResult, "diagnosis"),
		pickString(emrContent, "diagnosis"),
		pickString(pickMap(analysisResult, "consultation_record"), "diagnosis"),
	)
	resp.CurrentState = pickStringPtr(
		pickString(analysisSummary, "current_state"),
		pickString(analysisResult, "current_state"),
		pickString(pickMap(analysisResult, "business"), "current_state"),
	)

	if resp.RouteReview == nil {
		resp.RouteReview = pickMap(analysisResult, "route_review")
		if resp.RouteReview == nil {
			resp.RouteReview = pickMap(analysisDisplay, "route_review")
		}
	}
	resp.RouteReviewRequired = pickBoolPtr(
		pickBool(resp.RouteReview, "required"),
		pickBool(resp.RouteReview, "route_review_required"),
		pickBool(analysisResult, "route_review_required"),
		pickBool(analysisSummary, "route_review_required"),
	)
	resp.RouteReviewReason = pickStringPtr(
		pickString(resp.RouteReview, "reason"),
		pickString(resp.RouteReview, "route_review_reason"),
		pickString(analysisResult, "route_review_reason"),
		pickString(analysisSummary, "route_review_reason"),
	)
	resp.RouteAutoDecision = pickStringPtr(
		pickString(resp.RouteReview, "auto_decision"),
		pickString(resp.RouteReview, "route_auto_decision"),
		pickString(analysisResult, "route_auto_decision"),
		pickString(analysisSummary, "route_auto_decision"),
	)
	resp.RouteReviewStatus = pickStringPtr(
		pickString(resp.RouteReview, "status"),
		pickString(resp.RouteReview, "route_review_status"),
		pickString(analysisResult, "route_review_status"),
		pickString(analysisSummary, "route_review_status"),
	)
	if resp.RouteReviewRequired == nil {
		resp.RouteReviewRequired = boolPtr(false)
	}
	if resp.RouteReviewReason == nil {
		resp.RouteReviewReason = literalStringPtr("")
	}
	if resp.RouteAutoDecision == nil {
		resp.RouteAutoDecision = literalStringPtr("")
	}
	if resp.RouteReviewStatus == nil {
		resp.RouteReviewStatus = literalStringPtr("pending")
	}

	if len(resp.ContentSeeds) == 0 {
		resp.ContentSeeds = toMapSlice(firstNonEmptyArray(
			pickArray(analysisResult, "content_seeds"),
			pickArray(analysisResultDoctorContentGen, "content_seeds"),
			pickArray(analysisResultRawDoctorContentGen, "content_seeds"),
			pickArray(analysisSummary, "content_seeds"),
			pickArray(analysisDisplay, "content_seeds"),
		))
	}

	if len(resp.ContentSeeds) > 0 {
		types := make([]string, 0, len(resp.ContentSeeds))
		seen := map[string]struct{}{}
		for _, seed := range resp.ContentSeeds {
			if seedType := strings.TrimSpace(pickString(seed, "seed_type")); seedType != "" {
				if _, ok := seen[seedType]; !ok {
					seen[seedType] = struct{}{}
					types = append(types, seedType)
				}
			}
		}
		resp.ContentSeedsCount = len(resp.ContentSeeds)
		resp.ContentSeedsTypes = types
	} else if resp.ContentSeedsTypes == nil {
		resp.ContentSeedsTypes = []string{}
	}

	if resp.EMRStatus == nil {
		if draft := firstNonNilMap(resp.EMRDraft, pickMap(analysisDisplay, "emr_draft"), pickMap(analysisResult, "emr_draft")); draft != nil {
			if confirmed := pickBool(draft, "is_confirmed"); confirmed != nil {
				if *confirmed {
					resp.EMRStatus = pickStringPtr("confirmed")
				} else {
					resp.EMRStatus = pickStringPtr("draft")
				}
			}
		}
	}
	if resp.EMRStatus == nil {
		resp.EMRStatus = pickStringPtr(
			pickString(analysisDisplay, "emr_status"),
			pickString(analysisResult, "emr_status"),
		)
	}
}

func firstNonNilMap(candidates ...map[string]interface{}) map[string]interface{} {
	for _, candidate := range candidates {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}

func pickMap(source map[string]interface{}, key string) map[string]interface{} {
	if source == nil {
		return nil
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		return v
	case JSONObject:
		return map[string]interface{}(v)
	default:
		return nil
	}
}

func pickNestedStatus(source map[string]interface{}, key string) string {
	m := pickMap(source, key)
	if m == nil {
		return ""
	}
	return pickString(m, "status")
}

func pickArray(source map[string]interface{}, key string) []interface{} {
	if source == nil {
		return nil
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		return v
	case JSONArray:
		return []interface{}(v)
	default:
		return nil
	}
}

func pickStringSlice(candidates ...[]interface{}) []string {
	for _, candidate := range candidates {
		if len(candidate) == 0 {
			continue
		}
		result := make([]string, 0, len(candidate))
		for _, raw := range candidate {
			text := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if text == "" || text == "<nil>" {
				continue
			}
			result = append(result, text)
		}
		if len(result) > 0 {
			return result
		}
	}
	return nil
}

func toMapSlice(items []interface{}) []map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(items))
	for _, raw := range items {
		switch v := raw.(type) {
		case map[string]interface{}:
			result = append(result, v)
		case JSONObject:
			result = append(result, map[string]interface{}(v))
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func firstNonEmptyArray(candidates ...[]interface{}) []interface{} {
	for _, candidate := range candidates {
		if len(candidate) > 0 {
			return candidate
		}
	}
	return nil
}

func firstNonEmptyMap(candidates ...map[string]interface{}) map[string]interface{} {
	for _, candidate := range candidates {
		if len(candidate) > 0 {
			return candidate
		}
	}
	return nil
}

func toScoreMap(source map[string]interface{}) map[string]float64 {
	if len(source) == 0 {
		return map[string]float64{}
	}
	out := map[string]float64{}
	for k, v := range source {
		switch n := v.(type) {
		case float64:
			out[k] = n
		case float32:
			out[k] = float64(n)
		case int:
			out[k] = float64(n)
		case int64:
			out[k] = float64(n)
		case json.Number:
			if f, err := n.Float64(); err == nil {
				out[k] = f
			}
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				out[k] = f
			}
		}
	}
	return out
}

func buildTranscriptFallback(text string) ([]map[string]interface{}, []map[string]interface{}) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Split(trimmed, "\n")
	structured := make([]map[string]interface{}, 0, len(lines))
	timeline := make([]map[string]interface{}, 0, len(lines))
	cursor := 0
	for _, line := range lines {
		content := strings.TrimSpace(line)
		if content == "" {
			continue
		}
		speaker := "对话"
		if idx := strings.Index(content, ":"); idx > 0 && idx < 12 {
			candidate := strings.TrimSpace(content[:idx])
			if candidate != "" {
				speaker = candidate
				content = strings.TrimSpace(content[idx+1:])
			}
		}
		if content == "" {
			continue
		}
		duration := estimateDurationSeconds(content)
		start := cursor
		end := start + duration
		structured = append(structured, map[string]interface{}{
			"speaker": speaker,
			"content": content,
		})
		timeline = append(timeline, map[string]interface{}{
			"speaker":       speaker,
			"text":          content,
			"start_seconds": start,
			"end_seconds":   end,
		})
		cursor = end
	}
	if len(structured) == 0 {
		structured = []map[string]interface{}{{"speaker": "对话", "content": trimmed}}
		timeline = []map[string]interface{}{{"speaker": "对话", "text": trimmed, "start_seconds": 0, "end_seconds": estimateDurationSeconds(trimmed)}}
	}
	return structured, timeline
}

func buildTranscriptFromSegments(segments []map[string]interface{}) ([]map[string]interface{}, []map[string]interface{}) {
	if len(segments) == 0 {
		return nil, nil
	}
	structured := make([]map[string]interface{}, 0, len(segments))
	timeline := make([]map[string]interface{}, 0, len(segments))
	cursor := 0
	for _, segment := range segments {
		content := firstNonEmptyText(
			segment["text"],
			segment["content"],
			segment["transcript"],
			segment["utterance"],
		)
		if content == "" {
			continue
		}
		speaker := firstNonEmptyText(segment["speaker_role"], segment["speaker"], segment["role"])
		if speaker == "" {
			speaker = "unknown"
		}
		start := pickInt(segment["start_seconds"], segment["start_time"], segment["start"], segment["offset"])
		if start == nil {
			if startMs := pickInt(segment["start_ms"]); startMs != nil {
				value := *startMs / 1000
				start = &value
			}
		}
		end := pickInt(segment["end_seconds"], segment["end_time"], segment["end"])
		if end == nil {
			if endMs := pickInt(segment["end_ms"]); endMs != nil {
				value := *endMs / 1000
				end = &value
			}
		}
		startSec := cursor
		if start != nil && *start >= 0 {
			startSec = *start
		}
		endSec := startSec + estimateDurationSeconds(content)
		if end != nil && *end >= startSec {
			endSec = *end
		}
		if endSec < startSec {
			endSec = startSec
		}
		structured = append(structured, map[string]interface{}{
			"speaker": speaker,
			"content": content,
		})
		timeline = append(timeline, map[string]interface{}{
			"speaker":       speaker,
			"text":          content,
			"start_seconds": startSec,
			"end_seconds":   endSec,
		})
		cursor = endSec
	}
	if len(structured) == 0 {
		return nil, nil
	}
	return structured, timeline
}

func (s *Service) loadRawTranscriptionSegments(ctx context.Context, recordingID int64) ([]map[string]interface{}, error) {
	var raw interface{}
	err := s.store.pool.QueryRow(ctx, `SELECT transcription_segments FROM recordings WHERE id = $1`, recordingID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return decodeSegmentsJSON(raw), nil
}

func (s *Service) loadCleanedTranscriptionSegments(ctx context.Context, recordingID int64) ([]map[string]interface{}, error) {
	var raw interface{}
	err := s.store.pool.QueryRow(ctx, `SELECT cleaned_transcription FROM recordings WHERE id = $1`, recordingID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return decodeSegmentsJSON(raw), nil
}

func decodeSegmentsJSON(raw interface{}) []map[string]interface{} {
	decoded := decodeNestedJSONValue(raw, 0)
	switch value := decoded.(type) {
	case []interface{}:
		return toMapSlice(value)
	case []map[string]interface{}:
		return value
	case map[string]interface{}:
		for _, key := range []string{"segments", "items", "data"} {
			if nested, ok := value[key]; ok {
				return decodeSegmentsJSON(nested)
			}
		}
	}
	return nil
}

func decodeNestedJSONValue(raw interface{}, depth int) interface{} {
	if depth > 3 || raw == nil {
		return raw
	}
	switch value := raw.(type) {
	case []byte:
		trimmed := strings.TrimSpace(string(value))
		if trimmed == "" {
			return nil
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return raw
		}
		return decodeNestedJSONValue(parsed, depth+1)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		if !(strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
			return value
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return value
		}
		return decodeNestedJSONValue(parsed, depth+1)
	default:
		return raw
	}
}

func firstNonEmptyText(values ...interface{}) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func pickInt(values ...interface{}) *int {
	for _, value := range values {
		if value == nil {
			continue
		}
		switch v := value.(type) {
		case int:
			out := v
			return &out
		case int32:
			out := int(v)
			return &out
		case int64:
			out := int(v)
			return &out
		case float32:
			out := int(v)
			return &out
		case float64:
			out := int(v)
			return &out
		case json.Number:
			if i64, err := v.Int64(); err == nil {
				out := int(i64)
				return &out
			}
			if f64, err := v.Float64(); err == nil {
				out := int(f64)
				return &out
			}
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				continue
			}
			if i64, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
				out := int(i64)
				return &out
			}
			if f64, err := strconv.ParseFloat(trimmed, 64); err == nil {
				out := int(f64)
				return &out
			}
		}
	}
	return nil
}

func estimateDurationSeconds(text string) int {
	runes := len([]rune(strings.TrimSpace(text)))
	if runes <= 0 {
		return 5
	}
	seconds := runes / 6
	if seconds < 5 {
		return 5
	}
	if seconds > 90 {
		return 90
	}
	return seconds
}

func pickString(source map[string]interface{}, key string) string {
	if source == nil {
		return ""
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	default:
		return ""
	}
}

func literalStringPtr(v string) *string {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

func pickStringPtr(values ...string) *string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			s := strings.TrimSpace(v)
			return &s
		}
	}
	return nil
}

func pickFloat(source map[string]interface{}, key string) *float64 {
	if source == nil {
		return nil
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case float64:
		return &v
	case float32:
		x := float64(v)
		return &x
	case int:
		x := float64(v)
		return &x
	case int64:
		x := float64(v)
		return &x
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		if x, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return &x
		}
	}
	return nil
}

func pickFloatPtr(values ...*float64) *float64 {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func pickBool(source map[string]interface{}, key string) *bool {
	if source == nil {
		return nil
	}
	raw, ok := source[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case bool:
		return &v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		if s == "true" || s == "1" || s == "yes" || s == "y" {
			b := true
			return &b
		}
		if s == "false" || s == "0" || s == "no" || s == "n" {
			b := false
			return &b
		}
	}
	return nil
}

func pickBoolPtr(values ...*bool) *bool {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// Recording Statistics Services

// GetStatsOverview retrieves overview statistics
func (s *Service) GetStatsOverview(ctx context.Context, tenantID int64, startDate, endDate *string) (*RecordingStatsOverviewResponse, error) {
	return s.store.GetStatsOverview(ctx, tenantID, startDate, endDate)
}

// GetStatsByScene retrieves statistics by scene
func (s *Service) GetStatsByScene(ctx context.Context, tenantID int64) ([]RecordingStatsBySceneResponse, error) {
	return s.store.GetStatsByScene(ctx, tenantID)
}

// GetStatsBySource retrieves statistics by source
func (s *Service) GetStatsBySource(ctx context.Context, tenantID int64) ([]RecordingStatsBySourceResponse, error) {
	return s.store.GetStatsBySource(ctx, tenantID)
}

// Recording Task Services

// ListRecordingTasks retrieves a paginated list of recording tasks
func (s *Service) ListRecordingTasks(ctx context.Context, req TaskListRequest) ([]*TaskResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	tasks, total, err := s.store.ListRecordingTasks(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*TaskResponse, len(tasks))
	for i, t := range tasks {
		responses[i] = toTaskResponse(t)
	}
	if err := s.enrichTaskListResponse(ctx, responses); err != nil {
		return nil, 0, err
	}
	shouldCompact := req.RecordingID == nil
	if shouldCompact {
		for _, item := range responses {
			compactTaskListItem(item)
		}
	}

	return responses, total, nil
}

// GetTask retrieves a recording task by ID
func (s *Service) GetTask(ctx context.Context, id int64) (*TaskResponse, error) {
	task, err := s.store.GetTaskByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toTaskResponse(task), nil
}

// CompleteTask marks a task as completed
func (s *Service) CompleteTask(ctx context.Context, id int64, completedBy int64, req CompleteTaskRequest) error {
	task, err := s.store.GetTaskByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.CompleteTask(ctx, id, completedBy, req.Notes); err != nil {
		return err
	}
	_ = s.appendTaskInteraction(ctx, task, completedBy, "task_completed", "任务已完成")
	return nil
}

// CancelTask marks a task as cancelled
func (s *Service) CancelTask(ctx context.Context, id int64, req CancelTaskRequest) error {
	if req.Reason == "" {
		return fmt.Errorf("cancel reason is required")
	}
	task, err := s.store.GetTaskByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.CancelTask(ctx, id, req.Reason); err != nil {
		return err
	}
	_ = s.appendTaskInteraction(ctx, task, 0, "task_cancelled", "任务已忽略: "+strings.TrimSpace(req.Reason))
	return nil
}

func (s *Service) appendTaskInteraction(ctx context.Context, task *RecordingTask, employeeID int64, typ, content string) error {
	if task == nil || task.RecordingID <= 0 {
		return nil
	}
	var customerID *int64
	var tenantID int64
	var recordingEmployeeID int64
	if err := s.store.pool.QueryRow(ctx, `
		SELECT customer_id, tenant_id, employee_id
		FROM recordings
		WHERE id = $1
	`, task.RecordingID).Scan(&customerID, &tenantID, &recordingEmployeeID); err != nil {
		return err
	}
	if customerID == nil || *customerID <= 0 {
		return nil
	}
	if employeeID <= 0 {
		employeeID = recordingEmployeeID
	}
	return s.appendCustomerInteraction(ctx, tenantID, *customerID, employeeID, typ, "system", task.RecordingID, content)
}

func (s *Service) appendCustomerInteraction(ctx context.Context, tenantID, customerID, employeeID int64, typ, direction string, recordingID int64, content string) error {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil
	}
	if err := tenancy.RequirePositiveID("customer_id", customerID); err != nil {
		return nil
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
		return nil
	}
	if strings.TrimSpace(typ) == "" {
		return nil
	}
	_, err := s.store.pool.Exec(ctx, `
		INSERT INTO customer_interactions (
			customer_id, tenant_id, type, direction, content, duration, recording_id, employee_id, interacted_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, NULLIF($5, ''), NULL, $6, $7, NOW(), NOW(), NOW()
		)
	`, customerID, tenantID, typ, direction, strings.TrimSpace(content), recordingID, employeeID)
	if err != nil {
		return err
	}
	_, _ = s.store.pool.Exec(ctx, `UPDATE customers SET last_contacted_at = NOW(), updated_at = NOW() WHERE id = $1`, customerID)
	return nil
}

// GetTaskStats retrieves task statistics
func (s *Service) GetTaskStats(ctx context.Context, tenantID int64, tenantIDs []int64, assignedTo *int64) (*RecordingTaskStatsResponse, error) {
	return s.store.GetTaskStats(ctx, tenantID, tenantIDs, assignedTo)
}

// GetDailyBriefing retrieves a daily briefing
func (s *Service) GetDailyBriefing(ctx context.Context, tenantID int64, assignedTo *int64, date time.Time) (*DailyBriefingResponse, error) {
	return s.store.GetDailyBriefing(ctx, tenantID, assignedTo, date)
}

// Recording Prompt Services

// ListRecordingPrompts retrieves a paginated list of recording prompts
func (s *Service) ListRecordingPrompts(ctx context.Context, req RecordingPromptListRequest) ([]*RecordingPrompt, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	return s.store.ListRecordingPrompts(ctx, req)
}

// GetRecordingPrompt retrieves a recording prompt by code
func (s *Service) GetRecordingPrompt(ctx context.Context, code string) (*RecordingPrompt, error) {
	return s.store.GetRecordingPromptByCode(ctx, code)
}

// CreateRecordingPrompt creates a new recording prompt
func (s *Service) CreateRecordingPrompt(ctx context.Context, req CreateRecordingPromptRequest) (*RecordingPrompt, error) {
	// Validate request
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.PromptText == "" {
		return nil, fmt.Errorf("prompt_text is required")
	}
	if req.SystemPrompt == "" {
		return nil, fmt.Errorf("system_prompt is required")
	}

	return s.store.CreateRecordingPrompt(ctx, req)
}

// UpdateRecordingPrompt updates a recording prompt
func (s *Service) UpdateRecordingPrompt(ctx context.Context, code string, req UpdateRecordingPromptRequest) (*RecordingPrompt, error) {
	return s.store.UpdateRecordingPrompt(ctx, code, req)
}

// DeleteRecordingPrompt deletes a recording prompt
func (s *Service) DeleteRecordingPrompt(ctx context.Context, code string) error {
	return s.store.DeleteRecordingPrompt(ctx, code)
}

// Best Practice Services

// ListBestPractices retrieves a list of best practices
func (s *Service) ListBestPractices(ctx context.Context, tenantID int64) ([]*RecordingBestPractice, error) {
	return s.store.ListBestPractices(ctx, tenantID)
}

// AddBestPractice adds a recording to best practices
func (s *Service) AddBestPractice(ctx context.Context, tenantID, recordingID, createdBy int64, req AddBestPracticeRequest) (*RecordingBestPractice, error) {
	// Validate request
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	return s.store.AddBestPractice(ctx, tenantID, recordingID, createdBy, req)
}

// DeleteBestPractice removes a recording from best practices
func (s *Service) DeleteBestPractice(ctx context.Context, recordingID int64) error {
	return s.store.DeleteBestPractice(ctx, recordingID)
}

// Placeholder methods for remaining endpoints (to be implemented)

type workerJobRequest struct {
	RecordingID       int64  `json:"recording_id"`
	TenantID          int64  `json:"tenant_id"`
	JobType           string `json:"job_type"`
	Force             bool   `json:"force,omitempty"`
	TraceID           string `json:"trace_id,omitempty"`
	TriggerSource     string `json:"trigger_source,omitempty"`
	VendorRecordingID string `json:"vendor_recording_id,omitempty"`
}

type workerJobResponse struct {
	JobID string `json:"job_id"`
}

func (s *Service) enqueueLingceWorkerJob(ctx context.Context, recordingID int64, jobType, triggerSource string) (string, error) {
	if s.lingceWorkerURL == "" {
		return "", &recordingValidationError{code: "LINGCE_WORKER_NOT_CONFIGURED", message: "lingce-worker url is not configured"}
	}
	if strings.TrimSpace(triggerSource) == "" {
		triggerSource = "manual_replay"
	}
	url := fmt.Sprintf("%s/internal/jobs/enqueue?recording_id=%d&job_type=%s&trigger_source=%s", s.lingceWorkerURL, recordingID, jobType, triggerSource)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create lingce-worker request: %w", err)
	}
	if s.lingceWorkerToken != "" {
		httpReq.Header.Set("X-Internal-Token", s.lingceWorkerToken)
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
			return "", &workerUnavailableError{cause: err}
		}
		return "", &workerUnavailableError{cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		message := strings.TrimSpace(errResp.Error)
		if message == "" {
			message = fmt.Sprintf("lingce-worker returned status %d", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusBadRequest {
			return "", &recordingValidationError{code: "JOB_REJECTED", message: message}
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			return "", &workerUnavailableError{cause: fmt.Errorf("lingce-worker returned status %d", resp.StatusCode)}
		}
		return "", errors.New(message)
	}

	var result struct {
		Action string         `json:"action"`
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode lingce-worker response: %w", err)
	}
	if status, ok := result.Data["status"].(string); ok && strings.TrimSpace(status) != "" {
		return strings.TrimSpace(status), nil
	}
	return "queued", nil
}

func (s *Service) submitWorkerJob(ctx context.Context, req workerJobRequest) (string, error) {
	if s.workerURL == "" {
		return "", fmt.Errorf("recording worker url is not configured")
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal worker job request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.workerURL+"/v1/jobs", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create worker request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.workerToken != "" {
		httpReq.Header.Set("X-Internal-Token", s.workerToken)
		httpReq.Header.Set("Authorization", "Bearer "+s.workerToken)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
			return "", &workerUnavailableError{cause: err}
		}
		return "", &workerUnavailableError{cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			return "", &workerUnavailableError{cause: fmt.Errorf("recording worker returned status %d", resp.StatusCode)}
		}
		return "", fmt.Errorf("recording worker returned status %d", resp.StatusCode)
	}

	var result workerJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode worker response: %w", err)
	}

	return result.JobID, nil
}

func (s *Service) submitLingceWorkerReplay(ctx context.Context, recordingID int64) (string, error) {
	if s.lingceWorkerURL == "" {
		return "", &recordingValidationError{code: "LINGCE_WORKER_NOT_CONFIGURED", message: "lingce-worker url is not configured"}
	}
	url := fmt.Sprintf("%s/internal/analysis/replay?recording_id=%d&trigger_source=%s", s.lingceWorkerURL, recordingID, "manual_replay")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create lingce-worker request: %w", err)
	}
	if s.lingceWorkerToken != "" {
		httpReq.Header.Set("X-Internal-Token", s.lingceWorkerToken)
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
			return "", &workerUnavailableError{cause: err}
		}
		return "", &workerUnavailableError{cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		message := strings.TrimSpace(errResp.Error)
		if message == "" {
			message = fmt.Sprintf("lingce-worker returned status %d", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusBadRequest {
			return "", &recordingValidationError{code: "REPLAY_REJECTED", message: message}
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			return "", &workerUnavailableError{cause: fmt.Errorf("lingce-worker returned status %d", resp.StatusCode)}
		}
		return "", errors.New(message)
	}

	var result struct {
		Action string         `json:"action"`
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode lingce-worker response: %w", err)
	}
	if runID, ok := result.Data["run_id"].(string); ok && strings.TrimSpace(runID) != "" {
		return strings.TrimSpace(runID), nil
	}
	return "", fmt.Errorf("lingce-worker response missing run_id")
}

func (s *Service) ReanalyzeRecording(ctx context.Context, id int64) (string, error) {
	if s.lingceWorkerURL == "" {
		return s.TriggerAnalyze(ctx, id, TriggerAnalyzeRequest{Force: true})
	}
	if _, err := s.store.GetRecordingByID(ctx, id); err != nil {
		return "", err
	}
	return s.submitLingceWorkerReplay(ctx, id)
}

func (s *Service) triggerWorkerJob(ctx context.Context, id int64, jobType string, force bool) (string, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return "", err
	}

	if s.lingceWorkerURL != "" {
		return s.enqueueLingceWorkerJob(ctx, id, jobType, "manual_replay")
	}

	traceID := fmt.Sprintf("trace-manual-%d-%d", id, time.Now().UnixNano())
	vendorRecordingID := ""
	if recording.DeviceNo != "" {
		vendorRecordingID = recording.DeviceNo
	}
	jobID, err := s.submitWorkerJob(ctx, workerJobRequest{
		RecordingID:       id,
		TenantID:          recording.TenantID,
		JobType:           jobType,
		Force:             force,
		TraceID:           traceID,
		TriggerSource:     "manual_replay",
		VendorRecordingID: vendorRecordingID,
	})
	if err != nil {
		return "", err
	}

	return jobID, nil
}

// TriggerTranscribe triggers transcription for a recording
func (s *Service) TriggerTranscribe(ctx context.Context, id int64, req TriggerTranscribeRequest) (string, error) {
	return s.triggerWorkerJob(ctx, id, "transcribe", req.Force)
}

// TriggerAnalyze triggers analysis for a recording
func (s *Service) TriggerAnalyze(ctx context.Context, id int64, req TriggerAnalyzeRequest) (string, error) {
	return s.triggerWorkerJob(ctx, id, "analyze", req.Force)
}

// TriggerClean triggers cleaning for a recording
func (s *Service) TriggerClean(ctx context.Context, id int64, req TriggerCleanRequest) (string, error) {
	return s.triggerWorkerJob(ctx, id, "clean", req.Force)
}

// GetAnalysisResult retrieves analysis result for a recording
func (s *Service) GetAnalysisResult(ctx context.Context, id int64) (*RecordingAnalysisResult, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if recording.TranscriptText == nil &&
		recording.DoctorSummary == nil &&
		recording.TherapistSummary == nil &&
		recording.ConsultantSummary == nil {
		return nil, fmt.Errorf("analysis result not found")
	}

	return &RecordingAnalysisResult{
		ID:                recording.ID,
		RecordingID:       recording.ID,
		TenantID:          recording.TenantID,
		TranscriptText:    recording.TranscriptText,
		DoctorSummary:     recording.DoctorSummary,
		TherapistSummary:  recording.TherapistSummary,
		ConsultantSummary: recording.ConsultantSummary,
		CreatedAt:         recording.CreatedAt,
		UpdatedAt:         recording.UpdatedAt,
	}, nil
}

// GetQualityControlDashboard retrieves quality control dashboard
func (s *Service) GetQualityControlDashboard(ctx context.Context, tenantID int64) (*QualityControlDashboardResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}

	total := stats.TotalRecordings
	if total == 0 {
		return &QualityControlDashboardResponse{
			TopIssues: []IssueCount{},
		}, nil
	}

	qualified := stats.CompletedRecordings
	unqualified := total - qualified
	avgScore := float64(qualified) / float64(total) * 100

	topIssues := []IssueCount{}
	if stats.PendingRecordings > 0 {
		topIssues = append(topIssues, IssueCount{Issue: "待处理录音较多", Count: stats.PendingRecordings})
	}
	if stats.FailedRecordings > 0 {
		topIssues = append(topIssues, IssueCount{Issue: "处理失败录音", Count: stats.FailedRecordings})
	}
	if len(topIssues) == 0 {
		topIssues = append(topIssues, IssueCount{Issue: "当前无明显质量异常", Count: 0})
	}

	return &QualityControlDashboardResponse{
		TotalRecordings:  total,
		QualifiedCount:   qualified,
		UnqualifiedCount: unqualified,
		AvgScore:         avgScore,
		TopIssues:        topIssues,
	}, nil
}

// GetDoctorAbilityRanking retrieves doctor list style ranking for medical recordings.
func (s *Service) GetDoctorAbilityRanking(ctx context.Context, tenantID int64, period string) ([]DoctorAbilityRankingResponse, error) {
	windowDays := 30
	switch strings.TrimSpace(strings.ToLower(period)) {
	case "3m", "quarter":
		windowDays = 90
	case "6m", "half_year":
		windowDays = 180
	case "1m", "month", "":
		windowDays = 30
	}

	now := time.Now()
	currentStart := now.AddDate(0, 0, -(windowDays - 1))
	prevStart := currentStart.AddDate(0, 0, -windowDays)

	type doctorAbilityRow struct {
		EmployeeID int64
		Name       string
		TS         time.Time
		Analysis   map[string]interface{}
		Structured map[string]interface{}
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知医生') AS employee_name,
		       COALESCE(r.recorded_at, r.created_at) AS ts,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
		       COALESCE(ars.result_data, '{}'::json) AS structured_result
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN LATERAL (
			SELECT rar.result_data
			FROM recording_analysis_results rar
			WHERE rar.recording_id = r.id
			  AND rar.prompt_code = 'doctor_segue_structured'
			ORDER BY rar.created_at DESC
			LIMIT 1
		) ars ON true
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) <= $3
		  AND EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'doctor_assistant'])
		  )
	`, tenantID, prevStart, now)
	if err != nil {
		return nil, fmt.Errorf("failed to query doctor ability rows: %w", err)
	}
	defer rows.Close()

	type doctorAbilityAgg struct {
		EmployeeID          int64
		Name                string
		CurrentCount        int64
		CurrentSegueScores  []float64
		PreviousSegueScores []float64
		StageScores         map[string][]float64
		PatientStatusTotal  int64
		PatientAcceptCount  int64
		EmotionTotal        int64
		EmotionImproveCount int64
		CriticalGapCount    int64
	}
	aggByEmployee := map[int64]*doctorAbilityAgg{}

	for rows.Next() {
		var row doctorAbilityRow
		if scanErr := rows.Scan(&row.EmployeeID, &row.Name, &row.TS, &row.Analysis, &row.Structured); scanErr != nil {
			return nil, fmt.Errorf("failed to scan doctor ability row: %w", scanErr)
		}
		if row.EmployeeID <= 0 {
			continue
		}
		agg := aggByEmployee[row.EmployeeID]
		if agg == nil {
			agg = &doctorAbilityAgg{
				EmployeeID:  row.EmployeeID,
				Name:        row.Name,
				StageScores: map[string][]float64{"G1": {}, "G2": {}, "G3": {}, "G4": {}, "G5": {}, "G6": {}},
			}
			aggByEmployee[row.EmployeeID] = agg
		}

		mergedAnalysis := row.Analysis
		if mergedAnalysis == nil {
			mergedAnalysis = map[string]interface{}{}
		}
		if row.Structured != nil {
			if segue, ok := row.Structured["segue"]; ok {
				mergedAnalysis["segue"] = segue
			}
			if segueScores, ok := row.Structured["segue_scores"]; ok {
				mergedAnalysis["segue_scores"] = segueScores
			}
			if quality, ok := row.Structured["quality_score"]; ok {
				mergedAnalysis["quality_score"] = quality
			}
		}

		score := resolveWeeklyDisplayScore(mergedAnalysis)
		if row.TS.Before(currentStart) {
			if score > 0 {
				agg.PreviousSegueScores = append(agg.PreviousSegueScores, score)
			}
			continue
		}

		agg.CurrentCount++
		if score > 0 {
			agg.CurrentSegueScores = append(agg.CurrentSegueScores, score)
		}

		dims := extractDoctorDimensionScores(mergedAnalysis)
		for code, value := range dims {
			if value > 0 {
				agg.StageScores[code] = append(agg.StageScores[code], value)
			}
		}

		statusRaw := resolveVisitOutcomeStatus(mergedAnalysis)
		if normalized := normalizePatientStatus(statusRaw); normalized != "" {
			agg.PatientStatusTotal++
			if normalized == "顺利接受" {
				agg.PatientAcceptCount++
			}
		}

		if startEmotion, endEmotion, ok := readEmotionPair(mergedAnalysis); ok {
			agg.EmotionTotal++
			if isEmotionImproved(startEmotion, endEmotion) {
				agg.EmotionImproveCount++
			}
		}

		if critical := pickBool(mergedAnalysis, "critical_gap"); critical != nil && *critical {
			agg.CriticalGapCount++
		}
	}

	out := make([]DoctorAbilityRankingResponse, 0, len(aggByEmployee))
	for _, agg := range aggByEmployee {
		if agg.CurrentCount == 0 {
			continue
		}
		curAvg := avgFloat(agg.CurrentSegueScores)
		prevAvg := avgFloat(agg.PreviousSegueScores)
		strongest, weakest := findStrongestWeakestDimension(agg.StageScores)
		acceptRate := 0.0
		if agg.PatientStatusTotal > 0 {
			acceptRate = roundFloat((float64(agg.PatientAcceptCount)/float64(agg.PatientStatusTotal))*100, 1)
		}
		emotionRate := 0.0
		if agg.EmotionTotal > 0 {
			emotionRate = roundFloat((float64(agg.EmotionImproveCount)/float64(agg.EmotionTotal))*100, 1)
		}

		out = append(out, DoctorAbilityRankingResponse{
			EmployeeID:     agg.EmployeeID,
			EmployeeName:   agg.Name,
			RecordingCount: agg.CurrentCount,
			AvgScore:       roundFloat(curAvg, 1),
			StageScores: map[string]float64{
				"G1": roundFloat(avgFloat(agg.StageScores["G1"]), 1),
				"G2": roundFloat(avgFloat(agg.StageScores["G2"]), 1),
				"G3": roundFloat(avgFloat(agg.StageScores["G3"]), 1),
				"G4": roundFloat(avgFloat(agg.StageScores["G4"]), 1),
				"G5": roundFloat(avgFloat(agg.StageScores["G5"]), 1),
				"G6": roundFloat(avgFloat(agg.StageScores["G6"]), 1),
			},
			SegueAvg:       roundFloat(curAvg, 1),
			SegueTrend:     roundFloat(curAvg-prevAvg, 1),
			StrongestDim:   strongest,
			WeakestDim:     weakest,
			AcceptanceRate: acceptRate,
			EmotionRate:    emotionRate,
			CriticalGaps:   agg.CriticalGapCount,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].SegueAvg == out[j].SegueAvg {
			return out[i].RecordingCount > out[j].RecordingCount
		}
		return out[i].SegueAvg > out[j].SegueAvg
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

func avgFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func findStrongestWeakestDimension(stageScores map[string][]float64) (string, string) {
	type dimAvg struct {
		code string
		avg  float64
	}
	labels := map[string]string{
		"G1": "建立接诊环境",
		"G2": "引出信息",
		"G3": "给予信息",
		"G4": "理解患者视角",
		"G5": "结束接诊",
		"G6": "治疗/预防计划",
	}
	all := make([]dimAvg, 0, 6)
	for _, code := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		avg := avgFloat(stageScores[code])
		if avg <= 0 {
			continue
		}
		all = append(all, dimAvg{code: code, avg: avg})
	}
	if len(all) == 0 {
		return "", ""
	}
	sort.Slice(all, func(i, j int) bool { return all[i].avg > all[j].avg })
	strongest := all[0].code + " " + labels[all[0].code]
	weakest := all[len(all)-1].code + " " + labels[all[len(all)-1].code]
	return strongest, weakest
}

// GetDoctorAbilityDetail retrieves detailed doctor ability
func (s *Service) GetDoctorAbilityDetail(ctx context.Context, tenantID, employeeID int64) (*DoctorAbilityDetailResponse, error) {
	name, err := s.employeeStore.GetEmployeeNameByID(ctx, employeeID, tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := s.store.pool.Query(ctx, `
		SELECT r.employee_id,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts,
		       COALESCE(r.duration, 0) AS duration
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= NOW() - INTERVAL '30 days'
		  AND EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'doctor_assistant'])
		  )
		ORDER BY ts DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query doctor ability detail rows: %w", err)
	}
	defer rows.Close()

	type abilityRow struct {
		EmployeeID int64
		Analysis   map[string]interface{}
		TS         time.Time
		Duration   int64
	}
	allRows := make([]abilityRow, 0, 256)
	for rows.Next() {
		var (
			item        abilityRow
			analysisRaw interface{}
		)
		if scanErr := rows.Scan(&item.EmployeeID, &analysisRaw, &item.TS, &item.Duration); scanErr != nil {
			return nil, fmt.Errorf("failed to scan doctor ability detail row: %w", scanErr)
		}
		if decoded, ok := decodeNestedJSONValue(analysisRaw, 0).(map[string]interface{}); ok && decoded != nil {
			item.Analysis = decoded
		} else {
			item.Analysis = map[string]interface{}{}
		}
		allRows = append(allRows, item)
	}

	now := time.Now()
	curStart := now.AddDate(0, 0, -14)
	prevStart := now.AddDate(0, 0, -28)

	employeeRows := make([]medicalTrendRow, 0, 64)
	teamRows := make([]medicalTrendRow, 0, len(allRows))
	empCurrentRows := make([]medicalTrendRow, 0, 32)
	empPreviousRows := make([]medicalTrendRow, 0, 32)
	var employeeTotalDuration int64
	for _, row := range allRows {
		wrapped := medicalTrendRow{
			Analysis: row.Analysis,
			TS:       row.TS,
		}
		teamRows = append(teamRows, wrapped)
		if row.EmployeeID == employeeID {
			employeeRows = append(employeeRows, wrapped)
			employeeTotalDuration += row.Duration
			if !row.TS.Before(curStart) {
				empCurrentRows = append(empCurrentRows, wrapped)
			} else if !row.TS.Before(prevStart) && row.TS.Before(curStart) {
				empPreviousRows = append(empPreviousRows, wrapped)
			}
		}
	}

	employeeStage := collectDimensionAvg(employeeRows)
	teamStage := collectDimensionAvg(teamRows)
	prevOverall := calcOverall(avgStageMap(convertToTeamRows(empPreviousRows)))
	curOverall := calcOverall(avgStageMap(convertToTeamRows(empCurrentRows)))

	recentTrend := "stable"
	if curOverall-prevOverall >= 0.2 {
		recentTrend = "improving"
	} else if prevOverall-curOverall >= 0.2 {
		recentTrend = "declining"
	}

	type dimDiff struct {
		code string
		diff float64
	}
	labels := map[string]string{
		"G1": "建立接诊环境",
		"G2": "引出信息",
		"G3": "给予信息",
		"G4": "理解患者视角",
		"G5": "结束接诊",
		"G6": "治疗/预防计划",
	}
	diffs := make([]dimDiff, 0, 6)
	for _, code := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		diffs = append(diffs, dimDiff{
			code: code,
			diff: roundFloat(employeeStage[code]-teamStage[code], 2),
		})
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].diff > diffs[j].diff })

	strengths := []string{}
	weaknesses := []string{}
	const eps = 0.005
	for i := 0; i < len(diffs) && len(strengths) < 2; i++ {
		if diffs[i].diff > eps {
			strengths = append(strengths, fmt.Sprintf("%s%s 高于团队均值 %.2f 分", diffs[i].code, labels[diffs[i].code], diffs[i].diff))
		}
	}
	for i := len(diffs) - 1; i >= 0 && len(weaknesses) < 2; i-- {
		if diffs[i].diff < -eps {
			weaknesses = append(weaknesses, fmt.Sprintf("%s%s 低于团队均值 %.2f 分", diffs[i].code, labels[diffs[i].code], -diffs[i].diff))
		}
	}
	if len(strengths) == 0 {
		strengths = append(strengths, "近期样本中暂无明显优势维度")
	}
	if len(weaknesses) == 0 {
		weaknesses = append(weaknesses, "近期样本中暂无明显短板维度")
	}

	communication := roundFloat((employeeStage["G1"]+employeeStage["G2"])/2, 2)
	professionalism := roundFloat((employeeStage["G3"]+employeeStage["G6"])/2, 2)
	empathy := roundFloat(employeeStage["G4"], 2)
	efficiency := roundFloat(employeeStage["G5"], 2)
	if len(employeeRows) == 0 || !hasPositiveDimension(employeeStage) {
		communication, professionalism, empathy, efficiency = 0, 0, 0, 0
		recentTrend = "stable"
		strengths = []string{"暂无足够样本"}
		weaknesses = []string{"暂无足够样本"}
	}

	return &DoctorAbilityDetailResponse{
		EmployeeID:           employeeID,
		EmployeeName:         name,
		RecordingCount:       int64(len(employeeRows)),
		TotalDuration:        employeeTotalDuration,
		CommunicationScore:   communication,
		ProfessionalismScore: professionalism,
		EmpathyScore:         empathy,
		EfficiencyScore:      efficiency,
		RecentTrend:          recentTrend,
		Strengths:            strengths,
		Weaknesses:           weaknesses,
	}, nil
}

func convertToTeamRows(rows []medicalTrendRow) []teamAbilityRow {
	out := make([]teamAbilityRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, teamAbilityRow{
			Analysis: row.Analysis,
			TS:       row.TS,
		})
	}
	return out
}

// GetConsultantAbilityDetail retrieves detailed consultant ability using consultant scope rows.
func (s *Service) GetConsultantAbilityDetail(ctx context.Context, tenantID, employeeID int64) (*DoctorAbilityDetailResponse, error) {
	name, err := s.employeeStore.GetEmployeeNameByID(ctx, employeeID, tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := s.store.pool.Query(ctx, `
		SELECT r.employee_id,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(NULLIF(r.business_scope, ''), 'unknown') = 'consultant'
		  AND EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = 'consultant'
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor','doctor_assistant','frontdesk','reception','receptionist','therapist'])
		  )
		  AND COALESCE(r.recorded_at, r.created_at) >= NOW() - INTERVAL '30 days'
		ORDER BY ts DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query consultant ability rows: %w", err)
	}
	defer rows.Close()

	type abilityRow struct {
		EmployeeID int64
		Analysis   map[string]interface{}
		TS         time.Time
	}
	allRows := make([]abilityRow, 0, 256)
	for rows.Next() {
		var (
			item        abilityRow
			analysisRaw interface{}
		)
		if scanErr := rows.Scan(&item.EmployeeID, &analysisRaw, &item.TS); scanErr != nil {
			return nil, fmt.Errorf("failed to scan consultant ability row: %w", scanErr)
		}
		if decoded, ok := decodeNestedJSONValue(analysisRaw, 0).(map[string]interface{}); ok && decoded != nil {
			item.Analysis = decoded
		} else {
			item.Analysis = map[string]interface{}{}
		}
		allRows = append(allRows, item)
	}

	employeeRows := make([]teamAbilityRow, 0, 64)
	teamRows := make([]teamAbilityRow, 0, len(allRows))
	now := time.Now()
	curStart := now.AddDate(0, 0, -14)
	prevStart := now.AddDate(0, 0, -28)
	empCurrentRows := make([]teamAbilityRow, 0, 32)
	empPreviousRows := make([]teamAbilityRow, 0, 32)

	for _, row := range allRows {
		wrapped := teamAbilityRow{
			EmployeeID: row.EmployeeID,
			Analysis:   row.Analysis,
			TS:         row.TS,
		}
		teamRows = append(teamRows, wrapped)
		if row.EmployeeID == employeeID {
			employeeRows = append(employeeRows, wrapped)
			if !row.TS.Before(curStart) {
				empCurrentRows = append(empCurrentRows, wrapped)
			} else if !row.TS.Before(prevStart) && row.TS.Before(curStart) {
				empPreviousRows = append(empPreviousRows, wrapped)
			}
		}
	}

	employeeStage := avgStageMap(employeeRows)
	teamStage := avgStageMap(teamRows)
	prevOverall := calcOverall(avgStageMap(empPreviousRows))
	curOverall := calcOverall(avgStageMap(empCurrentRows))

	recentTrend := "stable"
	if curOverall-prevOverall >= 0.2 {
		recentTrend = "improving"
	} else if prevOverall-curOverall >= 0.2 {
		recentTrend = "declining"
	}

	type stageDiff struct {
		name string
		diff float64
	}
	diffs := make([]stageDiff, 0, len(stageOrder))
	for _, stage := range stageOrder {
		diffs = append(diffs, stageDiff{
			name: stage,
			diff: roundFloat(employeeStage[stage]-teamStage[stage], 2),
		})
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].diff > diffs[j].diff })
	strengths := []string{}
	weaknesses := []string{}
	for i := 0; i < len(diffs) && len(strengths) < 2; i++ {
		if diffs[i].diff >= 0 {
			strengths = append(strengths, fmt.Sprintf("%s 高于团队均值 %.2f 分", diffs[i].name, diffs[i].diff))
		}
	}
	for i := len(diffs) - 1; i >= 0 && len(weaknesses) < 2; i-- {
		if diffs[i].diff <= 0 {
			weaknesses = append(weaknesses, fmt.Sprintf("%s 低于团队均值 %.2f 分", diffs[i].name, -diffs[i].diff))
		}
	}
	if len(strengths) == 0 {
		strengths = append(strengths, "近期咨询录音评分稳定")
	}
	if len(weaknesses) == 0 {
		weaknesses = append(weaknesses, "暂无明显短板，建议维持当前节奏")
	}

	communication := roundFloat((employeeStage["开场建立权威"]+employeeStage["需求探索"]+employeeStage["问题放大"])/3, 2)
	professionalism := roundFloat((employeeStage["专业呈现"]+employeeStage["方案定制"])/2, 2)
	empathy := roundFloat(employeeStage["异议化解"], 2)
	efficiency := roundFloat(employeeStage["促成与收尾"], 2)
	if len(employeeRows) == 0 {
		communication = 0
		professionalism = 0
		empathy = 0
		efficiency = 0
		recentTrend = "stable"
		strengths = []string{"暂无足够样本"}
		weaknesses = []string{"暂无足够样本"}
	}

	return &DoctorAbilityDetailResponse{
		EmployeeID:           employeeID,
		EmployeeName:         name,
		CommunicationScore:   communication,
		ProfessionalismScore: professionalism,
		EmpathyScore:         empathy,
		EfficiencyScore:      efficiency,
		RecentTrend:          recentTrend,
		Strengths:            strengths,
		Weaknesses:           weaknesses,
	}, nil
}

// GetCommunicationAnalysis retrieves communication analysis
func (s *Service) GetCommunicationAnalysis(ctx context.Context, tenantID int64) (*CommunicationAnalysisResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	top, err := s.GetDoctorAbilityRanking(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	if len(top) > 5 {
		top = top[:5]
	}
	avg := 0.0
	if stats.TotalRecordings > 0 {
		avg = float64(stats.CompletedRecordings) / float64(stats.TotalRecordings) * 100
	}
	common := []IssueCount{}
	if stats.PendingRecordings > 0 {
		common = append(common, IssueCount{Issue: "转写待完成", Count: stats.PendingRecordings})
	}
	if stats.FailedRecordings > 0 {
		common = append(common, IssueCount{Issue: "处理失败需复核", Count: stats.FailedRecordings})
	}
	return &CommunicationAnalysisResponse{
		TotalRecordings:       stats.TotalRecordings,
		AvgCommunicationScore: avg,
		TopCommunicators:      top,
		CommonIssues:          common,
	}, nil
}

type morningMeetingCandidate struct {
	RecordingID      int64
	EmployeeID       int64
	EmployeeName     string
	SceneName        string
	RecordedAt       time.Time
	DurationSeconds  int64
	TranscriptText   string
	QualityScore     float64
	Analysis         map[string]interface{}
	DimensionScores  map[string]float64
	OverallScore     float64
	OverallScoreText string
	PrimaryDimension string
	PrimaryScore     float64
	PrimaryScoreText string
	Evidence         []string
	Narrative        string
	Positive         bool
	SelectionReason  string
	DropDimension    string
	DropAmount       float64
	DropBaseline     float64
	InvalidReason    string
	AudioStartSecond *int
	AudioEndSecond   *int
}

type morningMeetingMaterialRow struct {
	RecordingID      int64
	EmployeeID       int64
	EmployeeName     string
	SceneName        string
	SelectionReason  string
	OverallScore     float64
	OverallScoreText string
	PrimaryDimension string
	PrimaryScore     float64
	PrimaryScoreText string
	Evidence         []string
	Diagnosis        string
	CoachingScript   string
	GenerationMethod string
	GenerationStatus string
	FallbackReason   string
	PromptCode       string
	ModelCode        string
	LLMRequestID     string
	UsedAt           *time.Time
}

func (s *Service) GetMorningMeetingMaterial(ctx context.Context, tenantID int64, roleCode string, meetingDate string) (*MorningMeetingMaterialResponse, error) {
	role := normalizeMorningMeetingRole(roleCode)
	if role == "" {
		role = "consultant"
	}
	meetDate, err := parseMorningMeetingDate(meetingDate)
	if err != nil {
		return nil, err
	}
	reviewDate := meetDate.AddDate(0, 0, -1)
	candidates, err := s.listMorningMeetingCandidates(ctx, tenantID, role, reviewDate)
	if err != nil {
		return nil, err
	}

	resp := &MorningMeetingMaterialResponse{
		MeetingDate:       meetDate.Format("2006-01-02"),
		ReviewDate:        reviewDate.Format("2006-01-02"),
		RoleCode:          role,
		SourceRecordCount: int64(len(candidates)),
		Highlights:        []string{},
		BestPractices:     []BestPracticeItem{},
		ImprovementAreas:  []string{},
		AttentionItems:    []string{},
		PraiseItems:       []MorningMeetingPraiseItem{},
	}
	if len(candidates) == 0 {
		resp.EmptyStateMessage = "所选日期前一天暂无录音，今日早会暂无可自动生成素材。"
		return resp, nil
	}

	resp.AttentionItems = s.buildMorningMeetingAttentionItems(ctx, tenantID, role, reviewDate, candidates)
	usableCandidates := filterUsableMorningMeetingCandidates(candidates)
	resp.UsableRecordCount = int64(len(usableCandidates))
	if len(usableCandidates) == 0 {
		fallbackDate, fallbackCandidates, fallbackErr := s.findRecentUsableMorningMeetingCandidates(ctx, tenantID, role, reviewDate, 7)
		if fallbackErr == nil && len(fallbackCandidates) > 0 {
			usableCandidates = fallbackCandidates
			resp.AttentionItems = append([]string{fmt.Sprintf("昨日无可直接讲评录音，已回退到 %s 最近一条可复盘录音。", fallbackDate.Format("2006-01-02"))}, resp.AttentionItems...)
			reviewDate = fallbackDate
		} else {
			resp.EmptyStateMessage = "昨日有录音，但转写或分析证据不足，暂不建议直接用于讲评。请先补齐录音质量与分析结果。"
			resp.AttentionItems = append([]string{"昨日有录音，但可用于讲评的有效对话不足，请优先复核录音完整性。"}, resp.AttentionItems...)
			return resp, nil
		}
	}
	resp.PraiseItems = s.buildMorningMeetingPraiseItems(role, usableCandidates)

	selected := selectMorningMeetingCandidate(role, usableCandidates)
	material, err := s.ensureMorningMeetingMaterial(ctx, tenantID, role, meetDate, reviewDate, selected)
	if err != nil {
		return nil, err
	}
	resp.TodayReview = &MorningMeetingReviewItem{
		RecordingID:       material.RecordingID,
		EmployeeID:        material.EmployeeID,
		EmployeeName:      material.EmployeeName,
		RoleCode:          role,
		SceneName:         material.SceneName,
		ReviewDate:        reviewDate.Format("2006-01-02"),
		MeetingDate:       meetDate.Format("2006-01-02"),
		SelectionReason:   material.SelectionReason,
		OverallScore:      material.OverallScore,
		OverallScoreText:  material.OverallScoreText,
		PrimaryDimension:  material.PrimaryDimension,
		PrimaryScore:      material.PrimaryScore,
		PrimaryScoreText:  material.PrimaryScoreText,
		Evidence:          material.Evidence,
		Diagnosis:         material.Diagnosis,
		CoachingScript:    material.CoachingScript,
		GenerationMethod:  material.GenerationMethod,
		GenerationStatus:  material.GenerationStatus,
		FallbackReason:    material.FallbackReason,
		PromptCode:        material.PromptCode,
		ModelCode:         material.ModelCode,
		LLMRequestID:      material.LLMRequestID,
		SourceRecordCount: int64(len(usableCandidates)),
		AudioStartSeconds: selected.AudioStartSecond,
		AudioEndSeconds:   selected.AudioEndSecond,
	}
	if material.UsedAt != nil {
		resp.TodayReview.UsedAt = material.UsedAt.Format(time.RFC3339)
	}
	return resp, nil
}

func (s *Service) findRecentUsableMorningMeetingCandidates(ctx context.Context, tenantID int64, roleCode string, reviewDate time.Time, maxDays int) (time.Time, []morningMeetingCandidate, error) {
	if maxDays <= 0 {
		maxDays = 7
	}
	for i := 1; i <= maxDays; i++ {
		targetDate := reviewDate.AddDate(0, 0, -i)
		candidates, err := s.listMorningMeetingCandidates(ctx, tenantID, roleCode, targetDate)
		if err != nil {
			return time.Time{}, nil, err
		}
		usable := filterUsableMorningMeetingCandidates(candidates)
		if len(usable) > 0 {
			return targetDate, usable, nil
		}
	}
	return time.Time{}, nil, nil
}

func (s *Service) MarkMorningMeetingUsed(ctx context.Context, tenantID int64, usedBy int64, req MarkMorningMeetingUsedRequest) (*MorningMeetingMaterialResponse, error) {
	role := normalizeMorningMeetingRole(req.RoleCode)
	if role == "" {
		return nil, fmt.Errorf("role_code is required")
	}
	meetingDate, err := parseMorningMeetingDate(req.MeetingDate)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.pool.Exec(ctx, `
		UPDATE morning_meeting_materials
		SET used_at = NOW(), used_by = $4, updated_at = NOW()
		WHERE tenant_id = $1 AND role_code = $2 AND meeting_date = $3
	`, tenantID, role, meetingDate.Format("2006-01-02"), usedBy); err != nil {
		return nil, fmt.Errorf("failed to mark morning meeting used: %w", err)
	}
	return s.GetMorningMeetingMaterial(ctx, tenantID, role, meetingDate.Format("2006-01-02"))
}

func (s *Service) buildMorningMeetingAttentionItems(ctx context.Context, tenantID int64, roleCode string, reviewDate time.Time, candidates []morningMeetingCandidate) []string {
	lines := make([]string, 0, 2)
	if line := s.buildMorningMeetingLowAverageLine(ctx, tenantID, roleCode, reviewDate, candidates); line != "" {
		lines = append(lines, line)
	}
	if line := s.buildMorningMeetingOverdueTaskLine(ctx, tenantID, roleCode); line != "" {
		lines = append(lines, line)
	}
	return lines
}

func (s *Service) buildMorningMeetingLowAverageLine(ctx context.Context, tenantID int64, roleCode string, reviewDate time.Time, candidates []morningMeetingCandidate) string {
	type agg struct {
		name  string
		count int
		sum   float64
	}
	byEmployee := map[int64]*agg{}
	for _, item := range candidates {
		entry := byEmployee[item.EmployeeID]
		if entry == nil {
			entry = &agg{name: item.EmployeeName}
			byEmployee[item.EmployeeID] = entry
		}
		entry.count++
		entry.sum += item.OverallScore
	}
	var (
		bestLine string
		bestGap  float64
	)
	for employeeID, entry := range byEmployee {
		if entry.count <= 0 {
			continue
		}
		yesterdayAvg := roundFloat(entry.sum/float64(entry.count), 1)
		baseline, ok := s.loadMorningMeetingEmployeeBaseline(ctx, tenantID, employeeID, reviewDate, roleCode)
		if !ok || baseline <= 0 {
			continue
		}
		gap := baseline - yesterdayAvg
		if gap <= 0 {
			continue
		}
		if gap > bestGap {
			bestGap = gap
			bestLine = fmt.Sprintf("%s昨日%d条录音均分 %.1f，低于个人均值（%.1f）", entry.name, entry.count, yesterdayAvg, baseline)
		}
	}
	return bestLine
}

func (s *Service) loadMorningMeetingEmployeeBaseline(ctx context.Context, tenantID int64, employeeID int64, reviewDate time.Time, roleCode string) (float64, bool) {
	start := reviewDate.AddDate(0, 0, -14).Format("2006-01-02")
	end := reviewDate.Format("2006-01-02")
	rows, err := s.store.pool.Query(ctx, `
		SELECT COALESCE(r.quality_score, 0)::float8, COALESCE(r.analysis_result, '{}'::json)
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND r.employee_id = $2
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $3
		  AND COALESCE(r.recorded_at, r.created_at) < $4
		ORDER BY COALESCE(r.recorded_at, r.created_at) DESC
		LIMIT 20
	`, tenantID, employeeID, start, end)
	if err != nil {
		return 0, false
	}
	defer rows.Close()

	var (
		sum   float64
		count float64
	)
	for rows.Next() {
		var qualityScore float64
		var analysis map[string]interface{}
		if scanErr := rows.Scan(&qualityScore, &analysis); scanErr != nil {
			return 0, false
		}
		var scores map[string]float64
		switch normalizeMorningMeetingRole(roleCode) {
		case "doctor":
			scores = computeDoctorMorningScores(analysis)
		case "therapist":
			scores = computeTherapistMorningScores(analysis)
		default:
			scores = computeConsultantMorningScores(analysis, qualityScore)
		}
		overall, _, _ := pickLowestMorningScore(scores)
		if overall <= 0 {
			continue
		}
		sum += overall
		count += 1
	}
	if count <= 0 {
		return 0, false
	}
	return roundFloat(sum/count, 1), true
}

func (s *Service) buildMorningMeetingOverdueTaskLine(ctx context.Context, tenantID int64, roleCode string) string {
	type taskAgg struct {
		name  string
		count int
	}
	rows, err := s.store.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), NULLIF(e.username, ''), '未分配') AS assignee_name,
			COUNT(*)::bigint
		FROM recording_tasks t
		JOIN recordings r ON r.id = t.recording_id
		LEFT JOIN employees e ON e.id = t.assigned_to
		WHERE t.tenant_id = $1
		  AND t.status IN ('pending', 'assigned')
		  AND t.due_at < NOW()
		  %s
		GROUP BY assignee_name
		ORDER BY COUNT(*) DESC, assignee_name ASC
		LIMIT 5
	`, morningMeetingRoleFilterSQL(roleCode)), tenantID)
	if err != nil {
		return ""
	}
	defer rows.Close()

	parts := make([]string, 0, 5)
	total := 0
	for rows.Next() {
		var name string
		var count int
		if scanErr := rows.Scan(&name, &count); scanErr != nil {
			return ""
		}
		total += count
		parts = append(parts, fmt.Sprintf("%s%d条", name, count))
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("跟进任务超时 %d 条（%s）", total, strings.Join(parts, "、"))
}

func (s *Service) buildMorningMeetingPraiseItems(roleCode string, candidates []morningMeetingCandidate) []MorningMeetingPraiseItem {
	if len(candidates) == 0 {
		return nil
	}
	signal := pickMorningMeetingPraiseSignal(roleCode, candidates)
	if signal == nil {
		return nil
	}
	return []MorningMeetingPraiseItem{{
		Text:  signal.Text,
		Quote: signal.Quote,
	}}
}

type morningMeetingPraiseSignal struct {
	Text     string
	Quote    string
	Score    float64
	Priority int
}

func pickMorningMeetingPraiseSignal(roleCode string, candidates []morningMeetingCandidate) *morningMeetingPraiseSignal {
	var best *morningMeetingPraiseSignal
	for i := range candidates {
		item := candidates[i]
		signal := buildMorningMeetingPraiseSignal(roleCode, item)
		if signal == nil {
			continue
		}
		if best == nil ||
			signal.Priority > best.Priority ||
			(signal.Priority == best.Priority && signal.Score > best.Score) {
			best = signal
		}
	}
	return best
}

func isMorningMeetingPraiseUsable(item morningMeetingCandidate) bool {
	if strings.TrimSpace(item.InvalidReason) != "" {
		return false
	}
	if item.PrimaryScore <= 0 || strings.TrimSpace(item.PrimaryDimension) == "" {
		return false
	}
	if len(item.Evidence) == 0 {
		return false
	}
	if looksNegativeText(item.Evidence[0]) {
		return false
	}
	return true
}

func buildMorningMeetingPraiseSignal(roleCode string, item morningMeetingCandidate) *morningMeetingPraiseSignal {
	if !isMorningMeetingPraiseUsable(item) {
		return nil
	}
	dimension, score := pickHighestMorningScore(item.DimensionScores)
	if strings.TrimSpace(dimension) == "" || score <= 0 {
		dimension = item.PrimaryDimension
		score = item.PrimaryScore
	}
	if strings.TrimSpace(dimension) == "" || score <= 0 {
		return nil
	}
	evidence := extractDimensionEvidence(item.Analysis, roleCode, dimension)
	if len(evidence) == 0 {
		evidence = extractMorningFallbackEvidence(roleCode, item.Analysis, item.Narrative)
	}
	quote := ""
	if len(evidence) > 0 {
		quote = truncateText(strings.TrimSpace(evidence[0]), 60)
	}
	if quote == "" {
		return nil
	}
	scoreText := morningMeetingDimensionScoreText(roleCode, score)
	if normalizeMorningMeetingRole(roleCode) == "consultant" && roundFloat(score, 1) >= 5 {
		scoreText = "5分满分"
	}
	priority := morningMeetingPraisePriority(roleCode, score)
	if priority <= 0 {
		return nil
	}
	text := fmt.Sprintf("%s 昨日%s有一处做得比较扎实（%s）", item.EmployeeName, dimension, scoreText)
	if priority >= 3 {
		text = fmt.Sprintf("%s 昨日%s有一处做得很好（%s）", item.EmployeeName, dimension, scoreText)
	}
	return &morningMeetingPraiseSignal{
		Text:     text,
		Quote:    buildMorningMeetingPraiseQuote(roleCode, dimension, quote),
		Score:    score,
		Priority: priority,
	}
}

func pickHighestMorningScore(scores map[string]float64) (string, float64) {
	if len(scores) == 0 {
		return "", 0
	}
	bestDim := ""
	bestScore := -1.0
	for dim, score := range scores {
		if score > bestScore {
			bestDim = dim
			bestScore = score
		}
	}
	return bestDim, roundFloat(bestScore, 1)
}

func morningMeetingPraisePriority(roleCode string, score float64) int {
	if normalizeMorningMeetingRole(roleCode) == "consultant" {
		switch {
		case roundFloat(score, 1) >= 5:
			return 4
		case roundFloat(score, 1) >= 4:
			return 3
		case roundFloat(score, 1) >= 3:
			return 2
		default:
			return 0
		}
	}
	switch {
	case roundFloat(score, 1) >= 90:
		return 4
	case roundFloat(score, 1) >= 80:
		return 3
	case roundFloat(score, 1) >= 70:
		return 2
	default:
		return 0
	}
}

func buildMorningMeetingPraiseQuote(roleCode, dimension, quote string) string {
	role := normalizeMorningMeetingRole(roleCode)
	switch role {
	case "consultant":
		if strings.Contains(dimension, "需求探索") {
			return fmt.Sprintf("先把客户场景和真实顾虑问出来，再往下讲方案。原话：%s", quote)
		}
		if strings.Contains(dimension, "异议化解") {
			return fmt.Sprintf("先接住顾虑，再回应担心，不跟客户硬顶。原话：%s", quote)
		}
		if strings.Contains(dimension, "促成") {
			return fmt.Sprintf("把下一步动作说具体，客户更容易往前走。原话：%s", quote)
		}
		return fmt.Sprintf("这句表达能看出动作是落地的，不是空讲。原话：%s", quote)
	case "doctor":
		if strings.Contains(dimension, "引出信息") {
			return fmt.Sprintf("先把患者情况问清，再进入解释，接诊会稳很多。原话：%s", quote)
		}
		if strings.Contains(dimension, "理解患者视角") {
			return fmt.Sprintf("先接住患者担心，再给医学解释，更容易建立信任。原话：%s", quote)
		}
		return fmt.Sprintf("这句体现了可复用的接诊动作。原话：%s", quote)
	default:
		if strings.Contains(dimension, "互动评估") {
			return fmt.Sprintf("做完动作立刻确认体感变化，后续判断才有依据。原话：%s", quote)
		}
		if strings.Contains(dimension, "专业操作") {
			return fmt.Sprintf("边做边解释关键点，患者会更容易理解和配合。原话：%s", quote)
		}
		if strings.Contains(dimension, "方案闭环") {
			return fmt.Sprintf("把下一步说清楚，患者更知道回去该怎么做。原话：%s", quote)
		}
		return fmt.Sprintf("这句能直接拿来做团队动作示范。原话：%s", quote)
	}
}

// GetWeeklyMeetingMaterial retrieves weekly meeting material
func (s *Service) GetWeeklyMeetingMaterial(ctx context.Context, tenantID int64, roleCode string, weekOffset int) (*WeeklyMeetingMaterialResponse, error) {
	role := normalizeMorningMeetingRole(roleCode)
	if role == "" {
		role = "consultant"
	}
	weekStart, weekEnd := weeklyMeetingWindow(time.Now(), weekOffset)
	prevWeekStart := weekStart.AddDate(0, 0, -7)
	prevWeekEnd := weekEnd.AddDate(0, 0, -7)

	currentCandidates, err := s.listWeeklyMeetingCandidates(ctx, tenantID, role, weekStart, weekEnd)
	if err != nil {
		return nil, err
	}
	previousCandidates, err := s.listWeeklyMeetingCandidates(ctx, tenantID, role, prevWeekStart, prevWeekEnd)
	if err != nil {
		return nil, err
	}

	usableCurrent := filterUsableMorningMeetingCandidates(currentCandidates)
	usablePrevious := filterUsableMorningMeetingCandidates(previousCandidates)
	weeklyStats := s.buildWeeklyMeetingStats(ctx, tenantID, role, weekStart, weekEnd, prevWeekStart, prevWeekEnd, currentCandidates, previousCandidates)
	benchmarkStudy := s.selectWeeklyBenchmarkStudy(ctx, tenantID, role, weeklyStats)
	problemReview := s.buildWeeklyProblemReview(ctx, tenantID, role, usableCurrent)
	actionTracking, actionTrackingEmptyReason := s.buildWeeklyActionTracking(ctx, tenantID, role, prevWeekStart, prevWeekEnd, weekEnd, weeklyStats)
	nextFocus := s.buildWeeklyNextFocus(role, weeklyStats, benchmarkStudy, problemReview, actionTracking)

	highlights := []string{
		fmt.Sprintf("本周录音 %d 条（上周 %d）", len(currentCandidates), len(previousCandidates)),
		fmt.Sprintf("本周团队均分 %s（上周 %s）", formatWeeklyScoreText(role, averageWeeklyOverallScore(usableCurrent)), formatWeeklyScoreText(role, averageWeeklyOverallScore(usablePrevious))),
	}
	if weeklyStats != nil && normalizeMorningMeetingRole(role) == "consultant" {
		highlights = append(highlights, fmt.Sprintf("本周成交 %d 单（上周 %d）", weeklyStats.DealCount, weeklyStats.PreviousDealCount))
	}

	bestPractices := make([]BestPracticeItem, 0, 1)
	if benchmarkStudy != nil {
		bestPractices = append(bestPractices, BestPracticeItem{
			RecordingID:  benchmarkStudy.RecordingID,
			Title:        fmt.Sprintf("%s · %s", benchmarkStudy.Dimension, benchmarkStudy.EmployeeName),
			Description:  benchmarkStudy.InsightSummary,
			EmployeeName: benchmarkStudy.EmployeeName,
		})
	}
	improvementAreas := []string{}
	if problemReview != nil {
		improvementAreas = append(improvementAreas, fmt.Sprintf("%s：%s", problemReview.EmployeeName, problemReview.PrimaryDimension))
	}
	if weeklyStats != nil && strings.TrimSpace(weeklyStats.WeakestDimension) != "" {
		improvementAreas = append(improvementAreas, fmt.Sprintf("团队共性短板：%s", weeklyStats.WeakestDimension))
	}

	return &WeeklyMeetingMaterialResponse{
		WeekStart:                 weekStart.Format("2006-01-02"),
		WeekEnd:                   weekEnd.Format("2006-01-02"),
		RoleCode:                  role,
		WeekLabel:                 formatWeeklyMeetingLabel(weekStart, weekEnd),
		Highlights:                highlights,
		BestPractices:             bestPractices,
		ImprovementAreas:          improvementAreas,
		WeeklyStats:               weeklyStats,
		BenchmarkStudy:            benchmarkStudy,
		ProblemReview:             problemReview,
		ActionTracking:            actionTracking,
		ActionTrackingEmptyReason: actionTrackingEmptyReason,
		NextFocus:                 nextFocus,
	}, nil
}

func weeklyMeetingWindow(now time.Time, weekOffset int) (time.Time, time.Time) {
	local := now.In(time.Local)
	base := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	weekday := int(base.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := base.AddDate(0, 0, -(weekday-1)-7*weekOffset)
	sunday := monday.AddDate(0, 0, 6)
	return monday, sunday
}

func formatWeeklyMeetingLabel(weekStart, weekEnd time.Time) string {
	year, week := weekStart.ISOWeek()
	_ = year
	return fmt.Sprintf("第%d周(%d/%d-%d/%d)", week, weekStart.Month(), weekStart.Day(), weekEnd.Month(), weekEnd.Day())
}

func (s *Service) listWeeklyMeetingCandidates(ctx context.Context, tenantID int64, roleCode string, weekStart, weekEnd time.Time) ([]morningMeetingCandidate, error) {
	start := weekStart.Format("2006-01-02")
	end := weekEnd.Add(24 * time.Hour).Format("2006-01-02")
	filterClause := morningMeetingRoleFilterSQL(roleCode)
	rows, err := s.store.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			r.id,
			r.employee_id,
			COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知员工') AS employee_name,
			COALESCE(NULLIF(r.scene_name, ''), NULLIF(r.scene, ''), '') AS scene_name,
			COALESCE(r.recorded_at, r.created_at) AS ts,
			COALESCE(r.duration, 0)::bigint,
			COALESCE(
				NULLIF(
					CASE
						WHEN r.cleaned_transcription IS NULL THEN ''
						WHEN jsonb_typeof(r.cleaned_transcription) = 'string' THEN trim(both '"' from r.cleaned_transcription::text)
						WHEN jsonb_typeof(r.cleaned_transcription) = 'object' THEN COALESCE(r.cleaned_transcription->>'full_text', r.cleaned_transcription->>'text', '')
						ELSE ''
					END,
					''
				),
				COALESCE(r.transcription_text, '')
			) AS transcript_text,
			COALESCE(r.quality_score, 0)::float8,
			COALESCE(r.analysis_result, '{}'::json)
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		  %s
		ORDER BY ts DESC, r.id DESC
	`, filterClause), tenantID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query weekly meeting candidates: %w", err)
	}
	defer rows.Close()

	items := make([]morningMeetingCandidate, 0, 128)
	for rows.Next() {
		var item morningMeetingCandidate
		if scanErr := rows.Scan(&item.RecordingID, &item.EmployeeID, &item.EmployeeName, &item.SceneName, &item.RecordedAt, &item.DurationSeconds, &item.TranscriptText, &item.QualityScore, &item.Analysis); scanErr != nil {
			return nil, fmt.Errorf("failed to scan weekly meeting candidate: %w", scanErr)
		}
		s.enrichMorningMeetingCandidate(&item, roleCode)
		item.InvalidReason = detectMorningMeetingInvalidReason(item)
		items = append(items, item)
	}
	return items, nil
}

func averageWeeklyOverallScore(items []morningMeetingCandidate) float64 {
	if len(items) == 0 {
		return 0
	}
	total := 0.0
	for _, item := range items {
		total += item.OverallScore
	}
	return roundFloat(total/float64(len(items)), 1)
}

func formatWeeklyScoreText(role string, score float64) string {
	if normalizeMorningMeetingRole(role) == "consultant" {
		return fmt.Sprintf("%.1f", roundFloat(score, 1))
	}
	return fmt.Sprintf("%.1f%%", roundFloat(score, 1))
}

func (s *Service) buildWeeklyMeetingStats(ctx context.Context, tenantID int64, roleCode string, weekStart, weekEnd, prevWeekStart, prevWeekEnd time.Time, currentCandidates, previousCandidates []morningMeetingCandidate) *WeeklyMeetingStats {
	currentUsable := filterUsableMorningMeetingCandidates(currentCandidates)
	previousUsable := filterUsableMorningMeetingCandidates(previousCandidates)
	currentAvg := averageWeeklyOverallScore(currentUsable)
	previousAvg := averageWeeklyOverallScore(previousUsable)
	currentDeals := s.countWeeklyDeals(ctx, tenantID, roleCode, weekStart, weekEnd)
	previousDeals := s.countWeeklyDeals(ctx, tenantID, roleCode, prevWeekStart, prevWeekEnd)
	changes, weakest := buildWeeklyDimensionChanges(roleCode, currentUsable, previousUsable)
	return &WeeklyMeetingStats{
		TotalRecordings:    int64(len(currentCandidates)),
		PreviousRecordings: int64(len(previousCandidates)),
		AvgScore:           currentAvg,
		PreviousAvgScore:   previousAvg,
		DealCount:          currentDeals,
		PreviousDealCount:  previousDeals,
		WeakestDimension:   weakest,
		DimensionChanges:   changes,
	}
}

func (s *Service) countWeeklyDeals(ctx context.Context, tenantID int64, roleCode string, start, end time.Time) int64 {
	if normalizeMorningMeetingRole(roleCode) != "consultant" {
		return 0
	}
	filterClause := morningMeetingRoleFilterSQL(roleCode)
	var count int64
	_ = s.store.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		  AND r.confirmed_deal_status = '成交了'
		  %s
	`, filterClause), tenantID, start.Format("2006-01-02"), end.Add(24*time.Hour).Format("2006-01-02")).Scan(&count)
	return count
}

func buildWeeklyDimensionChanges(roleCode string, current, previous []morningMeetingCandidate) ([]WeeklyMeetingDimensionChangeItem, string) {
	currentAvg := aggregateWeeklyDimensionAverages(current)
	previousAvg := aggregateWeeklyDimensionAverages(previous)
	if len(currentAvg) == 0 {
		return []WeeklyMeetingDimensionChangeItem{}, ""
	}
	keys := make([]string, 0, len(currentAvg))
	for key := range currentAvg {
		keys = append(keys, key)
		if _, ok := previousAvg[key]; !ok {
			previousAvg[key] = 0
		}
	}
	sort.Strings(keys)
	items := make([]WeeklyMeetingDimensionChangeItem, 0, len(keys))
	weakest := ""
	weakestScore := math.MaxFloat64
	for _, key := range keys {
		cur := roundFloat(currentAvg[key], 1)
		prev := roundFloat(previousAvg[key], 1)
		delta := roundFloat(cur-prev, 1)
		trend := "flat"
		conclusion := "持平"
		if delta > 0 {
			trend = "up"
			conclusion = "改善"
		} else if delta < 0 {
			trend = "down"
			conclusion = "需关注"
		}
		items = append(items, WeeklyMeetingDimensionChangeItem{
			Dimension:  key,
			Current:    cur,
			Previous:   prev,
			Delta:      delta,
			Trend:      trend,
			Conclusion: conclusion,
		})
		if cur < weakestScore {
			weakestScore = cur
			weakest = key
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Delta == items[j].Delta {
			return items[i].Current < items[j].Current
		}
		return items[i].Delta > items[j].Delta
	})
	if len(items) > 4 {
		items = items[:4]
	}
	return items, weakest
}

func aggregateWeeklyDimensionAverages(items []morningMeetingCandidate) map[string]float64 {
	total := map[string]float64{}
	count := map[string]int{}
	for _, item := range items {
		for key, value := range item.DimensionScores {
			if strings.TrimSpace(key) == "" {
				continue
			}
			total[key] += value
			count[key]++
		}
	}
	out := make(map[string]float64, len(total))
	for key, value := range total {
		if count[key] == 0 {
			continue
		}
		out[key] = value / float64(count[key])
	}
	return out
}

func (s *Service) selectWeeklyBenchmarkStudy(ctx context.Context, tenantID int64, roleCode string, stats *WeeklyMeetingStats) *WeeklyMeetingBenchmarkStudy {
	items, _, err := s.store.ListBenchmarkClips(ctx, tenantID, "accepted", "", roleCode, "", "", 1, 100)
	if err != nil || len(items) == 0 {
		return nil
	}
	targetDimension := ""
	if stats != nil {
		targetDimension = strings.TrimSpace(stats.WeakestDimension)
	}
	var selected *BenchmarkClip
	for idx := range items {
		item := items[idx]
		if targetDimension != "" && strings.TrimSpace(item.Dimension) == targetDimension {
			selected = &item
			break
		}
	}
	if selected == nil {
		sort.Slice(items, func(i, j int) bool {
			if items[i].UsedInMeetings == items[j].UsedInMeetings {
				return items[i].ID > items[j].ID
			}
			return items[i].UsedInMeetings < items[j].UsedInMeetings
		})
		selected = &items[0]
	}
	clipText := strings.TrimSpace(selected.ClipText)
	learningPoints := append([]string{}, selected.LearningPoints...)
	audioStart := selected.AudioStartSecond
	audioEnd := selected.AudioEndSecond
	evidence := []string{}
	if isWeakWeeklyBenchmarkClipText(clipText) || len(learningPoints) == 0 || audioStart == nil || audioEnd == nil {
		fallbackEvidence, fallbackClip, fallbackStart, fallbackEnd, ok := s.loadWeeklyBenchmarkSupport(ctx, selected.RecordingID, roleCode, selected.Dimension)
		if ok {
			evidence = fallbackEvidence
			if isWeakWeeklyBenchmarkClipText(clipText) && strings.TrimSpace(fallbackClip) != "" {
				clipText = strings.TrimSpace(fallbackClip)
			}
			if audioStart == nil {
				audioStart = fallbackStart
			}
			if audioEnd == nil {
				audioEnd = fallbackEnd
			}
		}
	}
	if len(evidence) == 0 && strings.TrimSpace(clipText) != "" {
		evidence = []string{clipText}
	}
	summary := strings.TrimSpace(selected.AIComment)
	if summary == "" {
		summary = buildDiscoveryInsightSummary(roleCode, selected.Dimension, evidence, clipText)
	}
	if len(learningPoints) == 0 {
		learningPoints = buildWeeklyBenchmarkLearningPoints(roleCode, selected.Dimension, summary, clipText)
	}
	scoreText := weeklyBenchmarkScoreText(roleCode, selected.Score)
	return &WeeklyMeetingBenchmarkStudy{
		ClipID:             selected.ID,
		RecordingID:        selected.RecordingID,
		EmployeeID:         selected.EmployeeID,
		EmployeeName:       selected.EmployeeName,
		Dimension:          selected.Dimension,
		Score:              selected.Score,
		ScoreText:          scoreText,
		SceneName:          selected.SceneType,
		InsightSummary:     summary,
		ClipText:           clipText,
		LearningPoints:     learningPoints,
		UsedInMeetings:     int64(selected.UsedInMeetings),
		AudioStartSeconds:  audioStart,
		AudioEndSeconds:    audioEnd,
		AlreadyInBenchmark: true,
	}
}

func (s *Service) ensureWeeklyBenchmarkClipReady(ctx context.Context, tenantID int64, roleCode string, item *BenchmarkClip) *BenchmarkClip {
	if item == nil {
		return nil
	}
	needsClip := isWeakWeeklyBenchmarkClipText(item.ClipText) || item.AudioStartSecond == nil || item.AudioEndSecond == nil
	needsComment := isWeakWeeklyBenchmarkComment(roleCode, item.Dimension, item.AIComment)
	needsPoints := isWeakWeeklyBenchmarkLearningPoints(roleCode, item.Dimension, item.LearningPoints)
	needsLLM := needsComment || needsPoints
	if !needsClip && !needsLLM {
		return item
	}
	evidence, fallbackClip, fallbackStart, fallbackEnd, ok := s.loadWeeklyBenchmarkSupport(ctx, item.RecordingID, roleCode, item.Dimension)
	clipText := strings.TrimSpace(item.ClipText)
	if ok && strings.TrimSpace(fallbackClip) != "" && (needsClip || strings.TrimSpace(clipText) == "") {
		clipText = strings.TrimSpace(fallbackClip)
	}
	aiComment := strings.TrimSpace(item.AIComment)
	learningPoints := append([]string{}, item.LearningPoints...)
	if needsLLM {
		temp := *item
		if strings.TrimSpace(clipText) != "" {
			temp.ClipText = clipText
		}
		comment, points := s.generateBenchmarkCommentAndPoints(ctx, tenantID, &temp)
		if needsComment && strings.TrimSpace(comment) != "" {
			aiComment = strings.TrimSpace(comment)
		}
		if needsPoints && len(points) > 0 {
			learningPoints = points
		}
	}
	if strings.TrimSpace(aiComment) == "" {
		aiComment = buildDiscoveryInsightSummary(roleCode, item.Dimension, evidence, clipText)
	}
	if len(learningPoints) == 0 {
		learningPoints = buildWeeklyBenchmarkLearningPoints(roleCode, item.Dimension, aiComment, clipText)
	}
	updated, err := s.store.UpdateBenchmarkClipContent(ctx, tenantID, item.ID, clipText, aiComment, learningPoints, fallbackStart, fallbackEnd)
	if err != nil || updated == nil {
		item.ClipText = clipText
		item.AIComment = aiComment
		item.LearningPoints = learningPoints
		if item.AudioStartSecond == nil {
			item.AudioStartSecond = fallbackStart
		}
		if item.AudioEndSecond == nil {
			item.AudioEndSecond = fallbackEnd
		}
		return item
	}
	return updated
}

func weeklyBenchmarkScoreText(roleCode string, score float64) string {
	role := normalizeMorningMeetingRole(roleCode)
	if role == "consultant" {
		normalized := score
		if normalized > 5 {
			normalized = roundFloat(normalized/20.0, 1)
		}
		if normalized >= 5 {
			return "5分满分"
		}
		if math.Abs(normalized-math.Round(normalized)) < 0.05 {
			return fmt.Sprintf("%.0f/5分", math.Round(normalized))
		}
		return fmt.Sprintf("%.1f/5分", normalized)
	}
	return fmt.Sprintf("%.1f%%", roundFloat(score, 1))
}

func isWeakWeeklyBenchmarkClipText(text string) bool {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return true
	}
	if strings.HasPrefix(raw, "推荐说明：") || strings.HasPrefix(raw, "推荐依据：") {
		return true
	}
	if len([]rune(raw)) < 10 {
		return true
	}
	return false
}

func isWeakWeeklyBenchmarkComment(roleCode, dimension, comment string) bool {
	raw := strings.TrimSpace(comment)
	if raw == "" {
		return true
	}
	if strings.Contains(raw, "表现较好，表达清晰、节奏稳定") {
		return true
	}
	if strings.Contains(raw, "适合用于团队复盘讲评") && !strings.Contains(raw, "风险") && !strings.Contains(raw, "顾虑") && !strings.Contains(raw, "治疗") && !strings.Contains(raw, "患者") {
		return true
	}
	betterComment, _ := buildBenchmarkCommentAndPoints(&BenchmarkClip{
		RoleCode:  roleCode,
		Dimension: dimension,
	})
	return raw == strings.TrimSpace(betterComment)
}

func isWeakWeeklyBenchmarkLearningPoints(roleCode, dimension string, points []string) bool {
	if len(points) == 0 {
		return true
	}
	normalized := make([]string, 0, len(points))
	for _, item := range points {
		text := strings.TrimSpace(item)
		if text != "" {
			normalized = append(normalized, text)
		}
	}
	if len(normalized) == 0 {
		return true
	}
	generic := []string{
		"先明确对方具体情境，再给针对性回应",
		"使用容易理解的表述，减少抽象术语",
		"结尾给出明确下一步，形成沟通闭环",
	}
	if len(normalized) == len(generic) {
		match := true
		for idx := range generic {
			if normalized[idx] != generic[idx] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func (s *Service) loadWeeklyBenchmarkSupport(ctx context.Context, recordingID int64, roleCode string, dimension string) ([]string, string, *int, *int, bool) {
	var (
		transcriptText      string
		qualityScore        float64
		analysis            map[string]interface{}
		cleanedSegmentsRaw  interface{}
		timelineSegmentsRaw interface{}
	)
	err := s.store.pool.QueryRow(ctx, `
		SELECT
			COALESCE(
				NULLIF(
					CASE
						WHEN cleaned_transcription IS NULL THEN ''
						WHEN jsonb_typeof(cleaned_transcription) = 'string' THEN trim(both '"' from cleaned_transcription::text)
						WHEN jsonb_typeof(cleaned_transcription) = 'object' THEN COALESCE(cleaned_transcription->>'full_text', cleaned_transcription->>'text', '')
						ELSE ''
					END,
					''
				),
				COALESCE(transcription_text, '')
			) AS transcript_text,
			COALESCE(quality_score, 0)::float8,
			COALESCE(analysis_result, '{}'::json),
			cleaned_transcription,
			transcription_segments
		FROM recordings
		WHERE id = $1
		LIMIT 1
	`, recordingID).Scan(&transcriptText, &qualityScore, &analysis, &cleanedSegmentsRaw, &timelineSegmentsRaw)
	if err != nil {
		return nil, "", nil, nil, false
	}
	item := morningMeetingCandidate{
		RecordingID:    recordingID,
		TranscriptText: transcriptText,
		QualityScore:   qualityScore,
		Analysis:       analysis,
	}
	evidence := extractDimensionEvidence(analysis, roleCode, dimension)
	item.Evidence = evidence
	start, end := locateMorningMeetingAudioWindow(item, cleanedSegmentsRaw, timelineSegmentsRaw)
	clip := ""
	if len(evidence) > 0 {
		clip = evidence[0]
	}
	return evidence, clip, start, end, true
}

func buildWeeklyBenchmarkLearningPoints(roleCode, dimension, summary, clipText string) []string {
	role := normalizeMorningMeetingRole(roleCode)
	dim := strings.TrimSpace(dimension)
	if role == "consultant" {
		switch dim {
		case "问题放大":
			return []string{
				"用生活场景类比，不直接堆专业术语。",
				"把后果和患者年龄、职业或日常困扰绑在一起。",
				"引导患者自己意识到问题，而不是直接施压。",
			}
		case "需求探索":
			return []string{
				"先问真实诉求，再进入方案介绍。",
				"连续追问工作、用眼、预算等关键信息。",
				"避免基于模糊需求直接推荐全部选项。",
			}
		case "异议化解":
			return []string{
				"先复述顾虑，再回应，不要立刻反驳。",
				"多用同类案例或具体对比，降低抽象争论。",
				"把回应落回患者自己的决策场景。",
			}
		}
	}
	if role == "doctor" {
		return []string{
			"按接诊阶段推进，避免信息跳跃。",
			"先回应患者视角，再给专业建议。",
			"把关键动作说清楚，方便团队模仿复用。",
		}
	}
	if role == "therapist" {
		return []string{
			"操作中持续确认患者体感和理解。",
			"治疗前后都给患者清晰预期。",
			"把可复用的标准化动作沉淀下来。",
		}
	}
	out := make([]string, 0, 3)
	if strings.TrimSpace(summary) != "" {
		out = append(out, summary)
	}
	if strings.TrimSpace(clipText) != "" {
		out = append(out, "围绕这句原文拆解可复制动作。")
	}
	if len(out) == 0 {
		out = append(out, "提炼该片段中的高分动作，转成团队可复用话术。")
	}
	if len(out) > 3 {
		return out[:3]
	}
	return out
}

func (s *Service) buildWeeklyProblemReview(ctx context.Context, tenantID int64, roleCode string, candidates []morningMeetingCandidate) *WeeklyMeetingProblemReview {
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].OverallScore == candidates[j].OverallScore {
			return candidates[i].RecordedAt.After(candidates[j].RecordedAt)
		}
		return candidates[i].OverallScore < candidates[j].OverallScore
	})
	selected := candidates[0]
	if len(selected.Evidence) == 0 || selected.AudioStartSecond == nil || selected.AudioEndSecond == nil {
		evidence, _, start, end, ok := s.loadWeeklyBenchmarkSupport(ctx, selected.RecordingID, roleCode, selected.PrimaryDimension)
		if ok {
			if len(selected.Evidence) == 0 && len(evidence) > 0 {
				selected.Evidence = evidence
			}
			if selected.AudioStartSecond == nil {
				selected.AudioStartSecond = start
			}
			if selected.AudioEndSecond == nil {
				selected.AudioEndSecond = end
			}
		}
	}
	diagnosis, script := buildMorningMeetingFallback(roleCode, selected)
	return &WeeklyMeetingProblemReview{
		RecordingID:       selected.RecordingID,
		EmployeeID:        selected.EmployeeID,
		EmployeeName:      selected.EmployeeName,
		SceneName:         selected.SceneName,
		SelectionReason:   fmt.Sprintf("本周综合分最低，优先作为周会复盘录音。"),
		OverallScore:      selected.OverallScore,
		OverallScoreText:  formatWeeklyScoreText(roleCode, selected.OverallScore),
		PrimaryDimension:  selected.PrimaryDimension,
		PrimaryScore:      selected.PrimaryScore,
		PrimaryScoreText:  selected.PrimaryScoreText,
		Evidence:          selected.Evidence,
		Diagnosis:         diagnosis,
		CoachingScript:    script,
		GenerationMethod:  "rule",
		GenerationStatus:  "success",
		FallbackReason:    "weekly_non_blocking",
		AudioStartSeconds: selected.AudioStartSecond,
		AudioEndSeconds:   selected.AudioEndSecond,
	}
}

func (s *Service) buildWeeklyActionTracking(ctx context.Context, tenantID int64, roleCode string, prevWeekStart, prevWeekEnd, currentWeekEnd time.Time, stats *WeeklyMeetingStats) ([]WeeklyMeetingActionTrackingItem, string) {
	events, err := s.store.ListManagementEvents(ctx, tenantID, roleCode, "", &prevWeekStart, &currentWeekEnd)
	if err != nil || len(events) == 0 {
		return []WeeklyMeetingActionTrackingItem{}, "上周未记录会议决议，本周暂无可追踪内容。建议本次周会结束后记录1-3条明确决议，供下周自动追踪。"
	}
	hasPreviousWeekDecision := false
	items := make([]WeeklyMeetingActionTrackingItem, 0, 3)
	for idx := len(events) - 1; idx >= 0; idx-- {
		event := events[idx]
		if !strings.Contains(strings.ToLower(strings.TrimSpace(event.EventType)), "meeting_decision") {
			continue
		}
		eventDate, parseErr := time.Parse("2006-01-02", event.EventDate)
		if parseErr != nil {
			continue
		}
		matchesPreviousWeek := !(eventDate.Before(prevWeekStart) || eventDate.After(prevWeekEnd))
		metaWeekStart := strings.TrimSpace(pickString(event.Meta, "week_start"))
		metaMeetingType := strings.TrimSpace(pickString(event.Meta, "meeting_type"))
		metaSource := strings.TrimSpace(pickString(event.Meta, "source"))
		if metaMeetingType != "" && !strings.EqualFold(metaMeetingType, "weekly") {
			continue
		}
		if metaSource != "" && !strings.EqualFold(metaSource, "weekly_meeting") {
			continue
		}
		if metaWeekStart != "" {
			matchesPreviousWeek = metaWeekStart == prevWeekStart.Format("2006-01-02")
		}
		if !matchesPreviousWeek {
			continue
		}
		hasPreviousWeekDecision = true
		decisionText := strings.TrimSpace(pickString(event.Meta, "decision_text"))
		if decisionText == "" {
			decisionText = weeklyDecisionText(event.Title)
		}
		ownerName := strings.TrimSpace(pickString(event.Meta, "owner_name"))
		dueDate := strings.TrimSpace(pickString(event.Meta, "due_date"))
		taskStatus, taskStatusLabel := s.loadWeeklyDecisionTaskStatus(ctx, tenantID, roleCode, decisionText, event.CreatedAt)
		status := "warning"
		statusLabel := "⚠️ 需继续跟进"
		outcome := "本周已纳入追踪，建议继续复核落地情况。"
		followUpAdvice := "建议继续跟进，避免会议决议停留在口头。"
		currentScoreText := ""
		deltaText := ""
		dimension := strings.TrimSpace(pickString(event.Meta, "tracking_dimension"))
		if dimension == "" {
			dimension = strings.TrimSpace(event.DimensionCode)
		}
		if stats != nil && dimension != "" {
			for _, change := range stats.DimensionChanges {
				if strings.TrimSpace(change.Dimension) != dimension {
					continue
				}
				currentScoreText = formatWeeklyScoreText(roleCode, change.Current)
				if change.Delta > 0 {
					deltaText = fmt.Sprintf("+%.1f", change.Delta)
					outcome = fmt.Sprintf("%s本周 %s，较上周 %s。", change.Dimension, currentScoreText, deltaText)
					if taskStatus == "completed" {
						status = "success"
						statusLabel = "✅ 有改善"
						followUpAdvice = "本周已有改善，建议继续抽查，避免回弹。"
					} else {
						status = "progress"
						statusLabel = "🟡 有动作"
						followUpAdvice = "数据已有改善，但动作还要继续盯到稳定。"
					}
				} else if change.Delta < 0 {
					deltaText = fmt.Sprintf("%.1f", change.Delta)
					outcome = fmt.Sprintf("%s本周 %s，较上周 %s。", change.Dimension, currentScoreText, deltaText)
					status = "warning"
					statusLabel = "⚠️ 未见改善"
					if taskStatus == "completed" {
						followUpAdvice = "动作已执行，但结果还没起来，建议升级为1对1辅导。"
					} else {
						followUpAdvice = "结果仍未改善，且动作未真正落地，建议升级为1对1辅导。"
					}
				} else {
					deltaText = "持平"
					outcome = fmt.Sprintf("%s本周 %s，较上周持平。", change.Dimension, currentScoreText)
					if taskStatus == "completed" {
						status = "progress"
						statusLabel = "🟡 已执行待观察"
						followUpAdvice = "动作已经执行，但结果暂时没变化，建议再盯一周。"
					} else {
						status = "warning"
						statusLabel = "⚠️ 执行偏弱"
						followUpAdvice = "本周尚未见变化，建议明确责任人与截止时间后继续追踪。"
					}
				}
				break
			}
		}
		if outcome != "" && taskStatusLabel != "" {
			outcome = fmt.Sprintf("%s %s。", strings.TrimSuffix(outcome, "。"), taskStatusLabel)
		}
		items = append(items, WeeklyMeetingActionTrackingItem{
			Title:            event.Title,
			DecisionText:     decisionText,
			Description:      weeklyActionTrackingDescription(ownerName, dueDate),
			Outcome:          outcome,
			FollowUpAdvice:   followUpAdvice,
			Status:           status,
			StatusLabel:      statusLabel,
			OwnerName:        ownerName,
			DueDate:          dueDate,
			TaskStatus:       taskStatus,
			TaskStatusLabel:  taskStatusLabel,
			Dimension:        dimension,
			CurrentScoreText: currentScoreText,
			DeltaText:        deltaText,
		})
		if len(items) >= 3 {
			break
		}
	}
	if len(items) == 0 {
		if hasPreviousWeekDecision {
			return []WeeklyMeetingActionTrackingItem{}, "上周已记录会议决议，但本周暂未形成可量化的追踪结果，建议先核对任务执行与维度数据。"
		}
		return []WeeklyMeetingActionTrackingItem{}, "上周未记录会议决议，本周暂无可追踪内容。建议本次周会结束后记录1-3条明确决议，供下周自动追踪。"
	}
	return items, ""
}

func weeklyDecisionText(title string) string {
	text := strings.TrimSpace(title)
	text = strings.TrimPrefix(text, "周会决议：")
	text = strings.TrimPrefix(text, "早会决议：")
	return strings.TrimSpace(text)
}

func weeklyActionTrackingDescription(ownerName, dueDate string) string {
	parts := make([]string, 0, 2)
	if ownerName != "" {
		parts = append(parts, fmt.Sprintf("责任人：%s", ownerName))
	}
	if dueDate != "" {
		parts = append(parts, fmt.Sprintf("截止：%s", dueDate))
	}
	return strings.Join(parts, " · ")
}

func (s *Service) loadWeeklyDecisionTaskStatus(ctx context.Context, tenantID int64, roleCode, decisionText, eventCreatedAt string) (string, string) {
	taskTitle := fmt.Sprintf("周会行动：%s", strings.TrimSpace(decisionText))
	if strings.TrimSpace(decisionText) == "" {
		return "", ""
	}
	var (
		status      string
		completedAt *time.Time
		dueAt       *time.Time
	)
	err := s.store.pool.QueryRow(ctx, `
		SELECT status, completed_at, due_at
		FROM recording_tasks
		WHERE tenant_id = $1
		  AND recording_role_category = $2
		  AND source_detail = 'meeting_decision'
		  AND title = $3
		ORDER BY created_at DESC
		LIMIT 1
	`, tenantID, roleCode, taskTitle).Scan(&status, &completedAt, &dueAt)
	if err != nil {
		return "", ""
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		if completedAt != nil {
			return "completed", fmt.Sprintf("责任任务已完成（%s）", completedAt.Format("01-02"))
		}
		return "completed", "责任任务已完成"
	case "cancelled":
		return "cancelled", "责任任务已取消"
	case "assigned", "pending":
		if dueAt != nil && dueAt.Before(time.Now()) {
			return "overdue", "责任任务已到期未完成"
		}
		return "in_progress", "责任任务进行中"
	default:
		return strings.ToLower(strings.TrimSpace(status)), ""
	}
}

func (s *Service) buildWeeklyNextFocus(roleCode string, stats *WeeklyMeetingStats, benchmark *WeeklyMeetingBenchmarkStudy, problem *WeeklyMeetingProblemReview, actionTracking []WeeklyMeetingActionTrackingItem) []string {
	items := make([]string, 0, 3)
	if stats != nil && strings.TrimSpace(stats.WeakestDimension) != "" {
		items = append(items, fmt.Sprintf("团队级：%s 仍是本周薄弱点，建议做一次专项讨论。", stats.WeakestDimension))
	}
	if unresolved := pickWeeklyActionFollowUp(actionTracking); unresolved != "" {
		items = append(items, unresolved)
	} else if problem != nil {
		items = append(items, fmt.Sprintf("个人级：%s 的%s需要继续1对1复盘。", problem.EmployeeName, problem.PrimaryDimension))
	}
	if benchmark != nil {
		items = append(items, fmt.Sprintf("标杆复制：把%s在“%s”里的做法拆成团队可复用动作。", benchmark.EmployeeName, benchmark.Dimension))
	}
	if len(items) > 3 {
		return items[:3]
	}
	return items
}

func pickWeeklyActionFollowUp(items []WeeklyMeetingActionTrackingItem) string {
	for _, item := range items {
		if strings.TrimSpace(item.Status) == "warning" {
			if strings.TrimSpace(item.DecisionText) != "" {
				return fmt.Sprintf("跟进级：上周“%s”仍未落稳，建议明确责任人后继续追踪。", item.DecisionText)
			}
			if strings.TrimSpace(item.FollowUpAdvice) != "" {
				return fmt.Sprintf("跟进级：%s", item.FollowUpAdvice)
			}
		}
	}
	for _, item := range items {
		if strings.TrimSpace(item.Status) == "progress" && strings.TrimSpace(item.DecisionText) != "" {
			return fmt.Sprintf("跟进级：上周“%s”已有动作，建议下周继续核对是否稳定。", item.DecisionText)
		}
	}
	return ""
}

func normalizeMorningMeetingRole(roleCode string) string {
	switch strings.ToLower(strings.TrimSpace(roleCode)) {
	case "doctor", "doctor_assistant":
		return "doctor"
	case "therapist":
		return "therapist"
	case "consultant":
		return "consultant"
	default:
		return ""
	}
}

func parseMorningMeetingDate(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	}
	dt, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid meeting_date")
	}
	return dt, nil
}

func (s *Service) listMorningMeetingCandidates(ctx context.Context, tenantID int64, roleCode string, reviewDate time.Time) ([]morningMeetingCandidate, error) {
	start := reviewDate.Format("2006-01-02")
	end := reviewDate.Add(24 * time.Hour).Format("2006-01-02")
	filterClause := morningMeetingRoleFilterSQL(roleCode)
	rows, err := s.store.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			r.id,
			r.employee_id,
			COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知员工') AS employee_name,
			COALESCE(NULLIF(r.scene_name, ''), NULLIF(r.scene, ''), '') AS scene_name,
			COALESCE(r.recorded_at, r.created_at) AS ts,
			COALESCE(r.duration, 0)::bigint,
			COALESCE(
				NULLIF(
					CASE
						WHEN r.cleaned_transcription IS NULL THEN ''
						WHEN jsonb_typeof(r.cleaned_transcription) = 'string' THEN trim(both '"' from r.cleaned_transcription::text)
						WHEN jsonb_typeof(r.cleaned_transcription) = 'object' THEN COALESCE(r.cleaned_transcription->>'full_text', r.cleaned_transcription->>'text', '')
						ELSE ''
					END,
					''
				),
				COALESCE(r.transcription_text, '')
			) AS transcript_text,
			COALESCE(r.quality_score, 0)::float8,
			COALESCE(r.analysis_result, '{}'::json),
			r.cleaned_transcription,
			r.transcription_segments
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		  %s
		ORDER BY ts DESC, r.id DESC
	`, filterClause), tenantID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query morning meeting candidates: %w", err)
	}
	defer rows.Close()

	items := make([]morningMeetingCandidate, 0, 32)
	for rows.Next() {
		var (
			item                morningMeetingCandidate
			cleanedSegmentsRaw  interface{}
			timelineSegmentsRaw interface{}
		)
		if scanErr := rows.Scan(&item.RecordingID, &item.EmployeeID, &item.EmployeeName, &item.SceneName, &item.RecordedAt, &item.DurationSeconds, &item.TranscriptText, &item.QualityScore, &item.Analysis, &cleanedSegmentsRaw, &timelineSegmentsRaw); scanErr != nil {
			return nil, fmt.Errorf("failed to scan morning meeting candidate: %w", scanErr)
		}
		s.enrichMorningMeetingCandidate(&item, roleCode)
		s.enrichMorningMeetingSuddenDrop(ctx, tenantID, roleCode, reviewDate, &item)
		item.InvalidReason = detectMorningMeetingInvalidReason(item)
		item.AudioStartSecond, item.AudioEndSecond = locateMorningMeetingAudioWindow(item, cleanedSegmentsRaw, timelineSegmentsRaw)
		items = append(items, item)
	}
	return items, nil
}

func morningMeetingRoleFilterSQL(roleCode string) string {
	switch normalizeMorningMeetingRole(roleCode) {
	case "doctor":
		return `
		  AND EXISTS (
			SELECT 1 FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor','doctor_assistant'])
		  )`
	case "therapist":
		return `
		  AND EXISTS (
			SELECT 1 FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = 'therapist'
		  )`
	default:
		return `
		  AND EXISTS (
			SELECT 1 FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = 'consultant'
		  )`
	}
}

func detectMorningMeetingInvalidReason(item morningMeetingCandidate) string {
	text := strings.TrimSpace(item.TranscriptText)
	if item.DurationSeconds > 0 && item.DurationSeconds <= 15 {
		return "录音时长过短"
	}
	if text == "" {
		return "转写为空"
	}
	if len([]rune(text)) < 20 {
		return "有效对话过少"
	}
	if item.OverallScore <= 0 && len(item.Evidence) == 0 && strings.TrimSpace(item.Narrative) == "" {
		return "未识别出可复盘对话"
	}
	return ""
}

func filterUsableMorningMeetingCandidates(items []morningMeetingCandidate) []morningMeetingCandidate {
	out := make([]morningMeetingCandidate, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.InvalidReason) != "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func locateMorningMeetingAudioWindow(item morningMeetingCandidate, cleanedSegmentsRaw, timelineSegmentsRaw interface{}) (*int, *int) {
	targets := make([]string, 0, 4)
	if len(item.Evidence) > 0 {
		targets = append(targets, item.Evidence...)
	}
	if strings.TrimSpace(item.Narrative) != "" {
		targets = append(targets, item.Narrative)
	}
	if strings.TrimSpace(item.TranscriptText) != "" {
		targets = append(targets, item.TranscriptText)
	}
	timeline := buildMorningMeetingTimeline(cleanedSegmentsRaw, timelineSegmentsRaw, item.TranscriptText)
	if len(timeline) == 0 {
		return nil, nil
	}
	for _, target := range targets {
		start, end, ok := findMorningMeetingWindowFromTimeline(timeline, target)
		if ok {
			return &start, &end
		}
	}
	return nil, nil
}

func buildMorningMeetingTimeline(cleanedSegmentsRaw, timelineSegmentsRaw interface{}, transcriptText string) []map[string]interface{} {
	if segments := decodeSegmentsJSON(timelineSegmentsRaw); len(segments) > 0 {
		_, timeline := buildTranscriptFromSegments(segments)
		if len(timeline) > 0 {
			return timeline
		}
	}
	if segments := decodeSegmentsJSON(cleanedSegmentsRaw); len(segments) > 0 {
		_, timeline := buildTranscriptFromSegments(segments)
		if len(timeline) > 0 {
			return timeline
		}
	}
	_, timeline := buildTranscriptFallback(transcriptText)
	return timeline
}

func findMorningMeetingWindowFromTimeline(timeline []map[string]interface{}, target string) (int, int, bool) {
	normalizedTarget := normalizeMorningMeetingSnippet(target)
	if normalizedTarget == "" {
		return 0, 0, false
	}
	bestIdx := -1
	bestScore := 0
	bestStart := 0
	bestEnd := 0
	for idx, segment := range timeline {
		text := strings.TrimSpace(firstNonEmptyText(segment["text"], segment["content"]))
		normalizedText := normalizeMorningMeetingSnippet(text)
		if normalizedText == "" {
			continue
		}
		score := overlapMorningMeetingSnippetScore(normalizedTarget, normalizedText)
		if score <= bestScore {
			continue
		}
		bestIdx = idx
		bestScore = score
		bestStart = pickIntValue(segment["start_seconds"], segment["start_time"], segment["start"], 0)
		bestEnd = pickIntValue(segment["end_seconds"], segment["end_time"], segment["end"], bestStart+estimateDurationSeconds(text))
	}
	if bestIdx < 0 || bestScore < 4 {
		return 0, 0, false
	}
	if bestStart < 0 {
		bestStart = 0
	}
	if bestEnd < bestStart {
		bestEnd = bestStart
	}
	if bestEnd-bestStart < 8 {
		bestEnd = bestStart + 8
	}
	if bestIdx+1 < len(timeline) {
		nextStart := pickIntValue(timeline[bestIdx+1]["start_seconds"], timeline[bestIdx+1]["start_time"], timeline[bestIdx+1]["start"], bestEnd)
		if nextStart > bestStart {
			bestEnd = nextStart
		}
	}
	return bestStart, bestEnd, true
}

func normalizeMorningMeetingSnippet(text string) string {
	replacer := strings.NewReplacer("…", "", "...", "", "“", "", "”", "", "\"", "", "，", "", "。", "", "？", "", "！", "", "；", "", "：", "", "\n", "", "\r", "", "\t", "", " ", "")
	return strings.ToLower(strings.TrimSpace(replacer.Replace(text)))
}

func overlapMorningMeetingSnippetScore(target, candidate string) int {
	if target == "" || candidate == "" {
		return 0
	}
	if strings.Contains(candidate, target) || strings.Contains(target, candidate) {
		return maxInt(len([]rune(candidate)), len([]rune(target)))
	}
	best := 0
	runes := []rune(target)
	window := len(runes)
	if window > 24 {
		window = 24
	}
	for size := window; size >= 4; size-- {
		for start := 0; start+size <= len(runes); start++ {
			token := string(runes[start : start+size])
			if strings.Contains(candidate, token) {
				return size
			}
		}
		if best > 0 {
			break
		}
	}
	return best
}

func pickIntValue(values ...interface{}) int {
	for _, value := range values {
		if ptr := pickInt(value); ptr != nil {
			return *ptr
		}
	}
	return 0
}

func (s *Service) enrichMorningMeetingCandidate(item *morningMeetingCandidate, roleCode string) {
	if item == nil {
		return
	}
	role := normalizeMorningMeetingRole(roleCode)
	switch role {
	case "doctor":
		scores := computeDoctorMorningScores(item.Analysis)
		item.DimensionScores = scores
		item.OverallScore, item.PrimaryDimension, item.PrimaryScore = pickLowestMorningScore(scores)
		item.Evidence = extractDimensionEvidence(item.Analysis, role, item.PrimaryDimension)
		item.Narrative = strings.TrimSpace(firstNonEmptyText(
			pickString(pickMap(pickMap(item.Analysis, "raw"), "doctor_content_gen"), "draft_note"),
			pickString(item.Analysis, "critical_summary"),
			pickString(item.Analysis, "report"),
		))
	case "therapist":
		scores := computeTherapistMorningScores(item.Analysis)
		item.DimensionScores = scores
		item.OverallScore, item.PrimaryDimension, item.PrimaryScore = pickLowestMorningScore(scores)
		item.Evidence = extractDimensionEvidence(item.Analysis, role, item.PrimaryDimension)
		raw := pickMap(item.Analysis, "raw")
		item.Narrative = strings.TrimSpace(firstNonEmptyText(
			pickString(pickMap(pickMap(raw, "therapist_reset_analysis"), "treatment_record"), "pre_assessment"),
			pickString(pickMap(pickMap(raw, "therapist_reset_analysis"), "treatment_record"), "post_change"),
			strings.Join(pickStringSlice(firstNonEmptyArray(
				pickArray(pickMap(raw, "therapist_reset_analysis"), "improvement_priorities"),
				pickArray(pickMap(pickMap(raw, "therapist_reset_analysis"), "reset"), "improvement_priorities"),
			)), "；"),
			pickString(item.Analysis, "critical_summary"),
			pickString(item.Analysis, "report"),
		))
	default:
		scores := computeConsultantMorningScores(item.Analysis, item.QualityScore)
		item.DimensionScores = scores
		item.OverallScore, item.PrimaryDimension, item.PrimaryScore = pickLowestMorningScore(scores)
		item.Evidence = extractDimensionEvidence(item.Analysis, role, item.PrimaryDimension)
		item.Narrative = strings.TrimSpace(firstNonEmptyText(
			pickString(item.Analysis, "report"),
			pickString(pickMap(item.Analysis, "quality_score"), "summary"),
		))
	}
	if strings.TrimSpace(item.PrimaryDimension) == "" {
		item.PrimaryDimension = inferMorningPrimaryDimension(role, item.Analysis, item.Narrative)
	}
	if item.PrimaryScore <= 0 {
		item.PrimaryScore = inferMorningPrimaryScore(role, item.Analysis, item.OverallScore)
	}
	if len(item.Evidence) == 0 {
		item.Evidence = extractMorningFallbackEvidence(role, item.Analysis, item.Narrative)
	}
	if item.OverallScore <= 0 {
		item.OverallScore = inferMorningOverallScore(role, item.Analysis)
	}
	item.Positive = item.OverallScore >= morningMeetingPositiveThreshold(role)
	item.OverallScoreText = morningMeetingScoreText(role, item.OverallScore)
	item.PrimaryScoreText = morningMeetingDimensionScoreText(role, item.PrimaryScore)
}

func computeConsultantMorningScores(analysis map[string]interface{}, qualityScore float64) map[string]float64 {
	out := map[string]float64{}
	stages := pickArray(analysis, "stages")
	if len(stages) == 0 {
		stages = pickArray(pickMap(analysis, "quality_score"), "stages")
	}
	for _, st := range stages {
		m, ok := st.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(pickString(m, "name"))
		score := valueOrZeroFloat(pickFloat(m, "score"))
		if name == "" || score <= 0 {
			continue
		}
		out[name] = score
	}
	if qualityScore <= 0 {
		qualityScore = valueOrZeroFloat(pickFloat(pickMap(analysis, "quality_score"), "total"))
	}
	if len(out) == 0 && qualityScore > 0 {
		out["综合"] = roundFloat(qualityScore/20, 1)
	}
	return out
}

func computeDoctorMorningScores(analysis map[string]interface{}) map[string]float64 {
	result := map[string]float64{}
	counts := map[string]float64{}
	raw := pickMap(analysis, "raw")
	items := pickArray(pickMap(pickMap(raw, "doctor_segue_structured"), "segue"), "items")
	if len(items) == 0 {
		items = pickArray(pickMap(raw, "doctor_segue_structured"), "items")
	}
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		code := strings.TrimSpace(pickString(m, "code"))
		if len(code) < 2 {
			continue
		}
		group := normalizeDoctorDimensionCode(code[:2])
		switch strings.ToUpper(strings.TrimSpace(pickString(m, "result"))) {
		case "Y":
			result[group] += 1
			counts[group] += 1
		case "U":
			result[group] += 0.5
			counts[group] += 1
		case "N":
			counts[group] += 1
		}
	}
	out := map[string]float64{}
	for group, total := range counts {
		if total <= 0 {
			continue
		}
		out[doctorDimensionCodeToLabel(group)] = roundFloat(result[group]/total*100, 1)
	}
	if len(out) == 0 {
		for key, value := range pickMap(analysis, "dimension_scores") {
			score := 0.0
			switch v := value.(type) {
			case float64:
				score = v
			case json.Number:
				score, _ = v.Float64()
			case string:
				score, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
			}
			if score <= 0 {
				continue
			}
			out[doctorDimensionCodeToLabel(key)] = roundFloat(score, 1)
		}
	}
	return out
}

func computeTherapistMorningScores(analysis map[string]interface{}) map[string]float64 {
	result := map[string]float64{}
	counts := map[string]float64{}
	raw := pickMap(analysis, "raw")
	items := pickArray(pickMap(pickMap(raw, "therapist_reset_analysis"), "reset"), "items")
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		group := strings.ToUpper(strings.TrimSpace(pickString(m, "group")))
		if group == "" {
			code := strings.TrimSpace(pickString(m, "code"))
			if len(code) >= 2 {
				group = code[:2]
			}
		}
		group = normalizeTherapistDimensionCode(group)
		switch strings.ToUpper(strings.TrimSpace(pickString(m, "result"))) {
		case "Y":
			result[group] += 1
			counts[group] += 1
		case "U":
			result[group] += 0.5
			counts[group] += 1
		case "N":
			counts[group] += 1
		}
	}
	out := map[string]float64{}
	for group, total := range counts {
		if total <= 0 {
			continue
		}
		out[therapistDimensionCodeToLabel(group)] = roundFloat(result[group]/total*100, 1)
	}
	if len(out) == 0 {
		for key, value := range pickMap(analysis, "dimension_scores") {
			score := 0.0
			switch v := value.(type) {
			case float64:
				score = v
			case json.Number:
				score, _ = v.Float64()
			case string:
				score, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
			}
			if score <= 0 {
				continue
			}
			out[therapistDimensionCodeToLabel(key)] = roundFloat(score, 1)
		}
	}
	return out
}

func inferMorningOverallScore(roleCode string, analysis map[string]interface{}) float64 {
	role := normalizeMorningMeetingRole(roleCode)
	switch role {
	case "consultant":
		total := valueOrZeroFloat(pickFloat(pickMap(analysis, "quality_score"), "total"))
		if total > 0 {
			return roundFloat(total/20, 1)
		}
	case "doctor":
		if score := valueOrZeroFloat(pickFloat(analysis, "segue_percent")); score > 0 {
			return roundFloat(score, 1)
		}
	case "therapist":
		if score := valueOrZeroFloat(pickFloat(analysis, "reset_percent")); score > 0 {
			return roundFloat(score, 1)
		}
		if score := valueOrZeroFloat(pickFloat(analysis, "segue_percent")); score > 0 {
			return roundFloat(score, 1)
		}
	}
	return 0
}

func inferMorningPrimaryScore(roleCode string, analysis map[string]interface{}, overall float64) float64 {
	if overall > 0 {
		return roundFloat(overall, 1)
	}
	return inferMorningOverallScore(roleCode, analysis)
}

func inferMorningPrimaryDimension(roleCode string, analysis map[string]interface{}, narrative string) string {
	text := strings.ToLower(strings.Join([]string{
		narrative,
		pickString(analysis, "critical_summary"),
		pickString(analysis, "report"),
		strings.Join(pickStringSlice(pickArray(analysis, "improvement_priorities")), " "),
		strings.Join(pickStringSlice(pickArray(analysis, "highlights")), " "),
	}, " "))
	role := normalizeMorningMeetingRole(roleCode)
	switch role {
	case "consultant":
		switch {
		case strings.Contains(text, "异议"), strings.Contains(text, "顾虑"), strings.Contains(text, "后遗症"):
			return "异议化解"
		case strings.Contains(text, "需求"), strings.Contains(text, "场景"), strings.Contains(text, "预算"):
			return "需求探索"
		case strings.Contains(text, "方案"):
			return "方案定制"
		case strings.Contains(text, "收尾"), strings.Contains(text, "预约"), strings.Contains(text, "下一步"):
			return "促成与收尾"
		default:
			return "综合"
		}
	case "doctor":
		switch {
		case strings.Contains(text, "主诉"), strings.Contains(text, "问诊"), strings.Contains(text, "病情"):
			return "引出信息阶段"
		case strings.Contains(text, "计划"), strings.Contains(text, "治疗"), strings.Contains(text, "副作用"):
			return "治疗/预防计划"
		case strings.Contains(text, "同理"), strings.Contains(text, "担心"), strings.Contains(text, "视角"):
			return "理解患者视角"
		default:
			return "综合"
		}
	default:
		switch {
		case strings.Contains(text, "疗程"), strings.Contains(text, "家庭训练"), strings.Contains(text, "下次"), strings.Contains(text, "总结"):
			return "方案闭环"
		case strings.Contains(text, "感受"), strings.Contains(text, "体感"), strings.Contains(text, "变化"):
			return "互动评估"
		case strings.Contains(text, "解释"), strings.Contains(text, "操作"), strings.Contains(text, "评估"):
			return "专业操作"
		case strings.Contains(text, "铺垫"), strings.Contains(text, "治疗前"):
			return "治疗铺垫"
		default:
			return "综合"
		}
	}
}

func extractMorningFallbackEvidence(roleCode string, analysis map[string]interface{}, narrative string) []string {
	out := make([]string, 0, 2)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || looksNegativeText(v) {
			return
		}
		out = append(out, truncateText(v, 90))
	}
	for _, text := range pickStringSlice(pickArray(analysis, "highlights")) {
		add(text)
		if len(out) >= 2 {
			return out
		}
	}
	if roleCode == "consultant" || normalizeMorningMeetingRole(roleCode) == "consultant" {
		for _, text := range pickStringSlice(pickArray(analysis, "key_quotes")) {
			add(text)
			if len(out) >= 2 {
				return out
			}
		}
	}
	add(pickString(analysis, "critical_summary"))
	add(narrative)
	return out
}

func pickLowestMorningScore(scores map[string]float64) (float64, string, float64) {
	if len(scores) == 0 {
		return 0, "", 0
	}
	keys := make([]string, 0, len(scores))
	var sum float64
	for key, score := range scores {
		keys = append(keys, key)
		sum += score
	}
	sort.Strings(keys)
	overall := roundFloat(sum/float64(len(keys)), 1)
	weakest := keys[0]
	weakestScore := scores[weakest]
	for _, key := range keys[1:] {
		if scores[key] < weakestScore {
			weakest = key
			weakestScore = scores[key]
		}
	}
	return overall, weakest, weakestScore
}

func (s *Service) enrichMorningMeetingSuddenDrop(ctx context.Context, tenantID int64, roleCode string, reviewDate time.Time, item *morningMeetingCandidate) {
	if item == nil || item.EmployeeID <= 0 || len(item.DimensionScores) == 0 {
		return
	}
	baselines, err := s.loadMorningMeetingDimensionBaseline(ctx, tenantID, roleCode, item.EmployeeID, reviewDate)
	if err != nil || len(baselines) == 0 {
		return
	}
	var (
		bestDimension string
		bestDrop      float64
		bestBaseline  float64
	)
	for dim, current := range item.DimensionScores {
		baseline, ok := baselines[dim]
		if !ok || baseline <= 0 {
			continue
		}
		drop := baseline - current
		if drop > bestDrop {
			bestDimension = dim
			bestDrop = drop
			bestBaseline = baseline
		}
	}
	if bestDrop > 1.0 {
		item.DropDimension = bestDimension
		item.DropAmount = roundFloat(bestDrop, 1)
		item.DropBaseline = roundFloat(bestBaseline, 1)
	}
}

func (s *Service) loadMorningMeetingDimensionBaseline(ctx context.Context, tenantID int64, roleCode string, employeeID int64, reviewDate time.Time) (map[string]float64, error) {
	start := reviewDate.AddDate(0, 0, -14).Format("2006-01-02")
	end := reviewDate.Format("2006-01-02")
	rows, err := s.store.pool.Query(ctx, `
		SELECT COALESCE(r.quality_score, 0)::float8, COALESCE(r.analysis_result, '{}'::json)
		FROM recordings r
		WHERE r.tenant_id = $1
		  AND r.employee_id = $2
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $3
		  AND COALESCE(r.recorded_at, r.created_at) < $4
		ORDER BY COALESCE(r.recorded_at, r.created_at) DESC, r.id DESC
		LIMIT 20
	`, tenantID, employeeID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sums := map[string]float64{}
	counts := map[string]float64{}
	for rows.Next() {
		var (
			qualityScore float64
			analysis     map[string]interface{}
		)
		if scanErr := rows.Scan(&qualityScore, &analysis); scanErr != nil {
			return nil, scanErr
		}
		var scores map[string]float64
		switch normalizeMorningMeetingRole(roleCode) {
		case "doctor":
			scores = computeDoctorMorningScores(analysis)
		case "therapist":
			scores = computeTherapistMorningScores(analysis)
		default:
			scores = computeConsultantMorningScores(analysis, qualityScore)
		}
		for dim, score := range scores {
			if score <= 0 {
				continue
			}
			sums[dim] += score
			counts[dim] += 1
		}
	}
	out := map[string]float64{}
	for dim, total := range counts {
		if total <= 0 {
			continue
		}
		out[dim] = roundFloat(sums[dim]/total, 1)
	}
	return out, nil
}

func selectMorningMeetingCandidate(roleCode string, items []morningMeetingCandidate) morningMeetingCandidate {
	if len(items) == 0 {
		return morningMeetingCandidate{}
	}
	threshold := morningMeetingPositiveThreshold(roleCode)
	allHigh := true
	lowest := items[0]
	for _, item := range items[1:] {
		if item.OverallScore < lowest.OverallScore {
			lowest = item
		}
	}
	for _, item := range items {
		if item.OverallScore < threshold {
			allHigh = false
			break
		}
	}
	if !allHigh {
		lowest.SelectionReason = "昨日综合分最低，适合作为今天优先讲评录音。"
		return lowest
	}
	var (
		hasDrop bool
		dropHit = items[0]
	)
	for _, item := range items {
		if item.DropAmount <= 1 {
			continue
		}
		if !hasDrop || item.DropAmount > dropHit.DropAmount {
			hasDrop = true
			dropHit = item
		}
	}
	if hasDrop {
		dropHit.SelectionReason = fmt.Sprintf("%s较本人近期均值下降 %.1f，适合做针对性讲评。", dropHit.DropDimension, dropHit.DropAmount)
		return dropHit
	}
	best := items[0]
	for _, item := range items[1:] {
		if item.OverallScore > best.OverallScore {
			best = item
		}
	}
	best.Positive = true
	best.SelectionReason = "昨日整体表现较稳，选取高分录音做正向示范。"
	return best
}

func morningMeetingPositiveThreshold(roleCode string) float64 {
	if normalizeMorningMeetingRole(roleCode) == "consultant" {
		return 4
	}
	return 80
}

func morningMeetingScoreText(roleCode string, score float64) string {
	if normalizeMorningMeetingRole(roleCode) == "consultant" {
		return fmt.Sprintf("%.1f/5", score)
	}
	return fmt.Sprintf("%.1f%%", score)
}

func morningMeetingDimensionScoreText(roleCode string, score float64) string {
	return morningMeetingScoreText(roleCode, score)
}

func doctorDimensionCodeToLabel(code string) string {
	switch normalizeDoctorDimensionCode(code) {
	case "G1":
		return "建立接诊阶段"
	case "G2":
		return "引出信息阶段"
	case "G3":
		return "给予信息阶段"
	case "G4":
		return "理解患者视角"
	case "G5":
		return "结束接诊"
	case "G6":
		return "治疗/预防计划"
	default:
		return strings.TrimSpace(code)
	}
}

func therapistDimensionCodeToLabel(code string) string {
	switch normalizeTherapistDimensionCode(code) {
	case "D1":
		return "治疗铺垫"
	case "D2":
		return "互动评估"
	case "D3":
		return "专业操作"
	case "D4":
		return "顾虑处理"
	case "D5":
		return "方案闭环"
	default:
		return strings.TrimSpace(code)
	}
}

func (s *Service) ensureMorningMeetingMaterial(ctx context.Context, tenantID int64, roleCode string, meetingDate, reviewDate time.Time, candidate morningMeetingCandidate) (*morningMeetingMaterialRow, error) {
	existing, err := s.loadMorningMeetingMaterial(ctx, tenantID, roleCode, meetingDate)
	if err != nil {
		return nil, err
	}
	if existing != nil &&
		existing.RecordingID == candidate.RecordingID &&
		strings.TrimSpace(existing.Diagnosis) != "" &&
		strings.TrimSpace(existing.CoachingScript) != "" &&
		strings.TrimSpace(existing.PrimaryDimension) != "" &&
		strings.TrimSpace(existing.PrimaryScoreText) != "" {
		return existing, nil
	}

	diagnosis, script, method, status, fallbackReason, promptCode, modelCode, requestID := s.generateMorningMeetingContent(ctx, tenantID, roleCode, candidate)
	row := &morningMeetingMaterialRow{
		RecordingID:      candidate.RecordingID,
		EmployeeID:       candidate.EmployeeID,
		EmployeeName:     candidate.EmployeeName,
		SceneName:        candidate.SceneName,
		SelectionReason:  candidate.SelectionReason,
		OverallScore:     candidate.OverallScore,
		OverallScoreText: candidate.OverallScoreText,
		PrimaryDimension: candidate.PrimaryDimension,
		PrimaryScore:     candidate.PrimaryScore,
		PrimaryScoreText: candidate.PrimaryScoreText,
		Evidence:         candidate.Evidence,
		Diagnosis:        diagnosis,
		CoachingScript:   script,
		GenerationMethod: method,
		GenerationStatus: status,
		FallbackReason:   fallbackReason,
		PromptCode:       promptCode,
		ModelCode:        modelCode,
		LLMRequestID:     requestID,
	}
	if err := s.upsertMorningMeetingMaterial(ctx, tenantID, roleCode, meetingDate, reviewDate, row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) loadMorningMeetingMaterial(ctx context.Context, tenantID int64, roleCode string, meetingDate time.Time) (*morningMeetingMaterialRow, error) {
	var row morningMeetingMaterialRow
	var evidenceRaw []byte
	err := s.store.pool.QueryRow(ctx, `
		SELECT recording_id, employee_id, COALESCE(employee_name, ''), COALESCE(scene_name, ''),
		       COALESCE(selection_reason, ''), COALESCE(overall_score, 0), COALESCE(overall_score_text, ''),
		       COALESCE(primary_dimension, ''), COALESCE(primary_score, 0), COALESCE(primary_score_text, ''),
		       COALESCE(evidence, '[]'::jsonb), COALESCE(diagnosis, ''), COALESCE(coaching_script, ''),
		       COALESCE(generation_method, 'rule'), COALESCE(generation_status, 'success'), COALESCE(fallback_reason, ''),
		       COALESCE(prompt_code, ''), COALESCE(model_code, ''), COALESCE(llm_request_id, ''), used_at
		FROM morning_meeting_materials
		WHERE tenant_id = $1 AND role_code = $2 AND meeting_date = $3
		LIMIT 1
	`, tenantID, roleCode, meetingDate.Format("2006-01-02")).Scan(
		&row.RecordingID, &row.EmployeeID, &row.EmployeeName, &row.SceneName,
		&row.SelectionReason, &row.OverallScore, &row.OverallScoreText,
		&row.PrimaryDimension, &row.PrimaryScore, &row.PrimaryScoreText,
		&evidenceRaw, &row.Diagnosis, &row.CoachingScript,
		&row.GenerationMethod, &row.GenerationStatus, &row.FallbackReason,
		&row.PromptCode, &row.ModelCode, &row.LLMRequestID, &row.UsedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load morning meeting material: %w", err)
	}
	_ = json.Unmarshal(evidenceRaw, &row.Evidence)
	return &row, nil
}

func (s *Service) upsertMorningMeetingMaterial(ctx context.Context, tenantID int64, roleCode string, meetingDate, reviewDate time.Time, row *morningMeetingMaterialRow) error {
	if row == nil {
		return nil
	}
	evidenceJSON, _ := json.Marshal(row.Evidence)
	_, err := s.store.pool.Exec(ctx, `
		INSERT INTO morning_meeting_materials (
			tenant_id, role_code, meeting_date, review_date, recording_id, employee_id, employee_name, scene_name,
			selection_reason, overall_score, overall_score_text, primary_dimension, primary_score, primary_score_text,
			evidence, diagnosis, coaching_script, generation_method, generation_status, fallback_reason,
			prompt_code, model_code, llm_request_id, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,
			$9,$10,$11,$12,$13,$14,
			$15::jsonb,$16,$17,$18,$19,$20,
			$21,$22,$23,NOW()
		)
		ON CONFLICT (tenant_id, role_code, meeting_date) DO UPDATE SET
			review_date = EXCLUDED.review_date,
			recording_id = EXCLUDED.recording_id,
			employee_id = EXCLUDED.employee_id,
			employee_name = EXCLUDED.employee_name,
			scene_name = EXCLUDED.scene_name,
			selection_reason = EXCLUDED.selection_reason,
			overall_score = EXCLUDED.overall_score,
			overall_score_text = EXCLUDED.overall_score_text,
			primary_dimension = EXCLUDED.primary_dimension,
			primary_score = EXCLUDED.primary_score,
			primary_score_text = EXCLUDED.primary_score_text,
			evidence = EXCLUDED.evidence,
			diagnosis = EXCLUDED.diagnosis,
			coaching_script = EXCLUDED.coaching_script,
			generation_method = EXCLUDED.generation_method,
			generation_status = EXCLUDED.generation_status,
			fallback_reason = EXCLUDED.fallback_reason,
			prompt_code = EXCLUDED.prompt_code,
			model_code = EXCLUDED.model_code,
			llm_request_id = EXCLUDED.llm_request_id,
			updated_at = NOW()
	`, tenantID, roleCode, meetingDate.Format("2006-01-02"), reviewDate.Format("2006-01-02"), row.RecordingID, row.EmployeeID, row.EmployeeName, row.SceneName, row.SelectionReason, row.OverallScore, row.OverallScoreText, row.PrimaryDimension, row.PrimaryScore, row.PrimaryScoreText, string(evidenceJSON), row.Diagnosis, row.CoachingScript, row.GenerationMethod, row.GenerationStatus, row.FallbackReason, row.PromptCode, row.ModelCode, row.LLMRequestID)
	if err != nil {
		return fmt.Errorf("failed to save morning meeting material: %w", err)
	}
	return nil
}

func (s *Service) generateMorningMeetingContent(ctx context.Context, tenantID int64, roleCode string, candidate morningMeetingCandidate) (string, string, string, string, string, string, string, string) {
	fallbackDiagnosis, fallbackScript := buildMorningMeetingFallback(roleCode, candidate)
	if candidate.Positive || s.llmClient == nil {
		return fallbackDiagnosis, fallbackScript, "rule", "success", "", "", "", ""
	}
	systemPrompt, userPrompt, err := s.loadMorningMeetingPrompt(ctx, tenantID)
	if err != nil || strings.TrimSpace(userPrompt) == "" {
		return fallbackDiagnosis, fallbackScript, "rule", "success", "prompt_unavailable", "", "", ""
	}
	model, err := s.resolveMorningMeetingLLMModelConfig(ctx, tenantID)
	if err != nil {
		return fallbackDiagnosis, fallbackScript, "rule", "success", "model_unavailable", morningMeetingPromptCode, "", ""
	}
	ctxPkg := map[string]interface{}{
		"role_code":         roleCode,
		"employee_name":     candidate.EmployeeName,
		"scene_name":        candidate.SceneName,
		"overall_score":     candidate.OverallScoreText,
		"primary_dimension": candidate.PrimaryDimension,
		"primary_score":     candidate.PrimaryScoreText,
		"selection_reason":  candidate.SelectionReason,
		"evidence":          candidate.Evidence,
		"analysis_summary":  candidate.Narrative,
	}
	pkgJSON, _ := json.MarshalIndent(ctxPkg, "", "  ")
	renderedUserPrompt := strings.ReplaceAll(userPrompt, "{{morning_meeting_context_json}}", string(pkgJSON))
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "recording.morning_meeting",
		FunctionType:  model.FunctionType,
		Provider:      model.Provider,
		ModelCode:     model.ModelCode,
		Billing:       newRecordingBillingMetadata(candidate.RecordingID, "morning_meeting_generation", "morning_meeting"),
		Messages: []llmgateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: renderedUserPrompt},
		},
		Params: &llmgateway.Params{
			Temperature:    0.2,
			MaxTokens:      1000,
			TimeoutSeconds: 45,
			ResponseFormat: "json",
		},
	}
	applyModelParamsToBenchmarkLLMRequest(&req, model.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil || strings.TrimSpace(resp.Content) == "" {
		return fallbackDiagnosis, fallbackScript, "rule", "success", "llm_failed", morningMeetingPromptCode, model.ModelCode, ""
	}
	diagnosis, script, ok := parseMorningMeetingLLMOutput(resp.Content)
	if !ok {
		return fallbackDiagnosis, fallbackScript, "rule", "success", "llm_parse_failed", morningMeetingPromptCode, model.ModelCode, resp.RequestID
	}
	return diagnosis, script, "llm", "success", "", morningMeetingPromptCode, model.ModelCode, resp.RequestID
}

func buildMorningMeetingFallback(roleCode string, candidate morningMeetingCandidate) (string, string) {
	evidence := ""
	if len(candidate.Evidence) > 0 {
		evidence = truncateText(strings.TrimSpace(candidate.Evidence[0]), 60)
	}
	if candidate.Positive {
		diagnosis := fmt.Sprintf("%s在%s维度表现突出，动作清晰，适合作为团队正向示范。", candidate.EmployeeName, candidate.PrimaryDimension)
		script := fmt.Sprintf("这条录音今天不讲问题，讲动作。%s在%s拿到了%s，关键不在说了多少，而在于%s。大家听的时候重点记一句：下次遇到类似场景，怎样把这个动作迁移到自己的对话里。", candidate.EmployeeName, candidate.PrimaryDimension, candidate.PrimaryScoreText, buildDiscoveryInsightSummary(roleCode, candidate.PrimaryDimension, candidate.Evidence, evidence))
		return diagnosis, script
	}
	if evidence == "" {
		diagnosis := fmt.Sprintf("这条录音昨天综合表现偏弱，主要短板出在%s（%s）。当前系统只识别到了评分结果，还没有抓到足够清晰的证据片段，建议结合完整录音人工复核。", candidate.PrimaryDimension, candidate.PrimaryScoreText)
		script := fmt.Sprintf("%s，这条先不急着下结论。系统已经提示%s偏弱，但证据片段还不够完整。今天建议大家先带着问题回听完整录音，重点确认这一步到底卡在没问、没解释，还是没做收口。", candidate.EmployeeName, candidate.PrimaryDimension)
		return diagnosis, script
	}
	diagnosis := fmt.Sprintf("这条录音昨天综合表现偏弱，主要短板出在%s（%s）。%s", candidate.PrimaryDimension, candidate.PrimaryScoreText, buildMorningMeetingEvidenceDiagnosis(roleCode, candidate.PrimaryDimension, evidence))
	script := fmt.Sprintf("%s，这条我今天拿出来不是说你不努力，而是想帮你把最关键的一步补上。你在%s这里目前只有%s，说明这个动作还没形成稳定习惯。下次先只改一件事：%s。先把这一处练顺，再谈后面的展开。", candidate.EmployeeName, candidate.PrimaryDimension, candidate.PrimaryScoreText, buildMorningMeetingActionHint(roleCode, candidate.PrimaryDimension))
	return diagnosis, script
}

func buildMorningMeetingEvidenceDiagnosis(roleCode, dimension, evidence string) string {
	if evidence == "" {
		return "当前表达还没有把关键动作做扎实。"
	}
	role := normalizeMorningMeetingRole(roleCode)
	if role == "consultant" {
		return fmt.Sprintf("从原话“%s”看，沟通停留在信息说明，没有把患者个人情况真正问出来。", evidence)
	}
	if role == "doctor" {
		return fmt.Sprintf("从原话“%s”看，接诊动作覆盖还不完整，患者很难形成清晰的理解与决策。", evidence)
	}
	return fmt.Sprintf("从原话“%s”看，治疗过程中虽有交流，但关键确认动作还不够完整。", evidence)
}

func buildMorningMeetingActionHint(roleCode, dimension string) string {
	role := normalizeMorningMeetingRole(roleCode)
	switch role {
	case "consultant":
		if strings.Contains(dimension, "需求探索") {
			return "先连续问出患者的场景、顾虑和预算，再进入方案说明"
		}
		if strings.Contains(dimension, "问题放大") {
			return "把不处理的后果说具体，并和患者自身场景绑定"
		}
		return "先确认患者真实需求，再讲专业内容"
	case "doctor":
		if strings.Contains(dimension, "理解患者视角") {
			return "先接住患者担心，再给医学解释和建议"
		}
		return "把接诊阶段动作补齐，不要直接跳到给方案"
	default:
		if strings.Contains(dimension, "互动评估") {
			return "每做一个动作都追问一次体感变化，再决定下一步"
		}
		return "操作、解释、确认三步要连起来，不要只做动作不给判断"
	}
}

func (s *Service) resolveMorningMeetingLLMModelConfig(ctx context.Context, tenantID int64) (*benchmarkLLMModelSelection, error) {
	candidates := []string{"recording_morning_meeting_comment", "chat"}
	for _, functionType := range candidates {
		var out benchmarkLLMModelSelection
		err := s.store.pool.QueryRow(ctx, `
			SELECT
				COALESCE(function_type, ''),
				COALESCE(provider, ''),
				COALESCE(model_code, ''),
				COALESCE(model_params, extra_params, '{}'::json)
			FROM llm_model_configs
			WHERE deleted_at IS NULL
			  AND COALESCE(is_active, true) = true
			  AND function_type = $1
			  AND tenant_id IN ($2, 0)
			ORDER BY
			  CASE WHEN tenant_id = $2 THEN 0 ELSE 1 END,
			  CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
			  updated_at DESC,
			  id DESC
			LIMIT 1
		`, functionType, tenantID).Scan(&out.FunctionType, &out.Provider, &out.ModelCode, &out.ModelParams)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, err
		}
		if strings.TrimSpace(out.ModelCode) == "" {
			continue
		}
		if strings.TrimSpace(out.FunctionType) == "" {
			out.FunctionType = functionType
		}
		return &out, nil
	}
	return nil, fmt.Errorf("no llm model config for morning meeting")
}

func (s *Service) loadMorningMeetingPrompt(ctx context.Context, tenantID int64) (string, string, error) {
	var tenantPrompt string
	_ = s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(custom_user_prompt_template, '')
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1
		  AND prompt_code = $2
		  AND COALESCE(is_enabled, true) = true
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, tenantID, morningMeetingPromptCode).Scan(&tenantPrompt)
	basePrompt, err := s.store.GetRecordingPromptByCode(ctx, morningMeetingPromptCode)
	if err != nil {
		return "", "", err
	}
	systemPrompt := strings.TrimSpace(basePrompt.SystemPrompt)
	userPrompt := strings.TrimSpace(basePrompt.PromptText)
	if strings.TrimSpace(tenantPrompt) != "" {
		userPrompt = strings.TrimSpace(tenantPrompt)
	}
	if systemPrompt == "" {
		systemPrompt = "你是一名医疗场景晨会带教主管。"
	}
	return systemPrompt, userPrompt, nil
}

func parseMorningMeetingLLMOutput(text string) (string, string, bool) {
	payload := strings.TrimSpace(text)
	if payload == "" {
		return "", "", false
	}
	var out struct {
		Diagnosis      string `json:"diagnosis"`
		CoachingScript string `json:"coaching_script"`
	}
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return "", "", false
	}
	out.Diagnosis = strings.TrimSpace(out.Diagnosis)
	out.CoachingScript = strings.TrimSpace(out.CoachingScript)
	if out.Diagnosis == "" || out.CoachingScript == "" {
		return "", "", false
	}
	return out.Diagnosis, out.CoachingScript, true
}

type weeklySummaryRow struct {
	ID          int64
	EmployeeID  int64
	Employee    string
	PatientName string
	Analysis    map[string]interface{}
	TS          time.Time
}

type weeklySummaryHighlightEntry struct {
	Dimension string
	Text      string
}

type medicalTrendRow struct {
	Analysis map[string]interface{}
	TS       time.Time
}

// GetWeeklySummary retrieves weekly summary
func (s *Service) GetWeeklySummary(ctx context.Context, tenantID int64, weekOffset int) (*WeeklySummaryResponse, error) {
	now := time.Now()
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -int(now.Weekday())+1)
	if now.Weekday() == time.Sunday {
		monday = monday.AddDate(0, 0, -6)
	}
	weekStart := monday.AddDate(0, 0, weekOffset*7)
	weekEnd := weekStart.AddDate(0, 0, 7)
	prevStart := weekStart.AddDate(0, 0, -7)
	prev2Start := weekStart.AddDate(0, 0, -14)

	rows, err := s.store.pool.Query(ctx, `
		SELECT r.id,
		       r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知医生') AS employee_name,
		       COALESCE(c.name, '') AS patient_name,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		  AND EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'doctor_assistant'])
		  )
		ORDER BY ts DESC
	`, tenantID, prev2Start, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to query weekly summary rows: %w", err)
	}
	defer rows.Close()

	allRows := make([]weeklySummaryRow, 0, 512)
	for rows.Next() {
		var item weeklySummaryRow
		if scanErr := rows.Scan(&item.ID, &item.EmployeeID, &item.Employee, &item.PatientName, &item.Analysis, &item.TS); scanErr != nil {
			return nil, fmt.Errorf("failed to scan weekly summary row: %w", scanErr)
		}
		allRows = append(allRows, item)
	}

	currentRows := make([]weeklySummaryRow, 0, len(allRows))
	prevRows := make([]weeklySummaryRow, 0, len(allRows))
	prev2Rows := make([]weeklySummaryRow, 0, len(allRows))
	for _, row := range allRows {
		switch {
		case !row.TS.Before(weekStart) && row.TS.Before(weekEnd):
			currentRows = append(currentRows, row)
		case !row.TS.Before(prevStart) && row.TS.Before(weekStart):
			prevRows = append(prevRows, row)
		case !row.TS.Before(prev2Start) && row.TS.Before(prevStart):
			prev2Rows = append(prev2Rows, row)
		}
	}

	patientStatusCounter := map[string]int64{}
	blockerCounter := map[string]int64{}
	highlights := make([]WeeklySummaryHighlightItem, 0, 64)
	criticalAttention := make([]WeeklySummaryAttentionItem, 0, 64)
	doctorIDMap := map[string]int64{}

	for _, row := range currentRows {
		doctorIDMap[row.Employee] = row.EmployeeID

		rawStatus := resolveVisitOutcomeStatus(row.Analysis)
		if normalized := normalizePatientStatus(rawStatus); normalized != "" {
			patientStatusCounter[normalized]++
		}

		blockers := extractCoreBlockers(row.Analysis)
		for _, blocker := range blockers {
			blockerCounter[blocker]++
		}

		score := resolveWeeklyDisplayScore(row.Analysis)
		for _, entry := range extractWeeklyHighlightEntries(row.Analysis) {
			recID := row.ID
			item := WeeklySummaryHighlightItem{
				EmployeeName:  row.Employee,
				Type:          "highlight",
				Dimension:     formatWeeklyDimension(entry.Dimension),
				HighlightText: entry.Text,
				RecordingIDs:  []int64{recID},
			}
			if row.EmployeeID > 0 {
				eid := row.EmployeeID
				item.EmployeeID = &eid
			}
			if score > 0 {
				v := roundFloat(score, 1)
				item.Value = &v
			}
			highlights = append(highlights, item)
		}

		if isCritical := pickBool(row.Analysis, "critical_gap"); isCritical != nil && *isCritical {
			summary := strings.TrimSpace(firstNonEmptyText(pickString(row.Analysis, "critical_summary")))
			if summary == "" && len(blockers) > 0 {
				summary = blockers[0]
			}
			if summary == "" {
				summary = "本周出现关键缺口"
			}
			recID := row.ID
			item := WeeklySummaryAttentionItem{
				EmployeeName:    row.Employee,
				Type:            "attention",
				Dimension:       "关键缺口",
				CriticalSummary: summary,
				RecordingIDs:    []int64{recID},
			}
			if row.EmployeeID > 0 {
				eid := row.EmployeeID
				item.EmployeeID = &eid
			}
			criticalAttention = append(criticalAttention, item)
		}
	}

	curAvg := weeklyDoctorScoreAverage(currentRows)
	prevAvg := weeklyDoctorScoreAverage(prevRows)
	prev2Avg := weeklyDoctorScoreAverage(prev2Rows)

	trendAttention := make([]WeeklySummaryAttentionItem, 0, 16)
	for doctor, curVal := range curAvg {
		prevVal, ok1 := prevAvg[doctor]
		prev2Val, ok2 := prev2Avg[doctor]
		if !ok1 || !ok2 {
			continue
		}
		if prev2Val > prevVal && prevVal > curVal {
			item := WeeklySummaryAttentionItem{
				EmployeeName: doctor,
				Type:         "attention",
				Dimension:    "SEGUE 沟通分",
				Trend: []float64{
					roundFloat(prev2Val, 1),
					roundFloat(prevVal, 1),
					roundFloat(curVal, 1),
				},
			}
			if eid := doctorIDMap[doctor]; eid > 0 {
				id := eid
				item.EmployeeID = &id
			}
			trendAttention = append(trendAttention, item)
		}
	}

	sort.Slice(highlights, func(i, j int) bool {
		iv := 0.0
		jv := 0.0
		if highlights[i].Value != nil {
			iv = *highlights[i].Value
		}
		if highlights[j].Value != nil {
			jv = *highlights[j].Value
		}
		return iv > jv
	})
	if len(highlights) > 10 {
		highlights = highlights[:10]
	}

	attention := append(trendAttention, criticalAttention...)
	if len(attention) > 20 {
		attention = attention[:20]
	}

	coreBlockers := make([]WeeklySummaryBlockerItem, 0, len(blockerCounter))
	for blocker, count := range blockerCounter {
		coreBlockers = append(coreBlockers, WeeklySummaryBlockerItem{
			Blocker: blocker,
			Count:   count,
		})
	}
	sort.Slice(coreBlockers, func(i, j int) bool { return coreBlockers[i].Count > coreBlockers[j].Count })
	if len(coreBlockers) > 10 {
		coreBlockers = coreBlockers[:10]
	}

	highlightCandidates, candidateTotal, candidateErr := s.loadWeeklyHighlightCandidates(ctx, tenantID)
	if candidateErr != nil {
		return nil, candidateErr
	}

	top, err := s.GetDoctorAbilityRanking(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	if len(top) > 3 {
		top = top[:3]
	}

	keyInsights := []string{}
	if len(coreBlockers) > 0 {
		keyInsights = append(keyInsights, "主要阻塞："+coreBlockers[0].Blocker)
	}
	if len(attention) > 0 {
		keyInsights = append(keyInsights, "需重点关注人员："+attention[0].EmployeeName)
	}
	if len(keyInsights) == 0 {
		keyInsights = append(keyInsights, "本周整体稳定")
	}

	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}

	return &WeeklySummaryResponse{
		WeekStart:       weekStart.Format("2006-01-02"),
		WeekEnd:         weekEnd.AddDate(0, 0, -1).Format("2006-01-02"),
		TotalRecordings: stats.TotalRecordings,
		AvgScore:        safeRate(stats.CompletedRecordings, stats.TotalRecordings),
		TopPerformers:   top,
		KeyInsights:     keyInsights,
		Week: &WeeklySummaryWeek{
			Start:  weekStart.Format("2006-01-02"),
			End:    weekEnd.AddDate(0, 0, -1).Format("2006-01-02"),
			Offset: weekOffset,
		},
		RecordingCount:           int64(len(currentRows)),
		RecordingCountPrev:       int64(len(prevRows)),
		Attention:                attention,
		Highlights:               highlights,
		PatientStatus:            patientStatusCounter,
		CoreBlockers:             coreBlockers,
		HighlightCandidatesTotal: candidateTotal,
		HighlightCandidates:      highlightCandidates,
	}, nil
}

func normalizePatientStatus(raw string) string {
	text := strings.TrimSpace(raw)
	switch text {
	case "顺利接受", "已接受治疗", "已决定治疗", "已成交", "已预约":
		return "顺利接受"
	case "接受但有疑虑", "倾向治疗", "疗程随访中", "继续观察":
		return "接受但有疑虑"
	case "待作决定", "犹豫中":
		return "待作决定"
	case "倾向拒绝", "倾向不做", "明确拒绝":
		return "倾向拒绝"
	default:
		return ""
	}
}

func resolveVisitOutcomeStatus(analysis map[string]interface{}) string {
	return firstNonEmptyText(
		pickNestedStatus(analysis, "visit_outcome"),
		pickString(analysis, "visit_outcome_status"),
		pickString(analysis, "visit_outcome"),
		pickNestedStatus(pickMap(analysis, "analysis_summary"), "visit_outcome"),
		pickString(pickMap(analysis, "analysis_summary"), "visit_outcome_status"),
		pickString(pickMap(analysis, "analysis_summary"), "visit_outcome"),
		pickString(pickMap(pickMap(analysis, "patient_mindset"), "treatment_willingness"), "status"),
	)
}

func extractCoreBlockers(analysis map[string]interface{}) []string {
	raw, ok := analysis["core_blockers"]
	if !ok {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case map[string]interface{}:
			text := strings.TrimSpace(firstNonEmptyText(v["blocker"], v["name"], v["text"]))
			if text != "" {
				out = append(out, text)
			}
		default:
			text := strings.TrimSpace(firstNonEmptyText(v))
			if text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func extractWeeklyHighlightEntries(analysis map[string]interface{}) []weeklySummaryHighlightEntry {
	readEntries := func(src map[string]interface{}) []weeklySummaryHighlightEntry {
		if src == nil {
			return nil
		}
		candidates := []interface{}{}
		for _, key := range []string{"best_practices", "highlight_candidates", "highlights"} {
			if raw, exists := src[key]; exists {
				if arr, ok := raw.([]interface{}); ok {
					candidates = append(candidates, arr...)
				}
			}
		}
		out := make([]weeklySummaryHighlightEntry, 0, len(candidates))
		for _, candidate := range candidates {
			if text := strings.TrimSpace(firstNonEmptyText(candidate)); text != "" {
				out = append(out, weeklySummaryHighlightEntry{
					Dimension: "",
					Text:      text,
				})
				continue
			}
			m, ok := candidate.(map[string]interface{})
			if !ok {
				continue
			}
			text := strings.TrimSpace(firstNonEmptyText(m["highlight_text"], m["text"], m["summary"], m["detail"]))
			if text == "" {
				continue
			}
			dim := strings.TrimSpace(strings.ToUpper(firstNonEmptyText(m["dimension"], m["stage"], m["category"])))
			out = append(out, weeklySummaryHighlightEntry{
				Dimension: dim,
				Text:      text,
			})
		}
		return out
	}

	result := readEntries(analysis)
	if len(result) > 0 {
		return result
	}
	return readEntries(pickMap(analysis, "analysis_summary"))
}

func formatWeeklyDimension(raw string) string {
	dim := strings.ToUpper(strings.TrimSpace(raw))
	labels := map[string]string{
		"G1": "建立接诊环境",
		"G2": "引出信息",
		"G3": "给予信息",
		"G4": "理解患者视角",
		"G5": "结束接诊",
		"G6": "治疗/预防计划",
	}
	if label, ok := labels[dim]; ok {
		return dim + " " + label
	}
	if dim == "" {
		return "高光片段"
	}
	return dim
}

func resolveWeeklyDisplayScore(analysis map[string]interface{}) float64 {
	candidates := []*float64{
		pickFloat(analysis, "segue_percent"),
		pickFloat(analysis, "segue_score"),
		pickFloat(analysis, "communication_score"),
		pickFloat(pickMap(analysis, "segue"), "overall_score"),
		pickFloat(pickMap(analysis, "segue"), "overall"),
		pickFloat(pickMap(analysis, "analysis_summary"), "segue_percent"),
		pickFloat(pickMap(analysis, "analysis_summary"), "segue_score"),
		pickFloat(pickMap(analysis, "quality_score"), "overall_score"),
	}
	for _, candidate := range candidates {
		if candidate != nil && *candidate > 0 {
			return *candidate
		}
	}
	return 0
}

func weeklyDoctorScoreAverage(rows []weeklySummaryRow) map[string]float64 {
	values := map[string][]float64{}
	for _, row := range rows {
		score := resolveWeeklyDisplayScore(row.Analysis)
		if score <= 0 {
			continue
		}
		values[row.Employee] = append(values[row.Employee], score)
	}
	out := map[string]float64{}
	for doctor, scores := range values {
		var total float64
		for _, score := range scores {
			total += score
		}
		out[doctor] = total / float64(len(scores))
	}
	return out
}

func (s *Service) loadWeeklyHighlightCandidates(ctx context.Context, tenantID int64) ([]WeeklySummaryCandidateItem, int64, error) {
	since := time.Now().AddDate(0, 0, -21)
	rows, err := s.store.pool.Query(ctx, `
		SELECT r.id,
		       COALESCE(r.recorded_at, r.created_at) AS ts,
		       r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '-') AS employee_name,
		       COALESCE(c.name, '') AS patient_name,
		       COALESCE(r.analysis_result, '{}'::json) AS analysis_result
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.tenant_id = $1
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'doctor_assistant'])
		  )
		  AND r.id NOT IN (
		      SELECT recording_id
		      FROM recording_best_practices
		      WHERE tenant_id = $1
		  )
		ORDER BY ts DESC
		LIMIT 100
	`, tenantID, since)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query weekly highlight candidates: %w", err)
	}
	defer rows.Close()

	out := make([]WeeklySummaryCandidateItem, 0, 20)
	for rows.Next() {
		var (
			recordingID int64
			ts          time.Time
			employeeID  int64
			employee    string
			patientName string
			analysis    map[string]interface{}
		)
		if scanErr := rows.Scan(&recordingID, &ts, &employeeID, &employee, &patientName, &analysis); scanErr != nil {
			return nil, 0, fmt.Errorf("failed to scan weekly candidate row: %w", scanErr)
		}
		entries := extractWeeklyHighlightEntries(analysis)
		if len(entries) == 0 {
			continue
		}
		entry := entries[0]
		recordedAt := ts.Format("2006-01-02 15:04:05")
		item := WeeklySummaryCandidateItem{
			RecordingID:   recordingID,
			RecordedAt:    &recordedAt,
			EmployeeName:  employee,
			HighlightText: entry.Text,
		}
		if employeeID > 0 {
			eid := employeeID
			item.EmployeeID = &eid
		}
		if strings.TrimSpace(patientName) != "" {
			name := strings.TrimSpace(patientName)
			item.PatientName = &name
		}
		if strings.TrimSpace(entry.Dimension) != "" {
			dim := strings.ToUpper(strings.TrimSpace(entry.Dimension))
			item.SuggestedDimension = &dim
		}
		out = append(out, item)
		if len(out) >= 20 {
			break
		}
	}
	return out, int64(len(out)), nil
}

// GetTeamTrends retrieves team trends for medical recordings.
func (s *Service) GetTeamTrends(ctx context.Context, tenantID int64, dateFrom, dateTo, specialtyGroup string) (*TeamTrendsResponse, error) {
	now := time.Now()
	start := now.AddDate(0, 0, -29)
	end := now
	if strings.TrimSpace(dateFrom) != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateFrom), now.Location()); err == nil {
			start = parsed
		}
	}
	if strings.TrimSpace(dateTo) != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateTo), now.Location()); err == nil {
			end = parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		}
	}
	if end.Before(start) {
		start, end = end, start
	}

	query := `
		SELECT COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts
		FROM recordings r
		LEFT JOIN recording_route_results rr ON rr.recording_id = r.id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) <= $3
		  AND EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor', 'doctor_assistant'])
		  )
	`
	args := []interface{}{tenantID, start, end}
	if sg := strings.TrimSpace(specialtyGroup); sg != "" {
		query += " AND lower(COALESCE(rr.specialty_group, '')) = lower($4)"
		args = append(args, sg)
	}
	query += " ORDER BY ts ASC"
	rows, err := s.store.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query medical team trends rows: %w", err)
	}
	defer rows.Close()

	allRows := make([]medicalTrendRow, 0, 512)
	for rows.Next() {
		var item medicalTrendRow
		if scanErr := rows.Scan(&item.Analysis, &item.TS); scanErr != nil {
			return nil, fmt.Errorf("failed to scan medical trend row: %w", scanErr)
		}
		allRows = append(allRows, item)
	}

	dateSpanDays := int(end.Sub(start).Hours()/24) + 1
	if dateSpanDays < 1 {
		dateSpanDays = 1
	}
	windowDays := 7
	if dateSpanDays < 14 {
		windowDays = maxInt(1, dateSpanDays/2)
	}
	startWindowEnd := start.AddDate(0, 0, windowDays)
	endWindowStart := end.AddDate(0, 0, -(windowDays - 1))

	startRows := make([]medicalTrendRow, 0, len(allRows))
	endRows := make([]medicalTrendRow, 0, len(allRows))
	for _, row := range allRows {
		if !row.TS.Before(start) && row.TS.Before(startWindowEnd) {
			startRows = append(startRows, row)
		}
		if !row.TS.Before(endWindowStart) && !row.TS.After(end) {
			endRows = append(endRows, row)
		}
	}

	dimOrder := []string{"G1", "G2", "G3", "G4", "G5", "G6"}
	dimLabels := map[string]string{
		"G1": "建立接诊环境",
		"G2": "引出信息",
		"G3": "给予信息",
		"G4": "理解患者视角",
		"G5": "结束接诊",
		"G6": "治疗/预防计划",
	}
	startDim := collectDimensionAvg(startRows)
	endDim := collectDimensionAvg(endRows)
	dimensionTrends := make([]TeamTrendDimension, 0, len(dimOrder))
	for _, group := range dimOrder {
		sv := roundFloat(startDim[group], 1)
		ev := roundFloat(endDim[group], 1)
		dimensionTrends = append(dimensionTrends, TeamTrendDimension{
			Group:  group,
			Label:  dimLabels[group],
			Start:  sv,
			End:    ev,
			Change: roundFloat(ev-sv, 1),
		})
	}

	startAccept, startEmotion := collectPatientTrend(startRows)
	endAccept, endEmotion := collectPatientTrend(endRows)

	blockerCounter := map[string]int64{}
	for _, row := range allRows {
		for _, blocker := range extractCoreBlockers(row.Analysis) {
			blockerCounter[blocker]++
		}
	}
	coreBlockers := make([]TeamTrendBlocker, 0, len(blockerCounter))
	for blocker, count := range blockerCounter {
		coreBlockers = append(coreBlockers, TeamTrendBlocker{
			Blocker: blocker,
			Count:   count,
		})
	}
	sort.Slice(coreBlockers, func(i, j int) bool { return coreBlockers[i].Count > coreBlockers[j].Count })
	if len(coreBlockers) > 10 {
		coreBlockers = coreBlockers[:10]
	}

	return &TeamTrendsResponse{
		Period:          "custom",
		DateFrom:        start.Format("2006-01-02"),
		DateTo:          end.Format("2006-01-02"),
		DimensionTrends: dimensionTrends,
		PatientStatusTrend: TeamTrendPatientStatus{
			StartAcceptanceRate:         startAccept,
			EndAcceptanceRate:           endAccept,
			StartEmotionImprovementRate: startEmotion,
			EndEmotionImprovementRate:   endEmotion,
		},
		CoreBlockers: coreBlockers,
	}, nil
}

func collectDimensionAvg(rows []medicalTrendRow) map[string]float64 {
	values := map[string][]float64{
		"G1": {}, "G2": {}, "G3": {}, "G4": {}, "G5": {}, "G6": {},
	}
	for _, row := range rows {
		stageByName := extractStageScores(row.Analysis)
		for stageName, score := range stageByName {
			if score <= 0 {
				continue
			}
			if code := stageNameToDimensionCode(stageName); code != "" {
				values[code] = append(values[code], score)
			}
		}

		quality := pickMap(row.Analysis, "quality_score")
		stagesAny, ok := quality["stages"]
		if !ok {
			continue
		}
		stages, ok := stagesAny.([]interface{})
		if !ok {
			continue
		}
		for _, item := range stages {
			stage, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name := strings.TrimSpace(firstNonEmptyText(stage["group"], stage["code"], stage["key"], stage["name"]))
			code := toDimensionCode(name)
			if code == "" {
				continue
			}
			score, ok := toFloat(stage["score"])
			if !ok {
				continue
			}
			values[code] = append(values[code], score)
		}
	}
	out := map[string]float64{"G1": 0, "G2": 0, "G3": 0, "G4": 0, "G5": 0, "G6": 0}
	for code, arr := range values {
		if len(arr) == 0 {
			continue
		}
		var sum float64
		for _, v := range arr {
			sum += v
		}
		out[code] = sum / float64(len(arr))
	}
	return out
}

func hasPositiveDimension(stage map[string]float64) bool {
	for _, code := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		if stage[code] > 0 {
			return true
		}
	}
	return false
}

func stageNameToDimensionCode(name string) string {
	switch strings.TrimSpace(name) {
	case "开场建立权威":
		return "G1"
	case "需求探索":
		return "G2"
	case "问题放大":
		return "G3"
	case "专业呈现":
		return "G4"
	case "方案定制":
		return "G5"
	case "异议化解", "促成与收尾":
		return "G6"
	default:
		return ""
	}
}

func toDimensionCode(raw string) string {
	text := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(text, "G1") || strings.Contains(text, "建立接诊环境") || strings.Contains(text, "建立接诊阶段"):
		return "G1"
	case strings.HasPrefix(text, "G2") || strings.Contains(text, "引出信息"):
		return "G2"
	case strings.HasPrefix(text, "G3") || strings.Contains(text, "给予信息"):
		return "G3"
	case strings.HasPrefix(text, "G4") || strings.Contains(text, "理解患者视角") || strings.Contains(text, "同理沟通"):
		return "G4"
	case strings.HasPrefix(text, "G5") || strings.Contains(text, "结束接诊"):
		return "G5"
	case strings.HasPrefix(text, "G6") || strings.Contains(text, "治疗/预防计划") || strings.Contains(text, "治疗预防计划"):
		return "G6"
	default:
		return ""
	}
}

func normalizeDimensionScore(value float64) float64 {
	if value <= 0 {
		return 0
	}
	if value <= 1 {
		return value * 100
	}
	return value
}

func mergeDimensionScoresFromMap(target map[string]float64, source map[string]interface{}) {
	if len(source) == 0 {
		return
	}
	for _, code := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		if v, ok := source[code]; ok {
			if n, ok := toFloat(v); ok && n > 0 {
				target[code] = normalizeDimensionScore(n)
			}
		}
	}
}

func extractDoctorDimensionScores(analysis map[string]interface{}) map[string]float64 {
	out := map[string]float64{
		"G1": 0, "G2": 0, "G3": 0, "G4": 0, "G5": 0, "G6": 0,
	}
	if analysis == nil {
		return out
	}

	// 1) Prefer explicit SEGUE maps when present.
	mergeDimensionScoresFromMap(out, pickMap(analysis, "segue_scores"))
	mergeDimensionScoresFromMap(out, pickMap(pickMap(analysis, "segue"), "group_scores"))
	mergeDimensionScoresFromMap(out, pickMap(pickMap(analysis, "segue_detail"), "group_scores"))

	// 2) Fallback to stage arrays.
	quality := pickMap(analysis, "quality_score")
	if stagesAny, ok := quality["stages"]; ok {
		if stages, ok := stagesAny.([]interface{}); ok {
			for _, item := range stages {
				stage, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				code := toDimensionCode(firstNonEmptyText(stage["group"], stage["code"], stage["key"], stage["name"]))
				if code == "" {
					continue
				}
				if stageScore, ok := toFloat(stage["score"]); ok && stageScore > 0 {
					out[code] = normalizeDimensionScore(stageScore)
				}
			}
		}
	}
	return out
}

func collectPatientTrend(rows []medicalTrendRow) (acceptanceRate float64, emotionImproveRate float64) {
	total := 0
	acceptCount := 0
	emotionTotal := 0
	emotionImprove := 0
	for _, row := range rows {
		statusRaw := resolveVisitOutcomeStatus(row.Analysis)
		if normalized := normalizePatientStatus(statusRaw); normalized != "" {
			total++
			if normalized == "顺利接受" {
				acceptCount++
			}
		}

		if startEmotion, endEmotion, ok := readEmotionPair(row.Analysis); ok {
			emotionTotal++
			if isEmotionImproved(startEmotion, endEmotion) {
				emotionImprove++
			}
		}
	}
	if total > 0 {
		acceptanceRate = roundFloat((float64(acceptCount)/float64(total))*100, 1)
	}
	if emotionTotal > 0 {
		emotionImproveRate = roundFloat((float64(emotionImprove)/float64(emotionTotal))*100, 1)
	}
	return acceptanceRate, emotionImproveRate
}

func readEmotionPair(analysis map[string]interface{}) (string, string, bool) {
	candidates := []map[string]interface{}{
		pickMap(analysis, "patient_mindset"),
		pickMap(pickMap(analysis, "analysis_summary"), "patient_mindset"),
	}
	for _, m := range candidates {
		if m == nil {
			continue
		}
		start := strings.TrimSpace(firstNonEmptyText(m["start_emotion"], m["initial_emotion"], m["before"]))
		end := strings.TrimSpace(firstNonEmptyText(m["end_emotion"], m["final_emotion"], m["after"]))
		if start != "" && end != "" {
			return start, end, true
		}
	}
	return "", "", false
}

func isEmotionImproved(start, end string) bool {
	startText := strings.TrimSpace(start)
	endText := strings.TrimSpace(end)
	negativeSet := map[string]struct{}{"焦虑": {}, "迷茫": {}, "抵触": {}, "恐惧": {}, "担忧": {}}
	positiveSet := map[string]struct{}{"缓解": {}, "稳定": {}, "满意": {}, "放心": {}, "积极": {}}
	startNegative := false
	for k := range negativeSet {
		if strings.Contains(startText, k) {
			startNegative = true
			break
		}
	}
	if !startNegative {
		return false
	}
	for k := range positiveSet {
		if strings.Contains(endText, k) {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Service) GetTeamAbility(ctx context.Context, tenantID int64, months int, scope RecordingScope) (*TeamAbilityResponse, error) {
	if months <= 0 {
		months = 3
	}
	if months > 12 {
		months = 12
	}

	now := time.Now()
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	currentPeriodStart := addMonths(currentMonthStart, -(months - 1))
	currentPeriodEnd := addMonths(currentMonthStart, 1)
	prevPeriodStart := addMonths(currentPeriodStart, -months)
	prevPeriodEnd := currentPeriodStart

	filterClause := `
		  AND COALESCE(NULLIF(r.business_scope, ''), 'unknown') = 'consultant'`
	switch scope {
	case RecordingScopeDoctor:
		filterClause = `
		  AND EXISTS (
			SELECT 1 FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor','doctor_assistant'])
		  )`
	case RecordingScopeFrontdesk:
		filterClause = `
		  AND EXISTS (
			SELECT 1 FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['frontdesk','reception','receptionist'])
		  )`
	case RecordingScopeTherapist:
		filterClause = `
		  AND EXISTS (
			SELECT 1 FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = 'therapist'
		  )`
	case RecordingScopeConsultant:
		filterClause = `
		  AND COALESCE(NULLIF(r.business_scope, ''), 'unknown') = 'consultant'
		  AND EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = 'consultant'
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM institution_employee_roles ier
			WHERE ier.tenant_id = r.tenant_id
			  AND ier.employee_id = r.employee_id
			  AND lower(ier.role_code) = ANY(ARRAY['doctor','doctor_assistant','frontdesk','reception','receptionist','therapist'])
		  )`
	}

	rows, err := s.store.pool.Query(ctx, fmt.Sprintf(`
		SELECT r.employee_id,
		       COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), NULLIF(e.username, ''), NULLIF(e.phone, ''), '未知员工') AS employee_name,
		       r.analysis_result,
		       COALESCE(r.recorded_at, r.created_at) AS ts
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND r.analysis_status = 'completed'
		  AND r.analysis_result IS NOT NULL
		  %s
		  AND COALESCE(r.recorded_at, r.created_at) >= $2
		  AND COALESCE(r.recorded_at, r.created_at) < $3
		ORDER BY ts ASC
	`, filterClause), tenantID, prevPeriodStart, currentPeriodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to query team ability rows: %w", err)
	}
	defer rows.Close()

	allRows := make([]teamAbilityRow, 0, 512)
	for rows.Next() {
		var item teamAbilityRow
		var analysisRaw interface{}
		if scanErr := rows.Scan(&item.EmployeeID, &item.EmployeeName, &analysisRaw, &item.TS); scanErr != nil {
			return nil, fmt.Errorf("failed to scan team ability row: %w", scanErr)
		}
		if decoded, ok := decodeNestedJSONValue(analysisRaw, 0).(map[string]interface{}); ok && decoded != nil {
			item.Analysis = decoded
		} else {
			item.Analysis = map[string]interface{}{}
		}
		allRows = append(allRows, item)
	}

	currentRows := make([]teamAbilityRow, 0, len(allRows))
	previousRows := make([]teamAbilityRow, 0, len(allRows))
	for _, r := range allRows {
		if !r.TS.Before(currentPeriodStart) {
			currentRows = append(currentRows, r)
		} else if !r.TS.Before(prevPeriodStart) && r.TS.Before(prevPeriodEnd) {
			previousRows = append(previousRows, r)
		}
	}

	currentStageAvg := avgStageMap(currentRows)
	previousStageAvg := avgStageMap(previousRows)

	teamRadar := TeamAbilityRadar{
		Current:  make([]TeamAbilityRadarScore, 0, len(stageOrder)),
		Previous: make([]TeamAbilityRadarScore, 0, len(stageOrder)),
		Weakest:  weakestStages(currentStageAvg, 2),
	}
	for _, name := range stageOrder {
		teamRadar.Current = append(teamRadar.Current, TeamAbilityRadarScore{Name: name, Score: currentStageAvg[name]})
		teamRadar.Previous = append(teamRadar.Previous, TeamAbilityRadarScore{Name: name, Score: previousStageAvg[name]})
	}

	employeeGroup := map[int64][]teamAbilityRow{}
	employeeNames := map[int64]string{}
	for _, r := range currentRows {
		employeeGroup[r.EmployeeID] = append(employeeGroup[r.EmployeeID], r)
		employeeNames[r.EmployeeID] = r.EmployeeName
	}
	employeeMatrix := make([]TeamAbilityEmployeeMatrixItem, 0, len(employeeGroup))
	for eid, group := range employeeGroup {
		stageAvg := avgStageMap(group)
		overall := calcOverall(stageAvg)
		employeeMatrix = append(employeeMatrix, TeamAbilityEmployeeMatrixItem{
			EmployeeID:  eid,
			Name:        employeeNames[eid],
			Overall:     overall,
			Stages:      stageAvg,
			SampleCount: int64(len(group)),
		})
	}
	sort.Slice(employeeMatrix, func(i, j int) bool { return employeeMatrix[i].Overall > employeeMatrix[j].Overall })

	growthTrend := make([]TeamAbilityGrowthPoint, 0, months)
	monthPoints := make([]time.Time, 0, months)
	for i := 0; i < months; i++ {
		monthPoints = append(monthPoints, addMonths(currentPeriodStart, i))
	}
	for _, mStart := range monthPoints {
		mEnd := addMonths(mStart, 1)
		monthRows := make([]teamAbilityRow, 0, len(currentRows))
		for _, r := range currentRows {
			if !r.TS.Before(mStart) && r.TS.Before(mEnd) {
				monthRows = append(monthRows, r)
			}
		}
		monthAvg := avgStageMap(monthRows)
		growthTrend = append(growthTrend, TeamAbilityGrowthPoint{
			Month: fmt.Sprintf("%d月", int(mStart.Month())),
			Score: calcOverall(monthAvg),
		})
	}

	prevByEmployee := map[int64][]teamAbilityRow{}
	for _, r := range previousRows {
		prevByEmployee[r.EmployeeID] = append(prevByEmployee[r.EmployeeID], r)
	}

	fastestGrowth := TeamAbilityFastestGrowth{Name: "-", Change: 0}
	mostStable := TeamAbilityMostStable{Name: "-", Variance: 0}

	growthCandidates := make([]TeamAbilityFastestGrowth, 0, len(employeeMatrix))
	stableCandidates := make([]TeamAbilityMostStable, 0, len(employeeMatrix))
	for _, m := range employeeMatrix {
		prevOverall := calcOverall(avgStageMap(prevByEmployee[m.EmployeeID]))
		growthCandidates = append(growthCandidates, TeamAbilityFastestGrowth{
			Name:   m.Name,
			Change: roundFloat(m.Overall-prevOverall, 2),
		})

		monthlyScores := make([]float64, 0, months)
		for _, mStart := range monthPoints {
			mEnd := addMonths(mStart, 1)
			empRows := make([]teamAbilityRow, 0, 32)
			for _, r := range currentRows {
				if r.EmployeeID == m.EmployeeID && !r.TS.Before(mStart) && r.TS.Before(mEnd) {
					empRows = append(empRows, r)
				}
			}
			if len(empRows) == 0 {
				continue
			}
			monthlyScores = append(monthlyScores, calcOverall(avgStageMap(empRows)))
		}
		if len(monthlyScores) > 0 {
			stableCandidates = append(stableCandidates, TeamAbilityMostStable{
				Name:     m.Name,
				Variance: variance(monthlyScores),
			})
		}
	}
	if len(growthCandidates) > 0 {
		sort.Slice(growthCandidates, func(i, j int) bool { return growthCandidates[i].Change > growthCandidates[j].Change })
		fastestGrowth = growthCandidates[0]
	}
	if len(stableCandidates) > 0 {
		sort.Slice(stableCandidates, func(i, j int) bool { return stableCandidates[i].Variance < stableCandidates[j].Variance })
		mostStable = stableCandidates[0]
	}

	return &TeamAbilityResponse{
		TeamRadar:      teamRadar,
		EmployeeMatrix: employeeMatrix,
		GrowthTrend:    growthTrend,
		Highlights: TeamAbilityHighlights{
			FastestGrowth: fastestGrowth,
			MostStable:    mostStable,
		},
	}, nil
}

var stageWeights = map[string]float64{
	"开场建立权威": 0.10,
	"需求探索":   0.20,
	"问题放大":   0.15,
	"专业呈现":   0.15,
	"方案定制":   0.10,
	"异议化解":   0.20,
	"促成与收尾":  0.10,
}

var stageOrder = []string{"开场建立权威", "需求探索", "问题放大", "专业呈现", "方案定制", "异议化解", "促成与收尾"}

type teamAbilityRow struct {
	EmployeeID   int64
	EmployeeName string
	Analysis     map[string]interface{}
	TS           time.Time
}

func extractStageScores(analysisResult map[string]interface{}) map[string]float64 {
	out := map[string]float64{}
	quality := pickMap(analysisResult, "quality_score")
	stagesAny, ok := quality["stages"]
	if !ok {
		return out
	}
	stages, ok := stagesAny.([]interface{})
	if !ok {
		return out
	}
	for _, item := range stages {
		stage, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprintf("%v", stage["name"]))
		if _, exists := stageWeights[name]; !exists {
			continue
		}
		score, ok := toFloat(stage["score"])
		if !ok {
			continue
		}
		out[name] = score
	}
	return out
}

func avgStageMap(rows []teamAbilityRow) map[string]float64 {
	stageValues := map[string][]float64{}
	for _, name := range stageOrder {
		stageValues[name] = []float64{}
	}
	for _, row := range rows {
		stageScores := extractStageScores(row.Analysis)
		for name, score := range stageScores {
			stageValues[name] = append(stageValues[name], score)
		}
	}
	out := map[string]float64{}
	for _, name := range stageOrder {
		values := stageValues[name]
		if len(values) == 0 {
			out[name] = 0
			continue
		}
		var sum float64
		for _, v := range values {
			sum += v
		}
		out[name] = roundFloat(sum/float64(len(values)), 2)
	}
	return out
}

func calcOverall(stageAvg map[string]float64) float64 {
	var weightedSum float64
	var weightTotal float64
	for _, name := range stageOrder {
		score := stageAvg[name]
		if score <= 0 {
			continue
		}
		w := stageWeights[name]
		weightedSum += score * w
		weightTotal += w
	}
	if weightTotal <= 0 {
		return 0
	}
	return roundFloat(weightedSum/weightTotal, 2)
}

func weakestStages(stageAvg map[string]float64, n int) []string {
	type pair struct {
		Name  string
		Score float64
	}
	items := make([]pair, 0, len(stageOrder))
	for _, name := range stageOrder {
		items = append(items, pair{Name: name, Score: stageAvg[name]})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Score < items[j].Score })
	if n > len(items) {
		n = len(items)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, items[i].Name)
	}
	return out
}

func addMonths(dt time.Time, months int) time.Time {
	shifted := dt.AddDate(0, months, 0)
	return time.Date(shifted.Year(), shifted.Month(), 1, 0, 0, 0, 0, dt.Location())
}

func variance(values []float64) float64 {
	if len(values) == 0 {
		return 999
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var sq float64
	for _, v := range values {
		d := v - mean
		sq += d * d
	}
	return roundFloat(sq/float64(len(values)), 4)
}

// GetDailyReport retrieves daily report
func (s *Service) GetDailyReport(ctx context.Context, tenantID int64, dateFrom, dateTo, date string) (*DailyReportResponse, error) {
	rangeClause, rangeArgs := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "")
	rangeClauseR, rangeArgsR := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "r")
	args := append([]interface{}{tenantID}, rangeArgs...)

	reportDate := strings.TrimSpace(date)
	if reportDate == "" {
		reportDate = time.Now().Format("2006-01-02")
	}
	if strings.TrimSpace(dateTo) != "" {
		reportDate = strings.TrimSpace(dateTo)
	}

	var (
		confirmedCount  int64
		dealCount       int64
		totalAmount     float64
		totalRecordings int64
		activeEmployees int64
	)
	overviewQuery := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE confirmed_deal_status IS NOT NULL) AS confirmed_count,
			COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了') AS deal_count,
			COALESCE(SUM(converted_amount) FILTER (WHERE confirmed_deal_status = '成交了'), 0) AS total_amount,
			COUNT(*) AS total_recordings,
			COUNT(DISTINCT employee_id) AS active_employees
		FROM recordings
		WHERE tenant_id = $1 %s
	`, rangeClause)
	if err := s.store.pool.QueryRow(ctx, overviewQuery, args...).Scan(
		&confirmedCount, &dealCount, &totalAmount, &totalRecordings, &activeEmployees,
	); err != nil {
		return nil, fmt.Errorf("failed to query dashboard overview: %w", err)
	}

	dealRate := safeRate(dealCount, confirmedCount)
	avgDealAmount := 0.0
	if dealCount > 0 {
		avgDealAmount = roundFloat(totalAmount/float64(dealCount), 2)
	}

	// 跟进回收率（30天）
	recoveryQuery := fmt.Sprintf(`
		WITH follow_customers AS (
			SELECT DISTINCT r.customer_id, MIN(rt.created_at) AS first_task_at
			FROM recordings r
			JOIN recording_tasks rt ON rt.recording_id = r.id
			WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
			  AND r.customer_id IS NOT NULL
			  AND r.tenant_id = $1 %s
			GROUP BY r.customer_id
		),
		recovered_customers AS (
			SELECT DISTINCT fc.customer_id
			FROM follow_customers fc
			JOIN recordings r2 ON r2.customer_id = fc.customer_id
			WHERE r2.confirmed_deal_status = '成交了'
			  AND COALESCE(r2.recorded_at, r2.created_at)
			      BETWEEN fc.first_task_at AND fc.first_task_at + INTERVAL '30 days'
		)
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE rc.customer_id IS NOT NULL) AS recovered
		FROM follow_customers fc
		LEFT JOIN recovered_customers rc ON rc.customer_id = fc.customer_id
	`, rangeClauseR)
	var followTotal int64
	var followRecovered int64
	if err := s.store.pool.QueryRow(ctx, recoveryQuery, append([]interface{}{tenantID}, rangeArgsR...)...).Scan(&followTotal, &followRecovered); err != nil {
		return nil, fmt.Errorf("failed to query dashboard recovery: %w", err)
	}
	var recoveryRate *float64
	if followTotal > 0 {
		v := roundFloat(float64(followRecovered)/float64(followTotal)*100, 1)
		recoveryRate = &v
	}

	// 跟进管线（当前）
	var followingCount, active7dCount, overdueCount int64
	pipelineQuery := `
		WITH task_latest AS (
			SELECT recording_id, MAX(updated_at) AS last_task_action_at
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		),
		follow_pipeline AS (
			SELECT r.customer_id,
			       MAX(COALESCE(r.recorded_at, r.created_at)) AS last_recording_at,
			       MAX(tl.last_task_action_at) AS last_task_action_at
			FROM recordings r
			JOIN task_latest tl ON tl.recording_id = r.id
			WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
			  AND r.customer_id IS NOT NULL
			  AND r.tenant_id = $1
			GROUP BY r.customer_id
		)
		SELECT
			COUNT(*) AS following_count,
			COUNT(*) FILTER (
				WHERE GREATEST(last_recording_at, last_task_action_at) >= NOW() - INTERVAL '7 days'
			) AS active_7d_count,
			COUNT(*) FILTER (
				WHERE GREATEST(last_recording_at, last_task_action_at) < NOW() - INTERVAL '7 days'
			) AS overdue_count
		FROM follow_pipeline
	`
	if err := s.store.pool.QueryRow(ctx, pipelineQuery, tenantID).Scan(&followingCount, &active7dCount, &overdueCount); err != nil {
		return nil, fmt.Errorf("failed to query dashboard pipeline: %w", err)
	}

	// 月度目标
	var targetNullable *float64
	targetQuery := `SELECT monthly_revenue_target::float8 FROM tenants WHERE id = $1`
	if err := s.store.pool.QueryRow(ctx, targetQuery, tenantID).Scan(&targetNullable); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to query dashboard target: %w", err)
	}
	var targetProgress *float64
	if targetNullable != nil && *targetNullable > 0 {
		v := roundFloat(totalAmount/(*targetNullable)*100, 1)
		targetProgress = &v
	}

	// 环比
	var momAmount, momDealRate, momAvgAmount *float64
	if p := buildPrevRange(dateFrom, dateTo); p != nil {
		prevQuery := `
			SELECT
				COUNT(*) FILTER (WHERE confirmed_deal_status IS NOT NULL) AS confirmed,
				COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了') AS deals,
				COALESCE(SUM(converted_amount) FILTER (WHERE confirmed_deal_status = '成交了'), 0)::float8 AS amount
			FROM recordings
			WHERE tenant_id = $1
			  AND COALESCE(recorded_at, created_at) >= $2
			  AND COALESCE(recorded_at, created_at) < $3
		`
		var pvConfirmed, pvDeals int64
		var pvAmount float64
		if err := s.store.pool.QueryRow(ctx, prevQuery, tenantID, p.From, p.ToEx).Scan(&pvConfirmed, &pvDeals, &pvAmount); err != nil {
			return nil, fmt.Errorf("failed to query dashboard mom: %w", err)
		}
		pvRate := safeRate(pvDeals, pvConfirmed)
		pvAvg := 0.0
		if pvDeals > 0 {
			pvAvg = roundFloat(pvAmount/float64(pvDeals), 2)
		}
		a := roundFloat(totalAmount-pvAmount, 2)
		r := roundFloat(dealRate-pvRate, 1)
		av := roundFloat(avgDealAmount-pvAvg, 2)
		momAmount = &a
		momDealRate = &r
		momAvgAmount = &av
	}

	// 员工排行
	empQuery := fmt.Sprintf(`
		WITH task_latest AS (
			SELECT recording_id
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		)
		SELECT
			e.id AS eid,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工') AS name,
			COUNT(r.id) FILTER (WHERE r.confirmed_deal_status IS NOT NULL) AS consultations,
			COUNT(r.id) FILTER (WHERE r.confirmed_deal_status = '成交了') AS deals,
			COALESCE(SUM(r.converted_amount) FILTER (WHERE r.confirmed_deal_status = '成交了'), 0)::float8 AS amount,
			COUNT(DISTINCT r.customer_id) FILTER (
				WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
				  AND r.customer_id IS NOT NULL
				  AND tl.recording_id IS NOT NULL
			) AS following_count
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		LEFT JOIN task_latest tl ON tl.recording_id = r.id
		WHERE r.tenant_id = $1 %s
		GROUP BY e.id, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工')
		ORDER BY amount DESC
		LIMIT 20
	`, rangeClauseR)
	empRows, err := s.store.pool.Query(ctx, empQuery, append([]interface{}{tenantID}, rangeArgsR...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to query dashboard employee ranking: %w", err)
	}
	defer empRows.Close()

	overdueByEmployee := map[int64]int64{}
	overdueQuery := `
		WITH task_latest AS (
			SELECT recording_id, MAX(updated_at) AS last_task_action_at
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		),
		emp_customer_last AS (
			SELECT r.employee_id, r.customer_id,
			       GREATEST(MAX(COALESCE(r.recorded_at, r.created_at)), MAX(tl.last_task_action_at)) AS last_action_at
			FROM recordings r
			JOIN task_latest tl ON tl.recording_id = r.id
			WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
			  AND r.customer_id IS NOT NULL
			  AND r.tenant_id = $1
			GROUP BY r.employee_id, r.customer_id
		)
		SELECT employee_id, COUNT(*) AS overdue_count
		FROM emp_customer_last
		WHERE last_action_at < NOW() - INTERVAL '7 days'
		GROUP BY employee_id
	`
	overdueRows, err := s.store.pool.Query(ctx, overdueQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query dashboard overdue by employee: %w", err)
	}
	for overdueRows.Next() {
		var eid, cnt int64
		if scanErr := overdueRows.Scan(&eid, &cnt); scanErr != nil {
			overdueRows.Close()
			return nil, fmt.Errorf("failed to scan dashboard overdue by employee: %w", scanErr)
		}
		overdueByEmployee[eid] = cnt
	}
	overdueRows.Close()

	abilityMap, err := s.calcEmployeeAbilityScores(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	employeeRanking := make([]DashboardEmployeeRankingItem, 0, 20)
	topPerformers := make([]DoctorAbilityRankingResponse, 0, 5)
	rank := 1
	for empRows.Next() {
		var (
			eid           int64
			name          string
			consultations int64
			deals         int64
			amount        float64
			following     int64
		)
		if scanErr := empRows.Scan(&eid, &name, &consultations, &deals, &amount, &following); scanErr != nil {
			return nil, fmt.Errorf("failed to scan dashboard employee ranking: %w", scanErr)
		}
		ability := abilityMap[eid]
		item := DashboardEmployeeRankingItem{
			EmployeeID:         eid,
			Name:               name,
			DealAmount:         amount,
			DealRate:           safeRate(deals, consultations),
			Consultations:      consultations,
			AvgDealAmount:      0,
			FollowingCount:     following,
			OverdueCount:       overdueByEmployee[eid],
			AbilityScore:       ability.Score,
			AbilitySampleCount: ability.SampleCount,
		}
		if deals > 0 {
			item.AvgDealAmount = roundFloat(amount/float64(deals), 2)
		}
		employeeRanking = append(employeeRanking, item)
		if len(topPerformers) < 5 {
			topPerformers = append(topPerformers, DoctorAbilityRankingResponse{
				EmployeeID:     eid,
				EmployeeName:   name,
				RecordingCount: consultations,
				AvgScore:       item.DealRate,
				Rank:           rank,
			})
		}
		rank++
	}

	// 趋势
	trendQuery := fmt.Sprintf(`
		SELECT
			DATE(COALESCE(recorded_at, created_at)) AS day,
			COALESCE(SUM(converted_amount) FILTER (WHERE confirmed_deal_status = '成交了'), 0)::float8 AS amount,
			COUNT(*) FILTER (WHERE confirmed_deal_status IS NOT NULL) AS total,
			COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了') AS deals
		FROM recordings
		WHERE tenant_id = $1 %s
		GROUP BY day
		ORDER BY day
	`, rangeClause)
	trendRows, err := s.store.pool.Query(ctx, trendQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query dashboard trend: %w", err)
	}
	amountTrend := make([]DashboardAmountTrendItem, 0, 32)
	for trendRows.Next() {
		var day time.Time
		var amount float64
		var total int64
		var deals int64
		if scanErr := trendRows.Scan(&day, &amount, &total, &deals); scanErr != nil {
			trendRows.Close()
			return nil, fmt.Errorf("failed to scan dashboard trend: %w", scanErr)
		}
		amountTrend = append(amountTrend, DashboardAmountTrendItem{
			Date:     day.Format("2006-01-02"),
			Amount:   amount,
			DealRate: safeRate(deals, total),
		})
	}
	trendRows.Close()

	var insight *string
	if len(employeeRanking) > 0 {
		s := fmt.Sprintf("本期录音 %d 条，成交率 %.1f%%，建议优先关注低转化员工与超期跟进客户。", totalRecordings, dealRate)
		insight = &s
	}

	resp := &DailyReportResponse{
		Date:            reportDate,
		TotalRecordings: totalRecordings,
		TotalDuration:   0,
		AvgScore:        dealRate,
		TopPerformers:   topPerformers,
		KeyMetrics: JSONObject{
			"pending_tasks":            followingCount,
			"failed_tasks":             overdueCount,
			"deal_count":               dealCount,
			"total_amount":             totalAmount,
			"deal_rate":                dealRate,
			"avg_deal_amount":          avgDealAmount,
			"confirmed_count":          confirmedCount,
			"active_employees":         activeEmployees,
			"following_count":          followingCount,
			"active_7d_count":          active7dCount,
			"overdue_count":            overdueCount,
			"historical_recovery_rate": recoveryRate,
			"target":                   targetNullable,
		},
		Revenue: &DashboardRevenue{
			TotalAmount:     totalAmount,
			DealCount:       dealCount,
			DealRate:        dealRate,
			AvgDealAmount:   avgDealAmount,
			ConfirmedCount:  confirmedCount,
			TotalRecordings: totalRecordings,
			ActiveEmployees: activeEmployees,
			Target:          targetNullable,
			TargetProgress:  targetProgress,
			MomAmount:       momAmount,
			MomDealRate:     momDealRate,
			MomAvgAmount:    momAvgAmount,
		},
		Pipeline: &DashboardPipeline{
			FollowingCount:         followingCount,
			Active7DCount:          active7dCount,
			OverdueCount:           overdueCount,
			AvgDealAmount:          avgDealAmount,
			HistoricalRecoveryRate: recoveryRate,
		},
		EmployeeRanking: employeeRanking,
		AmountTrend:     amountTrend,
		Insight:         insight,
	}
	return resp, nil
}

// GetDiagnosis retrieves diagnosis
func (s *Service) GetDiagnosis(ctx context.Context, tenantID int64, dateFrom, dateTo string) (*DiagnosisResponse, error) {
	rangeClause, rangeArgs := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "")
	rangeClauseR, rangeArgsR := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "r")
	args := append([]interface{}{tenantID}, rangeArgs...)

	var recCount, analyzed int64
	qualityQuery := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE analysis_status = 'completed') AS analyzed
		FROM recordings
		WHERE tenant_id = $1 %s
	`, rangeClause)
	if err := s.store.pool.QueryRow(ctx, qualityQuery, args...).Scan(&recCount, &analyzed); err != nil {
		return nil, fmt.Errorf("failed to query diagnosis quality: %w", err)
	}
	analysisRate := safeRate(analyzed, recCount)

	var fc, fd, fn int64
	var dealAmount float64
	funnelQuery := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE confirmed_deal_status IS NOT NULL) AS confirmed,
			COUNT(*) FILTER (WHERE confirmed_deal_status = '成交了') AS deal_count,
			COALESCE(SUM(converted_amount) FILTER (WHERE confirmed_deal_status = '成交了'), 0)::float8 AS deal_amount,
			COUNT(*) FILTER (WHERE confirmed_deal_status IN ('没成交','还在跟进')) AS not_closed
		FROM recordings r
		WHERE r.tenant_id = $1 AND r.confirmed_deal_status IS NOT NULL %s
	`, rangeClauseR)
	if err := s.store.pool.QueryRow(ctx, funnelQuery, append([]interface{}{tenantID}, rangeArgsR...)...).Scan(&fc, &fd, &dealAmount, &fn); err != nil {
		return nil, fmt.Errorf("failed to query diagnosis funnel: %w", err)
	}

	var followingCount int64
	followingQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT r.customer_id) AS following_count
		FROM recordings r
		JOIN recording_tasks rt ON rt.recording_id = r.id
		WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
		  AND r.customer_id IS NOT NULL
		  AND r.tenant_id = $1 %s
	`, rangeClauseR)
	if err := s.store.pool.QueryRow(ctx, followingQuery, append([]interface{}{tenantID}, rangeArgsR...)...).Scan(&followingCount); err != nil {
		return nil, fmt.Errorf("failed to query diagnosis following count: %w", err)
	}

	rate := func(n, d int64) *float64 {
		if d <= 0 {
			return nil
		}
		v := roundFloat(float64(n)/float64(d)*100, 1)
		return &v
	}
	dealAmountCopy := dealAmount
	funnel := []DashboardFunnelStage{
		{Key: "confirmed", Label: "已确认咨询", Count: fc},
		{Key: "deal", Label: "成交", Count: fd, Amount: &dealAmountCopy, Rate: rate(fd, fc)},
		{Key: "not_closed", Label: "未成交", Count: fn, Rate: rate(fn, fc)},
		{Key: "following", Label: "正在跟进", Count: followingCount, Rate: rate(followingCount, fn)},
	}

	concernQuery := fmt.Sprintf(`
		SELECT COALESCE(not_closed_reason, ai_not_closed_reason) AS reason, COUNT(*) AS cnt
		FROM recordings
		WHERE tenant_id = $1
		  AND COALESCE(not_closed_reason, ai_not_closed_reason) IS NOT NULL
		  AND confirmed_deal_status IN ('没成交','还在跟进') %s
		GROUP BY COALESCE(not_closed_reason, ai_not_closed_reason)
		ORDER BY cnt DESC
	`, rangeClause)
	concernRows, err := s.store.pool.Query(ctx, concernQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query diagnosis concern distribution: %w", err)
	}
	labelMap := map[string]string{
		"price": "价格顾虑", "need_think": "还要考虑", "family": "家人意见",
		"trust": "信任不足", "plan_mismatch": "方案不满意", "timing": "时间安排",
		"competitor": "在比较", "other": "其他",
	}
	type concernTmp struct {
		reason string
		cnt    int64
	}
	tmpConcerns := make([]concernTmp, 0, 16)
	var totalConcern int64
	for concernRows.Next() {
		var reason string
		var cnt int64
		if scanErr := concernRows.Scan(&reason, &cnt); scanErr != nil {
			concernRows.Close()
			return nil, fmt.Errorf("failed to scan concern distribution: %w", scanErr)
		}
		tmpConcerns = append(tmpConcerns, concernTmp{reason: reason, cnt: cnt})
		totalConcern += cnt
	}
	concernRows.Close()
	concernDist := make([]DashboardConcernDistribution, 0, len(tmpConcerns))
	issues := make([]IssueCount, 0, len(tmpConcerns))
	for _, c := range tmpConcerns {
		pct := 0.0
		if totalConcern > 0 {
			pct = roundFloat(float64(c.cnt)/float64(totalConcern)*100, 1)
		}
		label := c.reason
		if mapped, ok := labelMap[c.reason]; ok {
			label = mapped
		}
		concernDist = append(concernDist, DashboardConcernDistribution{
			Reason:     c.reason,
			Label:      label,
			Count:      c.cnt,
			Percentage: pct,
		})
		issues = append(issues, IssueCount{Issue: label, Count: c.cnt})
	}

	empQuery := fmt.Sprintf(`
		WITH task_latest AS (
			SELECT recording_id
			FROM recording_tasks
			WHERE tenant_id = $1
			GROUP BY recording_id
		)
		SELECT
			e.id AS eid,
			COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工') AS name,
			COUNT(r.id) FILTER (WHERE r.confirmed_deal_status IS NOT NULL) AS consultations,
			COUNT(r.id) FILTER (WHERE r.confirmed_deal_status = '成交了') AS deal_count,
			COALESCE(SUM(r.converted_amount) FILTER (WHERE r.confirmed_deal_status = '成交了'), 0)::float8 AS deal_amount,
			COUNT(DISTINCT r.customer_id) FILTER (
				WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
				  AND r.customer_id IS NOT NULL
				  AND tl.recording_id IS NOT NULL
			) AS following_count
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		LEFT JOIN task_latest tl ON tl.recording_id = r.id
		WHERE r.tenant_id = $1 %s
		GROUP BY e.id, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), '未知员工')
		ORDER BY deal_amount DESC
		LIMIT 20
	`, rangeClauseR)
	empRows, err := s.store.pool.Query(ctx, empQuery, append([]interface{}{tenantID}, rangeArgsR...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to query diagnosis employee details: %w", err)
	}
	defer empRows.Close()

	overdueByEmployee := map[int64]int64{}
	overdueQuery := `
		SELECT employee_id, COUNT(*) AS overdue_count
		FROM (
			SELECT r.employee_id, r.customer_id
			FROM recordings r
			JOIN recording_tasks rt ON rt.recording_id = r.id
			WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
			  AND r.customer_id IS NOT NULL
			  AND r.tenant_id = $1
			GROUP BY r.employee_id, r.customer_id
			HAVING GREATEST(MAX(COALESCE(r.recorded_at, r.created_at)), MAX(rt.updated_at)) < NOW() - INTERVAL '7 days'
		) sub
		GROUP BY employee_id
	`
	overdueRows, err := s.store.pool.Query(ctx, overdueQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query diagnosis overdue employee: %w", err)
	}
	for overdueRows.Next() {
		var eid, cnt int64
		if scanErr := overdueRows.Scan(&eid, &cnt); scanErr != nil {
			overdueRows.Close()
			return nil, fmt.Errorf("failed to scan diagnosis overdue employee: %w", scanErr)
		}
		overdueByEmployee[eid] = cnt
	}
	overdueRows.Close()

	employeeDiagnosis := make([]DashboardEmployeeDiagnosisRow, 0, 20)
	for empRows.Next() {
		var eid int64
		var name string
		var consultations, dealCount, following int64
		var dealAmountRow float64
		if scanErr := empRows.Scan(&eid, &name, &consultations, &dealCount, &dealAmountRow, &following); scanErr != nil {
			return nil, fmt.Errorf("failed to scan diagnosis employee detail: %w", scanErr)
		}
		avg := 0.0
		if dealCount > 0 {
			avg = roundFloat(dealAmountRow/float64(dealCount), 2)
		}
		employeeDiagnosis = append(employeeDiagnosis, DashboardEmployeeDiagnosisRow{
			EmployeeID:     eid,
			Name:           name,
			Consultations:  consultations,
			DealCount:      dealCount,
			DealRate:       safeRate(dealCount, consultations),
			DealAmount:     dealAmountRow,
			AvgDealAmount:  avg,
			FollowingCount: following,
			OverdueCount:   overdueByEmployee[eid],
		})
	}

	// 兼容旧字段
	health := "poor"
	switch {
	case analysisRate >= 90:
		health = "excellent"
	case analysisRate >= 75:
		health = "good"
	case analysisRate >= 60:
		health = "fair"
	}
	trends := make([]TrendDataPoint, 0, 30)
	for _, item := range employeeDiagnosis {
		_ = item
	}
	trendRows, err := s.store.pool.Query(ctx, `
		SELECT DATE(COALESCE(created_at, recorded_at)) AS dt,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE analysis_status='completed') AS completed
		FROM recordings
		WHERE tenant_id = $1
		  AND COALESCE(created_at, recorded_at) >= NOW() - INTERVAL '30 days'
		GROUP BY dt
		ORDER BY dt ASC
	`, tenantID)
	if err == nil {
		for trendRows.Next() {
			var dt time.Time
			var total, completed int64
			if scanErr := trendRows.Scan(&dt, &total, &completed); scanErr == nil {
				trends = append(trends, TrendDataPoint{
					Date:           dt.Format("2006-01-02"),
					AvgScore:       safeRate(completed, total),
					RecordingCount: total,
				})
			}
		}
		trendRows.Close()
	}

	return &DiagnosisResponse{
		OverallHealth:   health,
		Issues:          issues,
		Recommendations: []string{"优先处理超7天未跟进客户", "重点复盘未成交原因Top项"},
		Trends:          trends,
		DataQuality: &DashboardDataQuality{
			RecordingCount:      recCount,
			AnalysisSuccessRate: analysisRate,
		},
		Funnel:          funnel,
		ConcernDist:     concernDist,
		EmployeeDetails: employeeDiagnosis,
	}, nil
}

func (s *Service) GetDashboardFunnelDetail(ctx context.Context, tenantID int64, dateFrom, dateTo, stageKey string) (*DashboardFunnelDetailResponse, error) {
	stageLabelMap := map[string]string{
		"deal":       "成交",
		"not_closed": "未成交",
		"following":  "正在跟进",
	}
	stageConditionMap := map[string]string{
		"deal":       "r.confirmed_deal_status = '成交了'",
		"not_closed": "r.confirmed_deal_status IN ('没成交','还在跟进')",
		"following":  "r.confirmed_deal_status IN ('没成交','还在跟进') AND EXISTS (SELECT 1 FROM recording_tasks rt WHERE rt.recording_id = r.id)",
	}
	cond, ok := stageConditionMap[stageKey]
	if !ok {
		return nil, fmt.Errorf("invalid stage_key")
	}

	rangeClauseR, rangeArgsR := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "r")
	query := fmt.Sprintf(`
		SELECT e.id AS eid, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name,''), '未知员工') AS name, COUNT(DISTINCT r.id) AS cnt
		FROM recordings r
		JOIN employees e ON e.id = r.employee_id
		WHERE r.tenant_id = $1
		  AND %s %s
		GROUP BY e.id, COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name,''), '未知员工')
		ORDER BY cnt DESC
	`, cond, rangeClauseR)
	rows, err := s.store.pool.Query(ctx, query, append([]interface{}{tenantID}, rangeArgsR...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to query funnel detail: %w", err)
	}
	defer rows.Close()

	overdueByEmployee := map[int64]int64{}
	if stageKey == "following" {
		rangeClause, rangeArgs := buildDashboardDateRangeSQL(dateFrom, dateTo, 2, "r")
		overdueQuery := fmt.Sprintf(`
			SELECT employee_id, COUNT(*) AS overdue_count
			FROM (
				SELECT r.employee_id, r.customer_id
				FROM recordings r
				JOIN recording_tasks rt ON rt.recording_id = r.id
				WHERE r.confirmed_deal_status IN ('没成交', '还在跟进')
				  AND r.customer_id IS NOT NULL
				  AND r.tenant_id = $1 %s
				GROUP BY r.employee_id, r.customer_id
				HAVING GREATEST(MAX(COALESCE(r.recorded_at, r.created_at)), MAX(rt.updated_at)) < NOW() - INTERVAL '7 days'
			) sub
			GROUP BY employee_id
		`, rangeClause)
		overdueRows, qerr := s.store.pool.Query(ctx, overdueQuery, append([]interface{}{tenantID}, rangeArgs...)...)
		if qerr == nil {
			for overdueRows.Next() {
				var eid, cnt int64
				if scanErr := overdueRows.Scan(&eid, &cnt); scanErr == nil {
					overdueByEmployee[eid] = cnt
				}
			}
			overdueRows.Close()
		}
	}

	byEmployee := make([]DashboardFunnelDetailByEmployee, 0, 32)
	var totalCount int64
	for rows.Next() {
		var eid int64
		var name string
		var cnt int64
		if scanErr := rows.Scan(&eid, &name, &cnt); scanErr != nil {
			return nil, fmt.Errorf("failed to scan funnel detail: %w", scanErr)
		}
		byEmployee = append(byEmployee, DashboardFunnelDetailByEmployee{
			EmployeeID:   eid,
			Name:         name,
			Count:        cnt,
			OverdueCount: overdueByEmployee[eid],
		})
		totalCount += cnt
	}

	return &DashboardFunnelDetailResponse{
		StageKey:   stageKey,
		StageLabel: stageLabelMap[stageKey],
		TotalCount: totalCount,
		ByEmployee: byEmployee,
	}, nil
}

type abilityScoreAggregate struct {
	Score       *float64
	SampleCount int64
}

func (s *Service) calcEmployeeAbilityScores(ctx context.Context, tenantID int64) (map[int64]abilityScoreAggregate, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT employee_id, analysis_result
		FROM recordings
		WHERE tenant_id = $1
		  AND analysis_status = 'completed'
		  AND analysis_result IS NOT NULL
		  AND COALESCE(recorded_at, created_at) >= NOW() - INTERVAL '30 days'
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query ability scores: %w", err)
	}
	defer rows.Close()

	weights := map[string]float64{
		"开场建立权威": 0.10,
		"需求探索":   0.20,
		"问题放大":   0.15,
		"专业呈现":   0.15,
		"方案定制":   0.10,
		"异议化解":   0.20,
		"促成与收尾":  0.10,
	}
	stageByEmployee := map[int64]map[string][]float64{}
	sampleByEmployee := map[int64]int64{}

	for rows.Next() {
		var employeeID int64
		var resultData JSONObject
		if scanErr := rows.Scan(&employeeID, &resultData); scanErr != nil {
			return nil, fmt.Errorf("failed to scan ability score row: %w", scanErr)
		}
		qualityScore := pickMap(resultData, "quality_score")
		stagesRaw := qualityScore["stages"]
		stageList, ok := stagesRaw.([]interface{})
		if !ok || len(stageList) == 0 {
			continue
		}
		hasValid := false
		for _, stageAny := range stageList {
			stageMap, ok := stageAny.(map[string]interface{})
			if !ok {
				continue
			}
			stageName := strings.TrimSpace(fmt.Sprintf("%v", stageMap["name"]))
			if _, exists := weights[stageName]; !exists {
				continue
			}
			score, ok := toFloat(stageMap["score"])
			if !ok {
				continue
			}
			if _, exists := stageByEmployee[employeeID]; !exists {
				stageByEmployee[employeeID] = map[string][]float64{}
			}
			stageByEmployee[employeeID][stageName] = append(stageByEmployee[employeeID][stageName], score)
			hasValid = true
		}
		if hasValid {
			sampleByEmployee[employeeID]++
		}
	}

	out := map[int64]abilityScoreAggregate{}
	for employeeID, stageMap := range stageByEmployee {
		var weightedSum float64
		var weightTotal float64
		for stageName, values := range stageMap {
			if len(values) == 0 {
				continue
			}
			var sum float64
			for _, v := range values {
				sum += v
			}
			avg := sum / float64(len(values))
			w := weights[stageName]
			weightedSum += avg * w
			weightTotal += w
		}
		var scorePtr *float64
		if weightTotal > 0 {
			score := roundFloat(weightedSum/weightTotal, 2)
			scorePtr = &score
		}
		out[employeeID] = abilityScoreAggregate{
			Score:       scorePtr,
			SampleCount: sampleByEmployee[employeeID],
		}
	}
	return out, nil
}

type dashboardPrevRange struct {
	From string
	ToEx string
}

func buildPrevRange(dateFrom, dateTo string) *dashboardPrevRange {
	if strings.TrimSpace(dateFrom) == "" || strings.TrimSpace(dateTo) == "" {
		return nil
	}
	start, err := time.Parse("2006-01-02", dateFrom)
	if err != nil {
		return nil
	}
	end, err := time.Parse("2006-01-02", dateTo)
	if err != nil {
		return nil
	}
	delta := end.Sub(start) + 24*time.Hour
	prevFrom := start.Add(-delta)
	prevTo := end.Add(-delta).Add(24 * time.Hour)
	return &dashboardPrevRange{
		From: prevFrom.Format("2006-01-02"),
		ToEx: prevTo.Format("2006-01-02"),
	}
}

func buildDashboardDateRangeSQL(dateFrom, dateTo string, startArg int, alias string) (string, []interface{}) {
	col := "COALESCE(recorded_at, created_at)"
	if strings.TrimSpace(alias) != "" {
		col = fmt.Sprintf("COALESCE(%s.recorded_at, %s.created_at)", alias, alias)
	}
	parts := make([]string, 0, 2)
	args := make([]interface{}, 0, 2)
	arg := startArg
	if strings.TrimSpace(dateFrom) != "" {
		parts = append(parts, fmt.Sprintf("%s >= $%d", col, arg))
		args = append(args, strings.TrimSpace(dateFrom))
		arg++
	}
	if strings.TrimSpace(dateTo) != "" {
		parts = append(parts, fmt.Sprintf("%s < $%d", col, arg))
		if dt, err := time.Parse("2006-01-02", strings.TrimSpace(dateTo)); err == nil {
			args = append(args, dt.Add(24*time.Hour).Format("2006-01-02"))
		} else {
			args = append(args, strings.TrimSpace(dateTo))
		}
		arg++
	}
	if len(parts) == 0 {
		return "", args
	}
	return " AND " + strings.Join(parts, " AND "), args
}

func roundFloat(v float64, precision int) float64 {
	if precision < 0 {
		return v
	}
	p := math.Pow(10, float64(precision))
	return math.Round(v*p) / p
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		parsed, err := n.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func safeRate(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func strOrDefault(v *string, def string) string {
	if v == nil {
		return def
	}
	return *v
}

// Helper functions

// toTaskResponse converts a RecordingTask to TaskResponse
func toTaskResponse(t *RecordingTask) *TaskResponse {
	resp := &TaskResponse{
		ID:            t.ID,
		TenantID:      t.TenantID,
		RecordingID:   t.RecordingID,
		TaskType:      string(t.TaskType),
		Title:         t.Title,
		Description:   t.Description,
		CustomerName:  t.CustomerName,
		Priority:      t.Priority,
		Script:        t.Script,
		ContactReason: t.ContactReason,
		SourceType:    t.SourceType,
		SourceDetail:  t.SourceDetail,
		AssignedTo:    t.AssignedTo,
		AssignedBy:    t.AssignedBy,
		Status:        string(t.Status),
		CompletedBy:   t.CompletedBy,
		CancelReason:  t.CancelReason,
		CreatedAt:     t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if t.DueDate != nil {
		formatted := t.DueDate.Format("2006-01-02T15:04:05Z07:00")
		resp.DueDate = &formatted
		resp.DueAt = &formatted
	}

	if t.CompletedAt != nil {
		formatted := t.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CompletedAt = &formatted
	}

	if t.CancelledAt != nil {
		formatted := t.CancelledAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CancelledAt = &formatted
	}
	if resp.SourceType != nil {
		if label := sourceTypeLabel(*resp.SourceType); label != "" {
			resp.SourceTypeLabel = &label
		}
	}
	if resp.SourceDetail != nil {
		if label := sourceDetailLabel(*resp.SourceDetail); label != "" {
			resp.SourceDetailLabel = &label
		}
	}

	return resp
}

func compactTaskListItem(item *TaskResponse) {
	if item == nil {
		return
	}
	item.Description = nil
	item.Script = nil
	item.ContactReason = nil
	item.SourceDetail = nil
	item.SourceDetailLabel = nil
}

func (s *Service) enrichTaskListResponse(ctx context.Context, items []*TaskResponse) error {
	if len(items) == 0 {
		return nil
	}
	assignedIDs := make([]int64, 0, len(items))
	recordingIDs := make([]int64, 0, len(items))
	assignedSeen := map[int64]struct{}{}
	recordingSeen := map[int64]struct{}{}
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.AssignedTo != nil && *item.AssignedTo > 0 {
			if _, ok := assignedSeen[*item.AssignedTo]; !ok {
				assignedSeen[*item.AssignedTo] = struct{}{}
				assignedIDs = append(assignedIDs, *item.AssignedTo)
			}
		}
		if item.RecordingID > 0 {
			if _, ok := recordingSeen[item.RecordingID]; !ok {
				recordingSeen[item.RecordingID] = struct{}{}
				recordingIDs = append(recordingIDs, item.RecordingID)
			}
		}
	}

	assignedNameMap := map[int64]string{}
	if len(assignedIDs) > 0 {
		rows, err := s.store.pool.Query(ctx, `
			SELECT id, COALESCE(name, '')
			FROM employees
			WHERE id = ANY($1)
		`, assignedIDs)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			assignedNameMap[id] = strings.TrimSpace(name)
		}
	}

	type recordingMeta struct {
		OwnerName    string
		RoleCategory string
	}
	recordingMetaMap := map[int64]recordingMeta{}
	if len(recordingIDs) > 0 {
		rows, err := s.store.pool.Query(ctx, `
			SELECT
				r.id AS recording_id,
				COALESCE(NULLIF(e.full_name, ''), COALESCE(e.name, '')) AS owner_name,
				CASE
					WHEN EXISTS (
						SELECT 1 FROM institution_employee_roles ier
						WHERE ier.tenant_id = r.tenant_id
						  AND ier.employee_id = r.employee_id
						  AND lower(ier.role_code) = ANY($2)
					) THEN 'doctor'
					WHEN EXISTS (
						SELECT 1 FROM institution_employee_roles ier
						WHERE ier.tenant_id = r.tenant_id
						  AND ier.employee_id = r.employee_id
						  AND lower(ier.role_code) = ANY($3)
					) THEN 'consultant'
					ELSE 'other'
				END AS role_category
			FROM recordings r
			LEFT JOIN employees e ON e.id = r.employee_id
			WHERE r.id = ANY($1)
		`, recordingIDs, []string{"doctor", "doctor_assistant"}, []string{"consultant"})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var recordingID int64
			var ownerName string
			var roleCategory string
			if err := rows.Scan(&recordingID, &ownerName, &roleCategory); err != nil {
				return err
			}
			recordingMetaMap[recordingID] = recordingMeta{
				OwnerName:    strings.TrimSpace(ownerName),
				RoleCategory: strings.TrimSpace(roleCategory),
			}
		}
	}

	for _, item := range items {
		if item == nil {
			continue
		}
		if item.AssignedTo != nil {
			if name := strings.TrimSpace(assignedNameMap[*item.AssignedTo]); name != "" {
				item.AssignedToName = &name
			}
		}
		if meta, ok := recordingMetaMap[item.RecordingID]; ok {
			if meta.OwnerName != "" {
				item.RecordingOwnerName = &meta.OwnerName
			}
			if meta.RoleCategory != "" {
				category := meta.RoleCategory
				label := recordingRoleLabel(category)
				item.RecordingRoleCategory = &category
				item.RecordingRoleLabel = &label
			}
		}
	}
	return nil
}

func recordingRoleLabel(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "consultant":
		return "咨询师录音"
	case "customer_service":
		return "客服录音"
	case "doctor":
		return "医生录音"
	case "therapist":
		return "康复师录音"
	case "doctor_assistant":
		return "医助录音"
	default:
		return "其他录音"
	}
}

func sourceTypeLabel(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "ai":
		return "AI任务"
	case "follow_up":
		return "跟进任务"
	case "manual":
		return "手动任务"
	default:
		return ""
	}
}

func sourceDetailLabel(sourceDetail string) string {
	switch strings.ToLower(strings.TrimSpace(sourceDetail)) {
	case "phone":
		return "电话"
	case "wechat":
		return "微信"
	case "sms":
		return "短信"
	default:
		return ""
	}
}

// Front-desk analysis methods

// ListShiftAnalyses lists shift analyses for a tenant
func (s *Service) ListShiftAnalyses(ctx context.Context, tenantID int64, page, pageSize int, employeeID *int64, dateStr string) ([]map[string]interface{}, int64, error) {
	return s.store.ListShiftAnalyses(ctx, tenantID, page, pageSize, employeeID, dateStr)
}

// GetShiftAnalysis gets a single shift analysis
func (s *Service) GetShiftAnalysis(ctx context.Context, tenantID, id int64) (map[string]interface{}, error) {
	return s.store.GetShiftAnalysis(ctx, tenantID, id)
}

// CreateShiftAnalysis creates a new shift analysis
func (s *Service) CreateShiftAnalysis(ctx context.Context, tenantID int64, req *ShiftAnalysisCreateReq) (map[string]interface{}, error) {
	return s.store.CreateShiftAnalysis(ctx, tenantID, req)
}

// ListDailyReports lists daily reports for a tenant
func (s *Service) ListDailyReports(ctx context.Context, tenantID int64, page, pageSize int) ([]map[string]interface{}, int64, error) {
	return s.store.ListDailyReports(ctx, tenantID, page, pageSize)
}

// GetFrontdeskDailyReport gets a frontdesk daily report by date
func (s *Service) GetFrontdeskDailyReport(ctx context.Context, tenantID int64, dateStr string) (map[string]interface{}, error) {
	return s.store.GetFrontdeskDailyReport(ctx, tenantID, dateStr)
}

// ListKnowledgeBases lists knowledge bases for a tenant
func (s *Service) ListKnowledgeBases(ctx context.Context, tenantID int64, kbType string) ([]map[string]interface{}, error) {
	return s.store.ListKnowledgeBases(ctx, tenantID, kbType)
}

// GetKnowledgeBase gets a knowledge base by type
func (s *Service) GetKnowledgeBase(ctx context.Context, tenantID int64, kbType string) (map[string]interface{}, error) {
	return s.store.GetKnowledgeBase(ctx, tenantID, kbType)
}

// CreateKnowledgeBase creates a new knowledge base
func (s *Service) CreateKnowledgeBase(ctx context.Context, tenantID int64, kbType string, content map[string]interface{}) (map[string]interface{}, error) {
	return s.store.CreateKnowledgeBase(ctx, tenantID, kbType, content)
}

// UpdateKnowledgeBase updates a knowledge base
func (s *Service) UpdateKnowledgeBase(ctx context.Context, tenantID int64, kbType string, content map[string]interface{}) (map[string]interface{}, error) {
	return s.store.UpdateKnowledgeBase(ctx, tenantID, kbType, content)
}

// ListWeeklyReports lists weekly reports for a tenant
func (s *Service) ListWeeklyReports(ctx context.Context, tenantID int64, page, pageSize int) ([]map[string]interface{}, int64, error) {
	return s.store.ListWeeklyReports(ctx, tenantID, page, pageSize)
}

// GetWeeklyReport gets a weekly report by date
func (s *Service) GetWeeklyReport(ctx context.Context, tenantID int64, dateStr string) (map[string]interface{}, error) {
	return s.store.GetWeeklyReport(ctx, tenantID, dateStr)
}

// GenerateWeeklyReport generates a weekly report
func (s *Service) GenerateWeeklyReport(ctx context.Context, tenantID int64, dateStr string) (map[string]interface{}, error) {
	weekEndDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid date format: %w", err)
	}

	weeklyService := NewWeeklyReportService(s.store, "", "", "", "", "", slog.Default())
	reportData, err := weeklyService.GenerateWeeklyReport(ctx, tenantID, weekEndDate)
	if err != nil {
		return nil, fmt.Errorf("generate weekly report data: %w", err)
	}

	analysis := map[string]interface{}{
		"summary":                 buildFrontdeskWeeklySummary(reportData),
		"total_interactions":      reportData.TotalInteractions,
		"total_appointments":      reportData.TotalAppointments,
		"total_walk_ins":          reportData.TotalWalkIns,
		"risk_event_count":        reportData.RiskEventCount,
		"top_questions":           reportData.TopQuestions,
		"competitor_mentions":     reportData.CompetitorMentions,
		"doctor_inquiries":        reportData.DoctorInquiries,
		"channel_feedback":        reportData.ChannelFeedback,
		"key_insights":            reportData.KeyInsights,
		"key_changes":             reportData.KeyInsights,
		"next_actions":            reportData.ImprovementSuggestions,
		"priority_actions":        reportData.ImprovementSuggestions,
		"question_structure":      reportData.TopQuestions,
		"response_quality":        buildWeeklyResponseQuality(reportData),
		"issue_summary":           reportData.IssueSummary,
		"staff_highlights":        buildWeeklyStaffHighlights(reportData),
		"staff_variances":         buildWeeklyStaffVariances(reportData),
		"staff_suggestions":       buildWeeklyStaffSuggestions(reportData),
		"improvement_suggestions": reportData.ImprovementSuggestions,
		"trend_analysis":          reportData.TrendAnalysis,
		"evidence_count":          len(reportData.TopQuestions),
		"evidence_items":          buildWeeklyEvidenceItems(reportData.TopQuestions),
		"action_owners":           buildWeeklyActionOwners(reportData),
		"action_effects":          buildWeeklyActionEffects(reportData),
		"week_start_date":         reportData.WeekStartDate,
		"week_end_date":           reportData.WeekEndDate,
		"generated_at":            reportData.GeneratedAt.Format(time.RFC3339),
		"status":                  "draft",
	}

	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("marshal weekly analysis: %w", err)
	}
	topQuestionsJSON, err := json.Marshal(reportData.TopQuestions)
	if err != nil {
		return nil, fmt.Errorf("marshal top_questions: %w", err)
	}
	competitorMentionsJSON, err := json.Marshal(reportData.CompetitorMentions)
	if err != nil {
		return nil, fmt.Errorf("marshal competitor_mentions: %w", err)
	}
	doctorInquiriesJSON, err := json.Marshal(reportData.DoctorInquiries)
	if err != nil {
		return nil, fmt.Errorf("marshal doctor_inquiries: %w", err)
	}
	channelFeedbackJSON, err := json.Marshal(reportData.ChannelFeedback)
	if err != nil {
		return nil, fmt.Errorf("marshal channel_feedback: %w", err)
	}

	if _, err := s.store.pool.Exec(ctx, `
		INSERT INTO frontdesk_daily_reports (
			tenant_id, report_date,
			total_estimated_interactions, estimated_appointment_count, estimated_walkin_count,
			top_questions, competitor_mentions, doctor_inquiries, channel_feedback, risk_event_count, testimonial_materials, created_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
		ON CONFLICT (tenant_id, report_date) DO UPDATE
		SET total_estimated_interactions = EXCLUDED.total_estimated_interactions,
		    estimated_appointment_count = EXCLUDED.estimated_appointment_count,
		    estimated_walkin_count = EXCLUDED.estimated_walkin_count,
		    top_questions = EXCLUDED.top_questions,
		    competitor_mentions = EXCLUDED.competitor_mentions,
		    doctor_inquiries = EXCLUDED.doctor_inquiries,
		    channel_feedback = EXCLUDED.channel_feedback,
		    risk_event_count = EXCLUDED.risk_event_count,
		    testimonial_materials = EXCLUDED.testimonial_materials
	`,
		tenantID, dateStr,
		reportData.TotalInteractions, reportData.TotalAppointments, reportData.TotalWalkIns,
		topQuestionsJSON, competitorMentionsJSON, doctorInquiriesJSON, channelFeedbackJSON, reportData.RiskEventCount, analysisJSON,
	); err != nil {
		return nil, fmt.Errorf("save weekly report: %w", err)
	}

	return s.store.GetWeeklyReport(ctx, tenantID, dateStr)
}

func (s *Service) PublishWeeklyReport(ctx context.Context, tenantID int64, dateStr, managerComment, publisherName string) (map[string]interface{}, error) {
	report, err := s.store.GetWeeklyReport(ctx, tenantID, dateStr)
	if err != nil {
		return nil, err
	}
	analysis, _ := report["analysis"].(map[string]interface{})
	if analysis == nil {
		analysis = map[string]interface{}{}
	}
	analysis["status"] = "published"
	analysis["manager_comment"] = strings.TrimSpace(managerComment)
	analysis["published_at"] = time.Now().Format(time.RFC3339)
	if strings.TrimSpace(publisherName) != "" {
		analysis["published_by_name"] = strings.TrimSpace(publisherName)
	}

	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("marshal published analysis: %w", err)
	}
	if _, err := s.store.pool.Exec(ctx, `
		UPDATE frontdesk_daily_reports
		SET testimonial_materials = $3
		WHERE tenant_id = $1 AND report_date = $2
	`, tenantID, dateStr, analysisJSON); err != nil {
		return nil, fmt.Errorf("update weekly report publish status: %w", err)
	}

	return s.store.GetWeeklyReport(ctx, tenantID, dateStr)
}

func (s *Service) AddWeeklyReportEvidence(ctx context.Context, tenantID int64, reportDate string, recordingID int64, shiftDate, summary string) (map[string]interface{}, error) {
	targetDate := strings.TrimSpace(reportDate)
	if targetDate == "" {
		targetDate = strings.TrimSpace(shiftDate)
	}
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	}

	report, err := s.store.GetWeeklyReport(ctx, tenantID, targetDate)
	if err != nil {
		if _, genErr := s.GenerateWeeklyReport(ctx, tenantID, targetDate); genErr != nil {
			return nil, fmt.Errorf("prepare weekly report: %w", genErr)
		}
		report, err = s.store.GetWeeklyReport(ctx, tenantID, targetDate)
		if err != nil {
			return nil, err
		}
	}

	analysis, _ := report["analysis"].(map[string]interface{})
	if analysis == nil {
		analysis = map[string]interface{}{}
	}

	items := make([]interface{}, 0, 8)
	if raw, ok := analysis["evidence_items"].([]interface{}); ok {
		items = append(items, raw...)
	}
	items = append(items, map[string]interface{}{
		"recording_id": recordingID,
		"shift_date":   strings.TrimSpace(shiftDate),
		"summary":      strings.TrimSpace(summary),
		"added_at":     time.Now().Format(time.RFC3339),
	})
	analysis["evidence_items"] = items
	analysis["evidence_count"] = len(items)

	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence analysis: %w", err)
	}
	if _, err := s.store.pool.Exec(ctx, `
		UPDATE frontdesk_daily_reports
		SET testimonial_materials = $3
		WHERE tenant_id = $1 AND report_date = $2
	`, tenantID, targetDate, analysisJSON); err != nil {
		return nil, fmt.Errorf("update weekly report evidence: %w", err)
	}
	return s.store.GetWeeklyReport(ctx, tenantID, targetDate)
}

func buildFrontdeskWeeklySummary(data *WeeklyReportData) string {
	if data == nil {
		return "本周暂无可用数据，建议优先补齐录音分析样本。"
	}
	return fmt.Sprintf(
		"本周累计交互 %d 次（预约 %d / walk-in %d），风险事件 %d 起，建议围绕高频问题与风险场景优先优化应答。",
		data.TotalInteractions,
		data.TotalAppointments,
		data.TotalWalkIns,
		data.RiskEventCount,
	)
}

func buildWeeklyResponseQuality(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	out := make([]string, 0, 3)
	if data.TotalInteractions > 0 {
		appointmentRate := float64(data.TotalAppointments) / float64(data.TotalInteractions) * 100
		walkInRate := float64(data.TotalWalkIns) / float64(data.TotalInteractions) * 100
		out = append(out, fmt.Sprintf("预约承接占比 %.1f%%（%d/%d）", appointmentRate, data.TotalAppointments, data.TotalInteractions))
		out = append(out, fmt.Sprintf("walk-in 占比 %.1f%%（%d/%d）", walkInRate, data.TotalWalkIns, data.TotalInteractions))
	}
	if data.RiskEventCount <= 0 {
		out = append(out, "合规风险本周未检出明显异常")
	} else {
		out = append(out, fmt.Sprintf("合规风险共 %d 起，建议对高风险应答逐条复盘", data.RiskEventCount))
	}
	return out
}

func buildWeeklyStaffHighlights(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	out := make([]string, 0, 2)
	if data.TotalAppointments > 0 {
		out = append(out, fmt.Sprintf("预约承接完成 %d 次，建议复用高转化应答片段", data.TotalAppointments))
	}
	if len(data.ChannelFeedback) > 0 {
		out = append(out, fmt.Sprintf("渠道反馈覆盖 %d 类问题，形成跨渠道标准答复基础", len(data.ChannelFeedback)))
	}
	return out
}

func buildWeeklyStaffVariances(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	out := make([]string, 0, 2)
	if data.TotalInteractions > 0 && data.TotalWalkIns > data.TotalInteractions/2 {
		out = append(out, "walk-in 占比偏高，班次承接稳定性波动风险上升")
	}
	if data.RiskEventCount > 0 {
		out = append(out, fmt.Sprintf("风险事件 %d 起，个别场景应答一致性不足", data.RiskEventCount))
	}
	return out
}

func buildWeeklyStaffSuggestions(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	out := make([]string, 0, 3)
	out = append(out, "按高频问题组织班次演练，统一标准应答")
	if data.RiskEventCount > 0 {
		out = append(out, "高风险录音建立日复盘机制，次日追踪改进结果")
	}
	if data.TotalWalkIns > 0 {
		out = append(out, "针对 walk-in 场景补齐即时安排与恢复期说明话术")
	}
	return out
}

func buildWeeklyEvidenceItems(questions []string) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(questions))
	for i, q := range questions {
		topic := strings.TrimSpace(q)
		if topic == "" {
			continue
		}
		items = append(items, map[string]interface{}{
			"recording_id": 0,
			"shift_date":   "",
			"summary":      fmt.Sprintf("高频问题主题：%s", topic),
			"source":       "weekly_aggregation",
			"rank":         i + 1,
		})
	}
	return items
}

func buildWeeklyActionOwners(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	owners := []string{"前台主管"}
	if data.RiskEventCount > 0 {
		owners = append(owners, "合规负责人")
	}
	if len(data.TopQuestions) > 0 {
		owners = append(owners, "培训负责人")
	}
	return owners
}

func buildWeeklyActionEffects(data *WeeklyReportData) []string {
	if data == nil {
		return []string{}
	}
	effects := make([]string, 0, 3)
	if data.TotalInteractions > 0 {
		effects = append(effects, "观察下周预约承接率与 walk-in 承接率变化")
	}
	if data.RiskEventCount > 0 {
		effects = append(effects, "跟踪高风险场景复盘后复发率是否下降")
	}
	if len(data.TopQuestions) > 0 {
		effects = append(effects, "验证高频问题命中后回答完整率是否提升")
	}
	return effects
}

package recording

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Service struct {
	store       *Store
	workerURL   string
	workerToken string
	httpClient  *http.Client
}

func NewService(store *Store, workerURL, workerToken string) *Service {
	return &Service{
		store:       store,
		workerURL:   strings.TrimRight(workerURL, "/"),
		workerToken: workerToken,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
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
		responses[i] = toRecordingResponse(r)
	}

	return responses, total, nil
}

// GetRecording retrieves a medical recording by ID
func (s *Service) GetRecording(ctx context.Context, id int64) (*RecordingResponse, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
}

// CreateRecording creates a new medical recording
func (s *Service) CreateRecording(ctx context.Context, req CreateRecordingRequest) (*RecordingResponse, error) {
	// Validate request
	if req.TenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if req.EmployeeID == 0 {
		return nil, fmt.Errorf("employee_id is required")
	}
	if req.PatientName == "" {
		return nil, fmt.Errorf("patient_name is required")
	}
	if req.RecordingURL == "" {
		return nil, fmt.Errorf("recording_url is required")
	}

	recording, err := s.store.CreateRecording(ctx, req)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
}

// UpdateRecording updates a medical recording
func (s *Service) UpdateRecording(ctx context.Context, id int64, req UpdateRecordingRequest) (*RecordingResponse, error) {
	recording, err := s.store.UpdateRecording(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toRecordingResponse(recording), nil
}

// DeleteRecording deletes a medical recording
func (s *Service) DeleteRecording(ctx context.Context, id int64) error {
	return s.store.DeleteRecording(ctx, id)
}

// toRecordingResponse converts a MedicalRecording to RecordingResponse
func toRecordingResponse(r *MedicalRecording) *RecordingResponse {
	resp := &RecordingResponse{
		ID:                r.ID,
		TenantID:          r.TenantID,
		EmployeeID:        r.EmployeeID,
		PatientName:       r.PatientName,
		PatientAge:        r.PatientAge,
		PatientGender:     r.PatientGender,
		PatientPhone:      r.PatientPhone,
		RecordingURL:      r.RecordingURL,
		RecordingDuration: r.RecordingDuration,
		TranscriptText:    r.TranscriptText,
		DoctorSummary:     r.DoctorSummary,
		TherapistSummary:  r.TherapistSummary,
		ConsultantSummary: r.ConsultantSummary,
		Status:            string(r.Status),
		ProcessingError:   r.ProcessingError,
		CreatedAt:         r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:         r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if r.RecordingStartedAt != nil {
		formatted := r.RecordingStartedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.RecordingStartedAt = &formatted
	}

	if r.RecordingEndedAt != nil {
		formatted := r.RecordingEndedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.RecordingEndedAt = &formatted
	}

	if r.ProcessedAt != nil {
		formatted := r.ProcessedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ProcessedAt = &formatted
	}

	return resp
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
	return s.store.CompleteTask(ctx, id, completedBy)
}

// CancelTask marks a task as cancelled
func (s *Service) CancelTask(ctx context.Context, id int64, req CancelTaskRequest) error {
	if req.Reason == "" {
		return fmt.Errorf("cancel reason is required")
	}
	return s.store.CancelTask(ctx, id, req.Reason)
}

// GetTaskStats retrieves task statistics
func (s *Service) GetTaskStats(ctx context.Context, tenantID int64, assignedTo *int64) (*RecordingTaskStatsResponse, error) {
	return s.store.GetTaskStats(ctx, tenantID, assignedTo)
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
	RecordingID int64  `json:"recording_id"`
	TenantID    int64  `json:"tenant_id"`
	JobType     string `json:"job_type"`
	Force       bool   `json:"force,omitempty"`
}

type workerJobResponse struct {
	JobID string `json:"job_id"`
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
		httpReq.Header.Set("Authorization", "Bearer "+s.workerToken)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to call recording worker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("recording worker returned status %d", resp.StatusCode)
	}

	var result workerJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode worker response: %w", err)
	}

	return result.JobID, nil
}

func (s *Service) triggerWorkerJob(ctx context.Context, id int64, jobType string, force bool) (string, error) {
	recording, err := s.store.GetRecordingByID(ctx, id)
	if err != nil {
		return "", err
	}

	jobID, err := s.submitWorkerJob(ctx, workerJobRequest{
		RecordingID: id,
		TenantID:    recording.TenantID,
		JobType:     jobType,
		Force:       force,
	})
	if err != nil {
		return "", err
	}

	status := StatusProcessing
	_, err = s.store.UpdateRecording(ctx, id, UpdateRecordingRequest{Status: &status})
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

// GetDoctorAbilityRanking retrieves doctor ability ranking
func (s *Service) GetDoctorAbilityRanking(ctx context.Context, tenantID int64) ([]DoctorAbilityRankingResponse, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT
			e.id,
			COALESCE(NULLIF(e.name, ''), '未知员工') AS employee_name,
			COUNT(mr.id) AS recording_count,
			COUNT(CASE WHEN mr.analysis_status = 'completed' THEN 1 END) AS completed_count
		FROM employees e
		LEFT JOIN recordings mr ON mr.employee_id = e.id AND mr.tenant_id = $1
		WHERE e.tenant_id = $1
		GROUP BY e.id, employee_name
		HAVING COUNT(mr.id) > 0
		ORDER BY recording_count DESC, completed_count DESC
		LIMIT 20
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query doctor ranking: %w", err)
	}
	defer rows.Close()

	var ranking []DoctorAbilityRankingResponse
	for rows.Next() {
		var (
			item           DoctorAbilityRankingResponse
			completedCount int64
		)
		if err := rows.Scan(&item.EmployeeID, &item.EmployeeName, &item.RecordingCount, &completedCount); err != nil {
			return nil, fmt.Errorf("failed to scan doctor ranking: %w", err)
		}
		if item.RecordingCount > 0 {
			item.AvgScore = float64(completedCount) / float64(item.RecordingCount) * 100
		}
		ranking = append(ranking, item)
	}

	for i := range ranking {
		ranking[i].Rank = i + 1
	}
	return ranking, nil
}

// GetDoctorAbilityDetail retrieves detailed doctor ability
func (s *Service) GetDoctorAbilityDetail(ctx context.Context, tenantID, employeeID int64) (*DoctorAbilityDetailResponse, error) {
	var name string
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(name, ''), '未知员工')
		FROM employees
		WHERE id = $1 AND tenant_id = $2
	`, employeeID, tenantID).Scan(&name); err != nil {
		return nil, fmt.Errorf("employee not found")
	}

	var total, completed int64
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(CASE WHEN analysis_status = 'completed' THEN 1 END)
		FROM recordings
		WHERE tenant_id = $1 AND employee_id = $2
	`, tenantID, employeeID).Scan(&total, &completed); err != nil {
		return nil, fmt.Errorf("failed to query doctor detail: %w", err)
	}

	score := 0.0
	if total > 0 {
		score = float64(completed) / float64(total) * 100
	}

	return &DoctorAbilityDetailResponse{
		EmployeeID:           employeeID,
		EmployeeName:         name,
		CommunicationScore:   score,
		ProfessionalismScore: score,
		EmpathyScore:         score,
		EfficiencyScore:      score,
		RecentTrend:          "stable",
		Strengths:            []string{"按计划完成录音处理"},
		Weaknesses:           []string{"建议提升高峰期处理效率"},
	}, nil
}

// GetCommunicationAnalysis retrieves communication analysis
func (s *Service) GetCommunicationAnalysis(ctx context.Context, tenantID int64) (*CommunicationAnalysisResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	top, err := s.GetDoctorAbilityRanking(ctx, tenantID)
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

// GetWeeklyMeetingMaterial retrieves weekly meeting material
func (s *Service) GetWeeklyMeetingMaterial(ctx context.Context, tenantID int64) (*WeeklyMeetingMaterialResponse, error) {
	now := time.Now()
	weekStart := now.AddDate(0, 0, -6)
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	practices, err := s.store.ListBestPractices(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	best := make([]BestPracticeItem, 0, len(practices))
	for i, p := range practices {
		if i >= 5 {
			break
		}
		best = append(best, BestPracticeItem{
			RecordingID: p.RecordingID,
			Title:       p.Title,
			Description: strOrDefault(p.Description, ""),
		})
	}
	return &WeeklyMeetingMaterialResponse{
		WeekStart: weekStart.Format("2006-01-02"),
		WeekEnd:   now.Format("2006-01-02"),
		Highlights: []string{
			fmt.Sprintf("本周累计处理录音 %d 条", stats.TotalRecordings),
			fmt.Sprintf("本周完成率 %.1f%%", safeRate(stats.CompletedRecordings, stats.TotalRecordings)),
		},
		BestPractices:    best,
		ImprovementAreas: []string{"关注失败和待处理录音", "持续优化跟进动作闭环"},
	}, nil
}

// GetWeeklySummary retrieves weekly summary
func (s *Service) GetWeeklySummary(ctx context.Context, tenantID int64) (*WeeklySummaryResponse, error) {
	now := time.Now()
	weekStart := now.AddDate(0, 0, -6)
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	top, err := s.GetDoctorAbilityRanking(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(top) > 3 {
		top = top[:3]
	}
	return &WeeklySummaryResponse{
		WeekStart:       weekStart.Format("2006-01-02"),
		WeekEnd:         now.Format("2006-01-02"),
		TotalRecordings: stats.TotalRecordings,
		AvgScore:        safeRate(stats.CompletedRecordings, stats.TotalRecordings),
		TopPerformers:   top,
		KeyInsights:     []string{"完成率稳定", "建议继续提升任务及时率"},
	}, nil
}

// GetTeamTrends retrieves team trends
func (s *Service) GetTeamTrends(ctx context.Context, tenantID int64, period string) (*TeamTrendsResponse, error) {
	if period == "" {
		period = "daily"
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT DATE(created_at) AS dt,
		       COUNT(*) AS total,
		       COUNT(CASE WHEN analysis_status = 'completed' THEN 1 END) AS completed
		FROM recordings
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY dt
		ORDER BY dt ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query team trends: %w", err)
	}
	defer rows.Close()

	var points []TrendDataPoint
	for rows.Next() {
		var (
			dt        time.Time
			total     int64
			completed int64
		)
		if err := rows.Scan(&dt, &total, &completed); err != nil {
			return nil, fmt.Errorf("failed to scan trend row: %w", err)
		}
		points = append(points, TrendDataPoint{
			Date:           dt.Format("2006-01-02"),
			AvgScore:       safeRate(completed, total),
			RecordingCount: total,
		})
	}
	return &TeamTrendsResponse{Period: period, Data: points}, nil
}

// GetDailyReport retrieves daily report
func (s *Service) GetDailyReport(ctx context.Context, tenantID int64, date string) (*DailyReportResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	top, err := s.GetDoctorAbilityRanking(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(top) > 5 {
		top = top[:5]
	}
	reportDate := date
	if reportDate == "" {
		reportDate = time.Now().Format("2006-01-02")
	}
	return &DailyReportResponse{
		Date:            reportDate,
		TotalRecordings: stats.TotalRecordings,
		TotalDuration:   stats.TotalDuration,
		AvgScore:        safeRate(stats.CompletedRecordings, stats.TotalRecordings),
		TopPerformers:   top,
		KeyMetrics: JSONObject{
			"pending_tasks": stats.PendingRecordings,
			"failed_tasks":  stats.FailedRecordings,
		},
	}, nil
}

// GetDiagnosis retrieves diagnosis
func (s *Service) GetDiagnosis(ctx context.Context, tenantID int64) (*DiagnosisResponse, error) {
	stats, err := s.store.GetStatsOverview(ctx, tenantID, nil, nil)
	if err != nil {
		return nil, err
	}
	score := safeRate(stats.CompletedRecordings, stats.TotalRecordings)
	health := "poor"
	switch {
	case score >= 90:
		health = "excellent"
	case score >= 75:
		health = "good"
	case score >= 60:
		health = "fair"
	}
	issues := []IssueCount{}
	if stats.PendingRecordings > 0 {
		issues = append(issues, IssueCount{Issue: "待处理录音", Count: stats.PendingRecordings})
	}
	if stats.FailedRecordings > 0 {
		issues = append(issues, IssueCount{Issue: "失败录音", Count: stats.FailedRecordings})
	}
	trends, err := s.GetTeamTrends(ctx, tenantID, "daily")
	if err != nil {
		return nil, err
	}
	data := trends.Data
	if len(data) > 7 {
		data = data[len(data)-7:]
	}
	sort.Slice(data, func(i, j int) bool { return data[i].Date < data[j].Date })

	return &DiagnosisResponse{
		OverallHealth: health,
		Issues:        issues,
		Recommendations: []string{
			"优先处理超时与失败任务",
			"提升录音分析完成率",
		},
		Trends: data,
	}, nil
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
		ID:           t.ID,
		TenantID:     t.TenantID,
		RecordingID:  t.RecordingID,
		TaskType:     string(t.TaskType),
		Title:        t.Title,
		Description:  t.Description,
		AssignedTo:   t.AssignedTo,
		AssignedBy:   t.AssignedBy,
		Status:       string(t.Status),
		CompletedBy:  t.CompletedBy,
		CancelReason: t.CancelReason,
		CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if t.DueDate != nil {
		formatted := t.DueDate.Format("2006-01-02T15:04:05Z07:00")
		resp.DueDate = &formatted
	}

	if t.CompletedAt != nil {
		formatted := t.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CompletedAt = &formatted
	}

	if t.CancelledAt != nil {
		formatted := t.CancelledAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CancelledAt = &formatted
	}

	return resp
}

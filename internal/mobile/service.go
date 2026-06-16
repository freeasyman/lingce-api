package mobile

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/recording"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool             *pgxpool.Pool
	recordingService *recording.Service
}

func NewService(pool *pgxpool.Pool, recordingService *recording.Service) *Service {
	return &Service{
		pool:             pool,
		recordingService: recordingService,
	}
}

func (s *Service) GetMe(ctx context.Context, claims *auth.Claims) (*EmployeeSummary, error) {
	return s.getCurrentEmployee(ctx, claims)
}

func (s *Service) GetHome(ctx context.Context, claims *auth.Claims) (*HomeResponse, error) {
	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return nil, err
	}

	todoTasks, todoTotal, err := s.ListTasks(ctx, claims, TaskListParams{Status: "todo", Page: 1, PageSize: 3})
	if err != nil {
		return nil, err
	}
	doneTotal, err := s.countTasks(ctx, claims, recording.TaskStatusCompleted)
	if err != nil {
		return nil, err
	}
	recordings, recordingTotal, err := s.ListRecordings(ctx, claims, RecordingListParams{Page: 1, PageSize: 3})
	if err != nil {
		return nil, err
	}

	return &HomeResponse{
		Me: me,
		Summary: HomeSummary{
			TodoTaskCount:    todoTotal,
			DoneTaskCount:    doneTotal,
			RecordingCount:   recordingTotal,
			RecentTaskCount:  len(todoTasks),
			RecentRecordings: len(recordings),
		},
		TodoTasks:        todoTasks,
		RecentRecordings: recordings,
		ServerTime:       time.Now().Format(time.RFC3339),
	}, nil
}

func (s *Service) ListTasks(ctx context.Context, claims *auth.Claims, params TaskListParams) ([]*recording.TaskResponse, int, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}

	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return nil, 0, err
	}

	switch strings.ToLower(strings.TrimSpace(params.Status)) {
	case "", "todo":
		return s.listTodoTasks(ctx, me, params)
	case "done":
		done := recording.TaskStatusCompleted
		req := recording.TaskListRequest{
			TenantID:   &me.TenantID,
			AssignedTo: &me.ID,
			Status:     &done,
			Page:       params.Page,
			PageSize:   params.PageSize,
		}
		s.applyTaskFilters(&req, params)
		return s.recordingService.ListRecordingTasks(ctx, req)
	default:
		return nil, 0, fmt.Errorf("unsupported task status: %s", params.Status)
	}
}

func (s *Service) GetTask(ctx context.Context, claims *auth.Claims, taskID int64) (*recording.TaskResponse, error) {
	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return nil, err
	}
	task, err := s.recordingService.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := ensureTaskOwnedByEmployee(task, me); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Service) CompleteTask(ctx context.Context, claims *auth.Claims, taskID int64, req recording.CompleteTaskRequest) error {
	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return err
	}
	task, err := s.recordingService.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if err := ensureTaskOwnedByEmployee(task, me); err != nil {
		return err
	}
	return s.recordingService.CompleteTask(ctx, taskID, me.ID, req)
}

func (s *Service) ListRecordings(ctx context.Context, claims *auth.Claims, params RecordingListParams) ([]*recording.RecordingResponse, int, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return nil, 0, err
	}
	scope := recordingScopeForRole(me.RoleCode)
	includeShort := false
	req := recording.RecordingListRequest{
		TenantID:     me.TenantID,
		EmployeeID:   &me.ID,
		Scope:        &scope,
		IncludeShort: &includeShort,
		Page:         params.Page,
		PageSize:     params.PageSize,
	}
	if params.BusinessScope != "" {
		req.BusinessScope = &params.BusinessScope
	}
	if params.Q != "" {
		req.Keyword = &params.Q
	}
	if params.AnalysisStatus != "" {
		status := recording.RecordingStatus(strings.TrimSpace(params.AnalysisStatus))
		req.Status = &status
	}
	if params.HasTask != nil {
		req.HasTask = params.HasTask
	}
	if params.HasContentSeed != nil {
		req.HasContentSeed = params.HasContentSeed
	}
	if params.CriticalGap != nil {
		req.CriticalGapOnly = params.CriticalGap
	}
	if params.Sort != "" {
		req.Sort = &params.Sort
	}
	if startDate, endDate, err := resolveDateRange(params.DateFrom, params.DateTo, params.TimeRange); err != nil {
		return nil, 0, err
	} else {
		req.StartDate = startDate
		req.EndDate = endDate
	}
	return s.recordingService.ListRecordings(ctx, req)
}

func (s *Service) GetRecording(ctx context.Context, claims *auth.Claims, recordingID int64) (*recording.RecordingResponse, error) {
	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return nil, err
	}
	item, err := s.recordingService.GetRecording(ctx, recordingID)
	if err != nil {
		return nil, err
	}
	if item.TenantID != me.TenantID || item.EmployeeID != me.ID {
		return nil, fmt.Errorf("recording not found")
	}
	return item, nil
}

func (s *Service) countTasks(ctx context.Context, claims *auth.Claims, status recording.TaskStatus) (int, error) {
	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return 0, err
	}
	_, total, err := s.recordingService.ListRecordingTasks(ctx, recording.TaskListRequest{
		TenantID:   &me.TenantID,
		AssignedTo: &me.ID,
		Status:     &status,
		Page:       1,
		PageSize:   1,
	})
	return total, err
}

func (s *Service) listTodoTasks(ctx context.Context, me *EmployeeSummary, params TaskListParams) ([]*recording.TaskResponse, int, error) {
	pendingTasks, pendingTotal, err := s.fetchAllTasksByStatus(ctx, me, recording.TaskStatusPending, params)
	if err != nil {
		return nil, 0, err
	}
	assignedTasks, assignedTotal, err := s.fetchAllTasksByStatus(ctx, me, recording.TaskStatusAssigned, params)
	if err != nil {
		return nil, 0, err
	}

	merged := make([]*recording.TaskResponse, 0, len(pendingTasks)+len(assignedTasks))
	merged = append(merged, pendingTasks...)
	merged = append(merged, assignedTasks...)
	sort.SliceStable(merged, func(i, j int) bool {
		return parseRFC3339(merged[i].CreatedAt).After(parseRFC3339(merged[j].CreatedAt))
	})

	total := pendingTotal + assignedTotal
	start := (params.Page - 1) * params.PageSize
	if start >= len(merged) {
		return []*recording.TaskResponse{}, total, nil
	}
	end := start + params.PageSize
	if end > len(merged) {
		end = len(merged)
	}
	return merged[start:end], total, nil
}

func (s *Service) fetchAllTasksByStatus(ctx context.Context, me *EmployeeSummary, status recording.TaskStatus, params TaskListParams) ([]*recording.TaskResponse, int, error) {
	all := make([]*recording.TaskResponse, 0)
	page := 1
	pageSize := 100
	total := 0

	for {
		req := recording.TaskListRequest{
			TenantID:   &me.TenantID,
			AssignedTo: &me.ID,
			Status:     &status,
			Page:       page,
			PageSize:   pageSize,
		}
		s.applyTaskFilters(&req, params)
		items, itemTotal, err := s.recordingService.ListRecordingTasks(ctx, req)
		if err != nil {
			return nil, 0, err
		}
		if total == 0 {
			total = itemTotal
		}
		all = append(all, items...)
		if len(all) >= total || len(items) == 0 {
			break
		}
		page++
	}

	return all, total, nil
}

func (s *Service) applyTaskFilters(req *recording.TaskListRequest, params TaskListParams) {
	if req == nil {
		return
	}
	if params.Q != "" {
		req.Keyword = &params.Q
	}
	if params.Priority != "" {
		req.Priority = &params.Priority
	}
	if params.DueBucket != "" {
		req.DueBucket = &params.DueBucket
	}
	if params.Sort != "" {
		req.Sort = &params.Sort
	}
	if params.TaskType != "" {
		taskType := recording.TaskType(strings.TrimSpace(params.TaskType))
		req.TaskType = &taskType
	}
	if startDate, endDate, err := resolveDateRange(params.DateFrom, params.DateTo, ""); err == nil {
		req.StartDate = startDate
		req.EndDate = endDate
	}
}

func resolveDateRange(dateFrom, dateTo, timeRange string) (*time.Time, *time.Time, error) {
	if strings.TrimSpace(timeRange) != "" {
		now := time.Now()
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		switch strings.TrimSpace(timeRange) {
		case "today":
			start := dayStart
			end := dayStart.Add(24*time.Hour - time.Second)
			return &start, &end, nil
		case "yesterday":
			start := dayStart.AddDate(0, 0, -1)
			end := dayStart.Add(-time.Second)
			return &start, &end, nil
		case "last_7d":
			start := dayStart.AddDate(0, 0, -6)
			end := dayStart.Add(24*time.Hour - time.Second)
			return &start, &end, nil
		case "last_30d":
			start := dayStart.AddDate(0, 0, -29)
			end := dayStart.Add(24*time.Hour - time.Second)
			return &start, &end, nil
		case "this_month":
			start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			end := dayStart.Add(24*time.Hour - time.Second)
			return &start, &end, nil
		default:
			return nil, nil, fmt.Errorf("unsupported time_range: %s", timeRange)
		}
	}

	var startDate *time.Time
	var endDate *time.Time
	if strings.TrimSpace(dateFrom) != "" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(dateFrom))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid date_from, expected YYYY-MM-DD")
		}
		startDate = &parsed
	}
	if strings.TrimSpace(dateTo) != "" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(dateTo))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid date_to, expected YYYY-MM-DD")
		}
		parsed = parsed.Add(24*time.Hour - time.Second)
		endDate = &parsed
	}
	return startDate, endDate, nil
}

func (s *Service) getCurrentEmployee(ctx context.Context, claims *auth.Claims) (*EmployeeSummary, error) {
	if claims == nil {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		return nil, fmt.Errorf("tenant_id missing in token")
	}

	const query = `
		SELECT
			e.id,
			e.tenant_id,
			COALESCE(t.name, '') AS tenant_name,
			COALESCE(
				NULLIF(NULLIF(e.full_name, 'unknown'), ''),
				NULLIF(NULLIF(e.name, 'unknown'), ''),
				NULLIF(e.username, ''),
				NULLIF(e.phone, ''),
				'未知员工'
			) AS full_name,
			COALESCE(e.phone, '') AS phone,
			e.department_id,
			NULLIF(d.name, '') AS department_name,
			COALESCE((
				SELECT lower(er.role_code)
				FROM inst_employee_roles er
				WHERE er.tenant_id = e.tenant_id
				  AND er.employee_id = e.id
				ORDER BY COALESCE(er.updated_at, er.created_at) DESC
				LIMIT 1
			), '') AS role_code,
			COALESCE((
				SELECT COALESCE(
					(
						SELECT ir.name
						FROM institution_roles ir
						WHERE ir.tenant_id = e.tenant_id
						  AND lower(ir.code) = lower(er.role_code)
						  AND ir.deleted_at IS NULL
						ORDER BY ir.id DESC
						LIMIT 1
					),
					(
						SELECT NULLIF(r.name_cn, '')
						FROM inst_roles r
						WHERE lower(r.code) = lower(er.role_code)
						ORDER BY r.code
						LIMIT 1
					),
					er.role_code
				)
				FROM inst_employee_roles er
				WHERE er.tenant_id = e.tenant_id
				  AND er.employee_id = e.id
				ORDER BY COALESCE(er.updated_at, er.created_at) DESC
				LIMIT 1
			), '') AS role_name
		FROM employees e
		JOIN tenants t ON t.id = e.tenant_id
		LEFT JOIN departments d ON d.id = e.department_id AND d.tenant_id = e.tenant_id
		WHERE e.id = $1
		  AND e.tenant_id = $2
		  AND e.deleted_at IS NULL
		  AND t.deleted_at IS NULL
	`

	var item EmployeeSummary
	err := s.pool.QueryRow(ctx, query, claims.UserID, *claims.TenantID).Scan(
		&item.ID,
		&item.TenantID,
		&item.TenantName,
		&item.FullName,
		&item.Phone,
		&item.DepartmentID,
		&item.DepartmentName,
		&item.RoleCode,
		&item.RoleName,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("employee not found")
		}
		return nil, err
	}

	return &item, nil
}

func ensureTaskOwnedByEmployee(task *recording.TaskResponse, me *EmployeeSummary) error {
	if task == nil {
		return fmt.Errorf("task not found")
	}
	if task.TenantID != me.TenantID {
		return fmt.Errorf("task not found")
	}
	if task.AssignedTo == nil || *task.AssignedTo != me.ID {
		return fmt.Errorf("task not found")
	}
	return nil
}

func recordingScopeForRole(roleCode string) recording.RecordingScope {
	role := strings.ToLower(strings.TrimSpace(roleCode))
	switch {
	case strings.Contains(role, "doctor"):
		return recording.RecordingScopeDoctor
	case strings.Contains(role, "therapist"):
		return recording.RecordingScopeTherapist
	case strings.Contains(role, "frontdesk"):
		return recording.RecordingScopeFrontdesk
	default:
		return recording.RecordingScopeConsultant
	}
}

func parseRFC3339(value string) time.Time {
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return ts
}

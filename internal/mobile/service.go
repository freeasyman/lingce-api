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

	todoTasks, todoTotal, err := s.ListTasks(ctx, claims, "todo", 1, 3)
	if err != nil {
		return nil, err
	}
	doneTotal, err := s.countTasks(ctx, claims, recording.TaskStatusCompleted)
	if err != nil {
		return nil, err
	}
	recordings, recordingTotal, err := s.ListRecordings(ctx, claims, 1, 3)
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

func (s *Service) ListTasks(ctx context.Context, claims *auth.Claims, status string, page, pageSize int) ([]*recording.TaskResponse, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return nil, 0, err
	}

	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "todo":
		return s.listTodoTasks(ctx, me, page, pageSize)
	case "done":
		done := recording.TaskStatusCompleted
		return s.recordingService.ListRecordingTasks(ctx, recording.TaskListRequest{
			TenantID:   &me.TenantID,
			AssignedTo: &me.ID,
			Status:     &done,
			Page:       page,
			PageSize:   pageSize,
		})
	default:
		return nil, 0, fmt.Errorf("unsupported task status: %s", status)
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

func (s *Service) ListRecordings(ctx context.Context, claims *auth.Claims, page, pageSize int) ([]*recording.RecordingResponse, int, error) {
	me, err := s.getCurrentEmployee(ctx, claims)
	if err != nil {
		return nil, 0, err
	}
	scope := recordingScopeForRole(me.RoleCode)
	includeShort := false
	return s.recordingService.ListRecordings(ctx, recording.RecordingListRequest{
		TenantID:     me.TenantID,
		EmployeeID:   &me.ID,
		Scope:        &scope,
		IncludeShort: &includeShort,
		Page:         page,
		PageSize:     pageSize,
	})
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

func (s *Service) listTodoTasks(ctx context.Context, me *EmployeeSummary, page, pageSize int) ([]*recording.TaskResponse, int, error) {
	pendingTasks, pendingTotal, err := s.fetchAllTasksByStatus(ctx, me, recording.TaskStatusPending)
	if err != nil {
		return nil, 0, err
	}
	assignedTasks, assignedTotal, err := s.fetchAllTasksByStatus(ctx, me, recording.TaskStatusAssigned)
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
	start := (page - 1) * pageSize
	if start >= len(merged) {
		return []*recording.TaskResponse{}, total, nil
	}
	end := start + pageSize
	if end > len(merged) {
		end = len(merged)
	}
	return merged[start:end], total, nil
}

func (s *Service) fetchAllTasksByStatus(ctx context.Context, me *EmployeeSummary, status recording.TaskStatus) ([]*recording.TaskResponse, int, error) {
	all := make([]*recording.TaskResponse, 0)
	page := 1
	pageSize := 100
	total := 0

	for {
		items, itemTotal, err := s.recordingService.ListRecordingTasks(ctx, recording.TaskListRequest{
			TenantID:   &me.TenantID,
			AssignedTo: &me.ID,
			Status:     &status,
			Page:       page,
			PageSize:   pageSize,
		})
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
				ORDER BY er.created_at DESC
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
				ORDER BY er.created_at DESC
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

package patientops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type TaskAssignee struct {
	EmployeeID int64  `json:"employee_id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
}

type AssignTaskRequest struct {
	EmployeeID int64 `json:"employee_id"`
}

type BulkRescheduleRequest struct {
	TaskIDs []int64 `json:"task_ids"`
	DueAt   string  `json:"due_at"`
}

func (s *Service) ListTaskAssignees(ctx context.Context, tenantID int64) ([]TaskAssignee, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(NULLIF(name,''), NULLIF(username,''), '未命名员工'), COALESCE(role,'')
		FROM employees
		WHERE tenant_id=$1 AND deleted_at IS NULL
		  AND is_active::text IN ('1','t','true','TRUE','yes')
		ORDER BY id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list task assignees: %w", err)
	}
	defer rows.Close()
	items := make([]TaskAssignee, 0)
	for rows.Next() {
		var item TaskAssignee
		if err := rows.Scan(&item.EmployeeID, &item.Name, &item.Role); err != nil {
			return nil, fmt.Errorf("scan task assignee: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) AssignPatientTask(ctx context.Context, tenantID, actorID, taskID, targetID int64, actorAdmin bool) error {
	if targetID <= 0 {
		return fmt.Errorf("employee_id is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin assign task: %w", err)
	}
	defer tx.Rollback(ctx)
	var ownerID int64
	var status string
	err = tx.QueryRow(ctx, `
		SELECT employee_id, status FROM patient_operation_tasks
		WHERE id=$1 AND tenant_id=$2
		FOR UPDATE
	`, taskID, tenantID).Scan(&ownerID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("task not found")
	}
	if err != nil {
		return fmt.Errorf("load task for assign: %w", err)
	}
	if status == "已完成" || status == "已取消" {
		return fmt.Errorf("completed or canceled task cannot be assigned")
	}
	if !actorAdmin && ownerID != actorID {
		allowed, err := s.HasPatientOpsPermission(ctx, tenantID, actorID, PatientOpsAbilityAssign)
		if err != nil {
			return fmt.Errorf("check assign permission: %w", err)
		}
		if !allowed {
			return fmt.Errorf("task assign permission required")
		}
	}
	var targetExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM employees WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL AND is_active::text IN ('1','t','true','TRUE','yes'))
	`, targetID, tenantID).Scan(&targetExists); err != nil {
		return fmt.Errorf("check assignee: %w", err)
	}
	if !targetExists {
		return fmt.Errorf("assignee not found or inactive")
	}
	if _, err := tx.Exec(ctx, `UPDATE patient_operation_tasks SET employee_id=$1, updated_at=NOW() WHERE id=$2 AND tenant_id=$3`, targetID, taskID, tenantID); err != nil {
		return fmt.Errorf("assign task: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit assign task: %w", err)
	}
	return nil
}

func (s *Service) BulkReschedulePatientTasks(ctx context.Context, tenantID, actorID int64, actorAdmin bool, req BulkRescheduleRequest) (int, error) {
	if len(req.TaskIDs) == 0 {
		return 0, fmt.Errorf("task_ids is required")
	}
	if len(req.TaskIDs) > 200 {
		return 0, fmt.Errorf("at most 200 tasks can be rescheduled at once")
	}
	due, err := parsePatientDueAt(req.DueAt, time.Now())
	if err != nil {
		return 0, fmt.Errorf("invalid due_at: %w", err)
	}
	allowedAll := actorAdmin
	if !allowedAll {
		allowed, err := s.HasPatientOpsPermission(ctx, tenantID, actorID, PatientOpsAbilityAssign)
		if err != nil {
			return 0, fmt.Errorf("check assign permission: %w", err)
		}
		allowedAll = allowed
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin bulk reschedule: %w", err)
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `
		UPDATE patient_operation_tasks
		SET due_at=$1, updated_at=NOW()
		WHERE tenant_id=$2 AND id = ANY($3) AND status NOT IN ('已完成','已取消')
		  AND (employee_id=$4 OR $5::boolean)
	`, due, tenantID, req.TaskIDs, actorID, allowedAll)
	if err != nil {
		return 0, fmt.Errorf("bulk reschedule tasks: %w", err)
	}
	affected := int(result.RowsAffected())
	if affected != len(req.TaskIDs) {
		return 0, fmt.Errorf("some tasks are not found, not yours, or already completed")
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit bulk reschedule: %w", err)
	}
	return affected, nil
}

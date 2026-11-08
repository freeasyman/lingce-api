package dealexpert

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// AnalysisAction 是卡片回填记录：咨询师对“现在做什么”的处理结果。
type AnalysisAction struct {
	ID              int64      `json:"id"`
	TenantID        int64      `json:"tenant_id"`
	RecordingID     int64      `json:"recording_id"`
	ActionType      string     `json:"action_type"`
	Note            string     `json:"note"`
	ActorEmployeeID int64      `json:"actor_employee_id"`
	ActorName       string     `json:"actor_name"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
}

const (
	AnalysisActionCompleted      = "completed"
	AnalysisActionNotApplicable  = "not_applicable"
	AnalysisActionNeedSupervisor = "need_supervisor"
)

// ensureAnalysisActionsTable 懒建表，避免依赖迁移脚本在开发环境手动执行。
func (s *Store) EnsureAnalysisActionsTable(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS analysis_actions (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			action_type TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			actor_employee_id BIGINT NOT NULL,
			actor_name TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_analysis_actions_recording
			ON analysis_actions (tenant_id, recording_id, id DESC);
	`)
	return err
}

func (s *Store) ListAnalysisActions(ctx context.Context, tenantID, recordingID int64) ([]*AnalysisAction, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, recording_id, action_type, note, actor_employee_id, actor_name, created_at
		FROM analysis_actions
		WHERE tenant_id = $1 AND recording_id = $2
		ORDER BY id DESC
		LIMIT 50
	`, tenantID, recordingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*AnalysisAction, 0, 16)
	for rows.Next() {
		var item AnalysisAction
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RecordingID, &item.ActionType, &item.Note,
			&item.ActorEmployeeID, &item.ActorName, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &item)
	}
	return out, rows.Err()
}

func (s *Store) CreateAnalysisAction(ctx context.Context, tenantID, recordingID, actorEmployeeID int64, actorName, actionType, note string) (*AnalysisAction, error) {
	var item AnalysisAction
	err := s.pool.QueryRow(ctx, `
		INSERT INTO analysis_actions (tenant_id, recording_id, action_type, note, actor_employee_id, actor_name)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, recording_id, action_type, note, actor_employee_id, actor_name, created_at
	`, tenantID, recordingID, actionType, strings.TrimSpace(note), actorEmployeeID, actorName).Scan(
		&item.ID, &item.TenantID, &item.RecordingID, &item.ActionType, &item.Note,
		&item.ActorEmployeeID, &item.ActorName, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (h *Handler) ListAnalysisActions(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, recordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	items, err := h.service.ListAnalysisActions(r.Context(), scope.TenantID, recordingID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) CreateAnalysisAction(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	if accessErr := h.checkRecordingAccess(r.Context(), scope, recordingID); accessErr != nil {
		writeTenantError(w, accessErr)
		return
	}
	var req struct {
		ActionType string `json:"action_type"`
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	actionType := strings.TrimSpace(req.ActionType)
	switch actionType {
	case AnalysisActionCompleted, AnalysisActionNotApplicable, AnalysisActionNeedSupervisor:
	default:
		httputil.WriteBadRequest(w, "invalid action_type")
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	actorID, actorName := int64(0), ""
	if claims != nil {
		actorID = claims.UserID
	}
	actorName = h.service.ActorName(r.Context(), scope.TenantID, actorID)
	item, err := h.service.CreateAnalysisAction(r.Context(), scope.TenantID, recordingID, actorID, actorName, actionType, req.Note)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// MyAction 是“我的批复/回填”列表项：我处理过的卡片动作。
type MyAction struct {
	ID           int64      `json:"id"`
	RecordingID  int64      `json:"recording_id"`
	CustomerName string     `json:"customer_name"`
	ActionType   string     `json:"action_type"`
	Note         string     `json:"note"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
}

func (s *Store) ListMyAnalysisActions(ctx context.Context, tenantID, actorEmployeeID int64) ([]*MyAction, error) {
	if err := s.EnsureAnalysisActionsTable(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.recording_id, COALESCE(c.name, ''), a.action_type, a.note, a.created_at
		FROM analysis_actions a
		LEFT JOIN recordings r ON r.id = a.recording_id AND r.tenant_id = a.tenant_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE a.tenant_id = $1 AND a.actor_employee_id = $2
		ORDER BY a.id DESC
		LIMIT 100
	`, tenantID, actorEmployeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*MyAction, 0, 32)
	for rows.Next() {
		var item MyAction
		if err := rows.Scan(&item.ID, &item.RecordingID, &item.CustomerName, &item.ActionType, &item.Note, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &item)
	}
	return out, rows.Err()
}

func (h *Handler) ListMyActions(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserID <= 0 {
		httputil.WriteSuccess(w, map[string]any{"items": []*MyAction{}})
		return
	}
	items, err := h.service.ListMyAnalysisActions(r.Context(), scope.TenantID, claims.UserID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

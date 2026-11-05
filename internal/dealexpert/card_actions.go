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

// DealCardAction 是卡片回填记录：咨询师对“现在做什么”的处理结果。
type DealCardAction struct {
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
	DealActionCompleted      = "completed"
	DealActionNotApplicable  = "not_applicable"
	DealActionNeedSupervisor = "need_supervisor"
)

// ensureCardActionsTable 懒建表，避免依赖迁移脚本在开发环境手动执行。
func (s *Store) EnsureCardActionsTable(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_card_actions (
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
		CREATE INDEX IF NOT EXISTS idx_deal_card_actions_recording
			ON deal_card_actions (tenant_id, recording_id, id DESC);
	`)
	return err
}

func (s *Store) ListCardActions(ctx context.Context, tenantID, recordingID int64) ([]*DealCardAction, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, recording_id, action_type, note, actor_employee_id, actor_name, created_at
		FROM deal_card_actions
		WHERE tenant_id = $1 AND recording_id = $2
		ORDER BY id DESC
		LIMIT 50
	`, tenantID, recordingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*DealCardAction, 0, 16)
	for rows.Next() {
		var item DealCardAction
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RecordingID, &item.ActionType, &item.Note,
			&item.ActorEmployeeID, &item.ActorName, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &item)
	}
	return out, rows.Err()
}

func (s *Store) CreateCardAction(ctx context.Context, tenantID, recordingID, actorEmployeeID int64, actorName, actionType, note string) (*DealCardAction, error) {
	var item DealCardAction
	err := s.pool.QueryRow(ctx, `
		INSERT INTO deal_card_actions (tenant_id, recording_id, action_type, note, actor_employee_id, actor_name)
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

func (h *Handler) ListCardActions(w http.ResponseWriter, r *http.Request) {
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
	items, err := h.service.ListCardActions(r.Context(), scope.TenantID, recordingID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) CreateCardAction(w http.ResponseWriter, r *http.Request) {
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
	case DealActionCompleted, DealActionNotApplicable, DealActionNeedSupervisor:
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
	item, err := h.service.CreateCardAction(r.Context(), scope.TenantID, recordingID, actorID, actorName, actionType, req.Note)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

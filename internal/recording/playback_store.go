package recording

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const defaultPlaybackWindowDays = 90

// RecordingPlaybackConfig 是机构级的录音播放开关与窗口。
type RecordingPlaybackConfig struct {
	PlaybackEnabled    bool
	PlaybackWindowDays int
}

// PlaybackGrant 是窗口外/跨人调取的限时授权。
type PlaybackGrant struct {
	ID           int64
	RecordingID  int64
	RequesterID  int64
	AuthorizerID int64
	ExpiresAt    time.Time
}

// GetRecordingPlaybackConfig 读取机构配置；未配置时按「开启 + 90 天」生效。
func (s *Store) GetRecordingPlaybackConfig(ctx context.Context, tenantID int64) (*RecordingPlaybackConfig, error) {
	const query = `
		SELECT playback_enabled, playback_window_days
		FROM recording_playback_configs
		WHERE tenant_id = $1
		LIMIT 1
	`
	cfg := &RecordingPlaybackConfig{PlaybackEnabled: true, PlaybackWindowDays: defaultPlaybackWindowDays}
	if err := s.pool.QueryRow(ctx, query, tenantID).Scan(&cfg.PlaybackEnabled, &cfg.PlaybackWindowDays); err != nil {
		if err == pgx.ErrNoRows {
			return cfg, nil
		}
		return nil, err
	}
	return cfg, nil
}

// UpsertRecordingPlaybackConfig 写入或更新机构级录音播放开关与窗口。
func (s *Store) UpsertRecordingPlaybackConfig(ctx context.Context, tenantID int64, enabled bool, windowDays int) error {
	const query = `
		INSERT INTO recording_playback_configs (tenant_id, playback_enabled, playback_window_days, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (tenant_id)
		DO UPDATE SET playback_enabled = EXCLUDED.playback_enabled, playback_window_days = EXCLUDED.playback_window_days, updated_at = NOW()
	`
	if _, err := s.pool.Exec(ctx, query, tenantID, enabled, windowDays); err != nil {
		return err
	}
	return nil
}

// InsertRecordingPlayLog 记录一次成功的播放地址获取（留痕）。
func (s *Store) InsertRecordingPlayLog(ctx context.Context, recordingID, tenantID, employeeID int64, reason string, authorizerID *int64) error {
	const query = `
		INSERT INTO recording_play_logs (recording_id, tenant_id, employee_id, reason, authorizer_id)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := s.pool.Exec(ctx, query, recordingID, tenantID, employeeID, reason, authorizerID); err != nil {
		return err
	}
	return nil
}

// InsertRecordingPlaybackGrant 写入一条限时授权（轻量版：申请人自审批，authorizer=requester）。
func (s *Store) InsertRecordingPlaybackGrant(ctx context.Context, recordingID, tenantID, requesterID int64, reason string, expiresAt time.Time) error {
	const query = `
		INSERT INTO recording_playback_grants (recording_id, tenant_id, requester_id, authorizer_id, reason, expires_at)
		VALUES ($1, $2, $3, $3, $4, $5)
	`
	if _, err := s.pool.Exec(ctx, query, recordingID, tenantID, requesterID, reason, expiresAt); err != nil {
		return err
	}
	return nil
}

// GetActivePlaybackGrant 返回该录音、该申请人尚未过期的授权；无则返回 nil。
func (s *Store) GetActivePlaybackGrant(ctx context.Context, recordingID, requesterID int64) (*PlaybackGrant, error) {
	const query = `
		SELECT id, recording_id, requester_id, authorizer_id, expires_at
		FROM recording_playback_grants
		WHERE recording_id = $1 AND requester_id = $2 AND expires_at > NOW()
		ORDER BY expires_at DESC
		LIMIT 1
	`
	var g PlaybackGrant
	if err := s.pool.QueryRow(ctx, query, recordingID, requesterID).Scan(&g.ID, &g.RecordingID, &g.RequesterID, &g.AuthorizerID, &g.ExpiresAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &g, nil
}

// PlaybackListener 是"谁被授权听谁"的一条授权记录。
type PlaybackListener struct {
	OwnerEmployeeID    int64
	ListenerEmployeeID int64
	IsActive           bool
}

// EmployeeHasEmrAbility 判断员工是否具备某 EMR 能力（emr_admin 角色视为全能力）。
func (s *Store) EmployeeHasEmrAbility(ctx context.Context, tenantID, employeeID int64, ability string) (bool, error) {
	const query = `
		SELECT 1
		FROM emr_permission_assignments
		WHERE tenant_id = $1 AND employee_id = $2 AND enabled = TRUE
		  AND (emr_role_code = 'emr_admin' OR EXISTS (
			SELECT 1 FROM jsonb_array_elements_text(abilities_json) AS t(v) WHERE t.v = $3
		  ))
		LIMIT 1
	`
	var found int
	if err := s.pool.QueryRow(ctx, query, tenantID, employeeID, ability).Scan(&found); err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return found == 1, nil
}

// IsPlaybackListener 判断 listener 是否被授权听 owner 的录音。
func (s *Store) IsPlaybackListener(ctx context.Context, tenantID, ownerEmployeeID, listenerEmployeeID int64) (bool, error) {
	const query = `
		SELECT 1
		FROM recording_playback_listeners
		WHERE tenant_id = $1
		  AND owner_employee_id = $2
		  AND listener_employee_id = $3
		  AND is_active = TRUE
		LIMIT 1
	`
	var found int
	if err := s.pool.QueryRow(ctx, query, tenantID, ownerEmployeeID, listenerEmployeeID).Scan(&found); err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return found == 1, nil
}

// ListPlaybackListeners 返回该机构全部有效授权记录。
func (s *Store) ListPlaybackListeners(ctx context.Context, tenantID int64) ([]PlaybackListener, error) {
	const query = `
		SELECT owner_employee_id, listener_employee_id, is_active
		FROM recording_playback_listeners
		WHERE tenant_id = $1
		ORDER BY owner_employee_id, listener_employee_id
	`
	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaybackListener
	for rows.Next() {
		var item PlaybackListener
		if err := rows.Scan(&item.OwnerEmployeeID, &item.ListenerEmployeeID, &item.IsActive); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpsertPlaybackListener 打勾/取消打勾一条授权记录。
func (s *Store) UpsertPlaybackListener(ctx context.Context, tenantID, ownerEmployeeID, listenerEmployeeID int64, active bool, operatorID int64) error {
	const query = `
		INSERT INTO recording_playback_listeners (tenant_id, owner_employee_id, listener_employee_id, is_active, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (tenant_id, owner_employee_id, listener_employee_id)
		DO UPDATE SET is_active = EXCLUDED.is_active, updated_by = EXCLUDED.updated_by, updated_at = NOW()
	`
	if _, err := s.pool.Exec(ctx, query, tenantID, ownerEmployeeID, listenerEmployeeID, active, operatorID); err != nil {
		return err
	}
	return nil
}

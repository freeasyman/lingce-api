package dealexpush

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrInvalidPushToken 表示 Expo push token 为空、过长或包含空白。
	ErrInvalidPushToken = errors.New("invalid expo push token")
	// ErrInvalidPushPlatform 表示 platform 不是 ios/android。
	ErrInvalidPushPlatform = errors.New("platform must be ios or android")
	// ErrDeviceStoreUnavailable 表示服务未配置设备存储（通常只在单测中出现）。
	ErrDeviceStoreUnavailable = errors.New("push device store unavailable")
)

// PushDevice 是 mobile_push_devices 的一行（user_id 即员工 ID）。
type PushDevice struct {
	ID          int64
	TenantID    int64
	EmployeeID  int64
	Platform    string
	DeviceID    string
	Token       string
	DeviceName  string
	AppVersion  string
	IsActive    bool
	LastError   string
	LastSentAt  *time.Time
	Invalidated *time.Time
}

// RegisterPushDeviceRequest 是设备注册请求体；app_version 为兼容字段。
type RegisterPushDeviceRequest struct {
	Token      string `json:"token"`
	Platform   string `json:"platform"`
	DeviceName string `json:"device_name"`
	AppVersion string `json:"app_version"`
}

// PushDeviceStore 抽象设备表读写，便于用 fake 做单测。
type PushDeviceStore interface {
	Upsert(ctx context.Context, device PushDevice) (*PushDevice, error)
	Deactivate(ctx context.Context, tenantID, employeeID int64, token string) error
	ActiveTokens(ctx context.Context, tenantID, employeeID int64) ([]PushDevice, error)
	MarkSent(ctx context.Context, tenantID, employeeID int64, token string, at time.Time) error
	MarkFailed(ctx context.Context, tenantID, employeeID int64, token, reason string) error
	MarkInvalid(ctx context.Context, tenantID, employeeID int64, token, reason string) error
}

type postgresPushDeviceStore struct {
	pool *pgxpool.Pool
}

func newPostgresPushDeviceStore(pool *pgxpool.Pool) PushDeviceStore {
	return &postgresPushDeviceStore{pool: pool}
}

func (s *postgresPushDeviceStore) Upsert(ctx context.Context, device PushDevice) (*PushDevice, error) {
	if s == nil || s.pool == nil {
		return nil, ErrDeviceStoreUnavailable
	}
	// 先按 tenant+employee+token 更新。历史表同时存在 expo_push_token 与
	// (user_id, device_id) 两个唯一索引，单条 INSERT ... ON CONFLICT (token)
	// 在第二次注册时仍可能触发 (user_id, device_id) 冲突，因此先更新再插入。
	updateSQL := `
		UPDATE mobile_push_devices
		SET platform = $4,
		    device_id = $5,
		    device_name = $6,
		    app_version = COALESCE(NULLIF($7, ''), app_version),
		    is_active = TRUE,
		    last_error = '',
		    invalidated_at = NULL,
		    last_seen_at = NOW(),
		    updated_at = NOW()
		WHERE tenant_id = $1 AND user_id = $2 AND expo_push_token = $3
		RETURNING id, tenant_id, user_id, platform, device_id, expo_push_token,
		          COALESCE(app_version, ''), device_name, is_active, last_error, last_sent_at, invalidated_at`
	insertSQL := `
		INSERT INTO mobile_push_devices
		  (user_id, tenant_id, platform, device_id, expo_push_token, app_version,
		   device_name, is_active, last_error, invalidated_at, last_seen_at, created_at, updated_at)
		VALUES ($2,$1,$4,$5,$3,$7,$6,TRUE,'',NULL,NOW(),NOW(),NOW())
		ON CONFLICT (expo_push_token) DO UPDATE SET
		  user_id = EXCLUDED.user_id,
		  tenant_id = EXCLUDED.tenant_id,
		  platform = EXCLUDED.platform,
		  device_id = EXCLUDED.device_id,
		  device_name = EXCLUDED.device_name,
		  app_version = COALESCE(NULLIF(EXCLUDED.app_version, ''), mobile_push_devices.app_version),
		  is_active = TRUE,
		  last_error = '',
		  invalidated_at = NULL,
		  last_seen_at = NOW(),
		  updated_at = NOW()
		RETURNING id, tenant_id, user_id, platform, device_id, expo_push_token,
		          COALESCE(app_version, ''), device_name, is_active, last_error, last_sent_at, invalidated_at`

	args := []any{device.TenantID, device.EmployeeID, device.Token, device.Platform,
		device.DeviceID, device.DeviceName, device.AppVersion}
	for attempt := 0; attempt < 2; attempt++ {
		updated, err := scanPushDevice(s.pool.QueryRow(ctx, updateSQL, args...))
		if err == nil {
			return updated, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("update push device: %w", err)
		}

		inserted, err := scanPushDevice(s.pool.QueryRow(ctx, insertSQL, args...))
		if err == nil {
			return inserted, nil
		}
		if isUniqueViolation(err) {
			// 并发注册同一 token：重试一次 update。
			continue
		}
		return nil, fmt.Errorf("insert push device: %w", err)
	}
	return nil, fmt.Errorf("upsert push device: too many concurrent conflicts")
}

func scanPushDevice(row pgx.Row) (*PushDevice, error) {
	var out PushDevice
	if err := row.Scan(&out.ID, &out.TenantID, &out.EmployeeID, &out.Platform, &out.DeviceID, &out.Token,
		&out.AppVersion, &out.DeviceName, &out.IsActive, &out.LastError, &out.LastSentAt, &out.Invalidated); err != nil {
		return nil, err
	}
	return &out, nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

func (s *postgresPushDeviceStore) Deactivate(ctx context.Context, tenantID, employeeID int64, token string) error {
	if s == nil || s.pool == nil {
		return ErrDeviceStoreUnavailable
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE mobile_push_devices
		SET is_active = FALSE, last_error = 'unregistered', updated_at = NOW()
		WHERE tenant_id = $1 AND user_id = $2 AND expo_push_token = $3
	`, tenantID, employeeID, token)
	if err != nil {
		return fmt.Errorf("deactivate push device: %w", err)
	}
	return nil
}

func (s *postgresPushDeviceStore) ActiveTokens(ctx context.Context, tenantID, employeeID int64) ([]PushDevice, error) {
	if s == nil || s.pool == nil {
		return nil, ErrDeviceStoreUnavailable
	}
	rows, err := s.pool.Query(ctx, `
		SELECT expo_push_token
		FROM mobile_push_devices
		WHERE tenant_id = $1 AND user_id = $2 AND is_active = TRUE AND expo_push_token <> ''
		ORDER BY id
	`, tenantID, employeeID)
	if err != nil {
		return nil, fmt.Errorf("list active push devices: %w", err)
	}
	defer rows.Close()
	devices := make([]PushDevice, 0)
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, fmt.Errorf("scan active push device: %w", err)
		}
		devices = append(devices, PushDevice{TenantID: tenantID, EmployeeID: employeeID, Token: token})
	}
	return devices, rows.Err()
}

func (s *postgresPushDeviceStore) MarkSent(ctx context.Context, tenantID, employeeID int64, token string, at time.Time) error {
	if s == nil || s.pool == nil {
		return ErrDeviceStoreUnavailable
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE mobile_push_devices
		SET last_sent_at = $4, last_error = '', updated_at = NOW()
		WHERE tenant_id = $1 AND user_id = $2 AND expo_push_token = $3
	`, tenantID, employeeID, token, at)
	if err != nil {
		return fmt.Errorf("mark push device sent: %w", err)
	}
	return nil
}

func (s *postgresPushDeviceStore) MarkFailed(ctx context.Context, tenantID, employeeID int64, token, reason string) error {
	if s == nil || s.pool == nil {
		return ErrDeviceStoreUnavailable
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE mobile_push_devices
		SET last_error = $4, updated_at = NOW()
		WHERE tenant_id = $1 AND user_id = $2 AND expo_push_token = $3
	`, tenantID, employeeID, token, truncateString(reason, 500))
	if err != nil {
		return fmt.Errorf("mark push device failed: %w", err)
	}
	return nil
}

func (s *postgresPushDeviceStore) MarkInvalid(ctx context.Context, tenantID, employeeID int64, token, reason string) error {
	if s == nil || s.pool == nil {
		return ErrDeviceStoreUnavailable
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE mobile_push_devices
		SET is_active = FALSE, last_error = $4, invalidated_at = NOW(), updated_at = NOW()
		WHERE tenant_id = $1 AND user_id = $2 AND expo_push_token = $3
	`, tenantID, employeeID, token, truncateString(reason, 500))
	if err != nil {
		return fmt.Errorf("invalidate push device: %w", err)
	}
	return nil
}

// RegisterDevice 幂等注册（tenant + employee + token）并返回设备记录。
func (s *Service) RegisterDevice(ctx context.Context, tenantID, employeeID int64, req RegisterPushDeviceRequest) (*PushDevice, error) {
	if tenantID <= 0 || employeeID <= 0 {
		return nil, fmt.Errorf("tenant_id and employee_id are required")
	}
	if s == nil || s.devices == nil {
		return nil, ErrDeviceStoreUnavailable
	}

	token := strings.TrimSpace(req.Token)
	if token == "" || len([]rune(token)) > 255 || strings.ContainsAny(token, " \t\r\n") {
		return nil, ErrInvalidPushToken
	}
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	if platform != "ios" && platform != "android" {
		return nil, ErrInvalidPushPlatform
	}

	device := PushDevice{
		TenantID:   tenantID,
		EmployeeID: employeeID,
		Platform:   platform,
		DeviceID:   deviceIDForToken(token),
		Token:      token,
		DeviceName: truncateString(strings.TrimSpace(req.DeviceName), 120),
		AppVersion: truncateString(strings.TrimSpace(req.AppVersion), 64),
		IsActive:   true,
	}
	saved, err := s.devices.Upsert(ctx, device)
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// UnregisterDevice 注销当前员工的一条 token；不存在也返回成功（幂等）。
func (s *Service) UnregisterDevice(ctx context.Context, tenantID, employeeID int64, token string) error {
	if tenantID <= 0 || employeeID <= 0 {
		return fmt.Errorf("tenant_id and employee_id are required")
	}
	if s == nil || s.devices == nil {
		return ErrDeviceStoreUnavailable
	}
	token = strings.TrimSpace(token)
	if token == "" || len([]rune(token)) > 255 {
		return ErrInvalidPushToken
	}
	return s.devices.Deactivate(ctx, tenantID, employeeID, token)
}

// deviceIDForToken 生成稳定的设备 ID（兼容历史 device_id varchar(120) 限制）。
func deviceIDForToken(token string) string {
	if len(token) <= 120 {
		return token
	}
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])[:32]
}

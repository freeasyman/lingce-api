package support

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// GetTenantNameMap returns tenant_id -> tenant_name map.
func (s *Store) GetTenantNameMap(ctx context.Context, tenantIDs []int64) (map[int64]string, error) {
	result := make(map[int64]string, len(tenantIDs))
	if len(tenantIDs) == 0 {
		return result, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(NULLIF(name, ''), CONCAT('租户#', id::text)) AS tenant_name
		FROM tenants
		WHERE id = ANY($1) AND deleted_at IS NULL
	`, tenantIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query tenant names: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("failed to scan tenant name: %w", err)
		}
		result[id] = name
	}
	for _, id := range tenantIDs {
		if _, ok := result[id]; !ok {
			result[id] = fmt.Sprintf("租户#%d", id)
		}
	}
	return result, nil
}

// Notification Methods

// RegisterDeviceToken registers a device token for push notifications
func (s *Store) RegisterDeviceToken(ctx context.Context, userID int64, userType string, req RegisterDeviceTokenRequest) (*DeviceToken, error) {
	// Check if token already exists
	var existingID int64
	checkQuery := `SELECT id FROM device_tokens WHERE token = $1 AND user_id = $2 AND user_type = $3`
	err := s.pool.QueryRow(ctx, checkQuery, req.Token, userID, userType).Scan(&existingID)

	if err == nil {
		// Token exists, update it
		updateQuery := `
			UPDATE device_tokens
			SET device_model = $1, app_version = $2, is_active = true, last_used_at = NOW(), updated_at = NOW()
			WHERE id = $3
			RETURNING id, user_id, user_type, token, platform, device_model, app_version, is_active, last_used_at, created_at, updated_at
		`
		var dt DeviceToken
		err = s.pool.QueryRow(ctx, updateQuery, req.DeviceModel, req.AppVersion, existingID).Scan(
			&dt.ID, &dt.UserID, &dt.UserType, &dt.Token, &dt.Platform, &dt.DeviceModel, &dt.AppVersion,
			&dt.IsActive, &dt.LastUsedAt, &dt.CreatedAt, &dt.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to update device token: %w", err)
		}
		return &dt, nil
	}

	// Token doesn't exist, create new
	insertQuery := `
		INSERT INTO device_tokens (user_id, user_type, token, platform, device_model, app_version, is_active, last_used_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, NOW(), NOW(), NOW())
		RETURNING id, user_id, user_type, token, platform, device_model, app_version, is_active, last_used_at, created_at, updated_at
	`

	var dt DeviceToken
	err = s.pool.QueryRow(ctx, insertQuery, userID, userType, req.Token, req.Platform, req.DeviceModel, req.AppVersion).Scan(
		&dt.ID, &dt.UserID, &dt.UserType, &dt.Token, &dt.Platform, &dt.DeviceModel, &dt.AppVersion,
		&dt.IsActive, &dt.LastUsedAt, &dt.CreatedAt, &dt.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to register device token: %w", err)
	}

	return &dt, nil
}

// UnregisterDeviceToken unregisters a device token
func (s *Store) UnregisterDeviceToken(ctx context.Context, userID int64, token string) error {
	query := `
		UPDATE device_tokens
		SET is_active = false, updated_at = NOW()
		WHERE user_id = $1 AND token = $2
	`

	result, err := s.pool.Exec(ctx, query, userID, token)
	if err != nil {
		return fmt.Errorf("failed to unregister device token: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("device token not found")
	}

	return nil
}

// DeleteDeviceTokenByID unregisters a device token by ID.
func (s *Store) DeleteDeviceTokenByID(ctx context.Context, userID, tokenID int64) error {
	query := `
		UPDATE device_tokens
		SET is_active = false, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND is_active = true
	`

	result, err := s.pool.Exec(ctx, query, tokenID, userID)
	if err != nil {
		return fmt.Errorf("failed to unregister device token: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("device token not found")
	}

	return nil
}

// GetUserDeviceTokens retrieves device tokens for a user
func (s *Store) GetUserDeviceTokens(ctx context.Context, userID int64, userType string) ([]*DeviceToken, error) {
	query := `
		SELECT id, user_id, user_type, token, platform, device_model, app_version, is_active, last_used_at, created_at, updated_at
		FROM device_tokens
		WHERE user_id = $1 AND user_type = $2 AND is_active = true
		ORDER BY last_used_at DESC
	`

	rows, err := s.pool.Query(ctx, query, userID, userType)
	if err != nil {
		return nil, fmt.Errorf("failed to query device tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*DeviceToken
	for rows.Next() {
		var dt DeviceToken
		if err := rows.Scan(&dt.ID, &dt.UserID, &dt.UserType, &dt.Token, &dt.Platform, &dt.DeviceModel,
			&dt.AppVersion, &dt.IsActive, &dt.LastUsedAt, &dt.CreatedAt, &dt.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan device token: %w", err)
		}
		tokens = append(tokens, &dt)
	}

	return tokens, nil
}

// CreateNotification creates a notification
func (s *Store) CreateNotification(ctx context.Context, notification *Notification) error {
	query := `
		INSERT INTO notifications (tenant_id, user_id, user_type, title, content, type, related_id, related_type, is_read, extra_data, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, false, $9, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`

	return s.pool.QueryRow(ctx, query,
		notification.TenantID,
		notification.UserID,
		notification.UserType,
		notification.Title,
		notification.Content,
		notification.Type,
		notification.RelatedID,
		notification.RelatedType,
		notification.ExtraData,
	).Scan(&notification.ID, &notification.CreatedAt, &notification.UpdatedAt)
}

// ListNotifications retrieves a paginated list of notifications
func (s *Store) ListNotifications(ctx context.Context, req NotificationListRequest) ([]*Notification, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIndex))
	args = append(args, req.UserID)
	argIndex++

	conditions = append(conditions, fmt.Sprintf("user_type = $%d", argIndex))
	args = append(args, req.UserType)
	argIndex++

	if req.Type != nil {
		conditions = append(conditions, fmt.Sprintf("type = $%d", argIndex))
		args = append(args, *req.Type)
		argIndex++
	}

	if req.IsRead != nil {
		conditions = append(conditions, fmt.Sprintf("is_read = $%d", argIndex))
		args = append(args, *req.IsRead)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM notifications WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count notifications: %w", err)
	}

	// Query notifications
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, user_id, user_type, title, content, type, related_id, related_type, is_read, read_at, extra_data, created_at, updated_at
		FROM notifications
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.TenantID, &n.UserID, &n.UserType, &n.Title, &n.Content, &n.Type,
			&n.RelatedID, &n.RelatedType, &n.IsRead, &n.ReadAt, &n.ExtraData, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan notification: %w", err)
		}
		notifications = append(notifications, &n)
	}

	return notifications, total, nil
}

// GetNotificationByID retrieves a notification by ID.
func (s *Store) GetNotificationByID(ctx context.Context, notificationID, userID int64) (*Notification, error) {
	query := `
		SELECT id, user_id, user_type, title, content, type, is_read, related_id, related_type, extra_data, created_at, updated_at
		FROM notifications
		WHERE id = $1 AND user_id = $2
	`

	var notification Notification
	err := s.pool.QueryRow(ctx, query, notificationID, userID).Scan(
		&notification.ID, &notification.UserID, &notification.UserType, &notification.Title, &notification.Content,
		&notification.Type, &notification.IsRead, &notification.RelatedID, &notification.RelatedType, &notification.ExtraData,
		&notification.CreatedAt, &notification.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("notification not found")
	}

	return &notification, nil
}

// GetUnreadCount retrieves the count of unread notifications
func (s *Store) GetUnreadCount(ctx context.Context, userID int64, userType string) (int64, error) {
	query := `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND user_type = $2 AND is_read = false`

	var count int64
	err := s.pool.QueryRow(ctx, query, userID, userType).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get unread count: %w", err)
	}

	return count, nil
}

// MarkNotificationAsRead marks a notification as read
func (s *Store) MarkNotificationAsRead(ctx context.Context, notificationID, userID int64) error {
	query := `
		UPDATE notifications
		SET is_read = true, read_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND user_id = $2
	`

	result, err := s.pool.Exec(ctx, query, notificationID, userID)
	if err != nil {
		return fmt.Errorf("failed to mark notification as read: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("notification not found")
	}

	return nil
}

// MarkAllNotificationsAsRead marks all notifications as read for a user
func (s *Store) MarkAllNotificationsAsRead(ctx context.Context, userID int64, userType string) error {
	query := `
		UPDATE notifications
		SET is_read = true, read_at = NOW(), updated_at = NOW()
		WHERE user_id = $1 AND user_type = $2 AND is_read = false
	`

	_, err := s.pool.Exec(ctx, query, userID, userType)
	if err != nil {
		return fmt.Errorf("failed to mark all notifications as read: %w", err)
	}

	return nil
}

// Operation Log Methods

// CreateOperationLog creates an operation log entry
func (s *Store) CreateOperationLog(ctx context.Context, log *OperationLog) error {
	query := `
		INSERT INTO operation_logs (tenant_id, user_id, user_type, username, action, resource, resource_id, method, path, ip_address, user_agent, request_body, response_code, error_message, duration, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
		RETURNING id, created_at
	`

	return s.pool.QueryRow(ctx, query,
		log.TenantID, log.UserID, log.UserType, log.Username, log.Action, log.Resource, log.ResourceID,
		log.Method, log.Path, log.IPAddress, log.UserAgent, log.RequestBody, log.ResponseCode,
		log.ErrorMessage, log.Duration,
	).Scan(&log.ID, &log.CreatedAt)
}

// ListOperationLogs retrieves a paginated list of operation logs
func (s *Store) ListOperationLogs(ctx context.Context, req OperationLogListRequest) ([]*OperationLog, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.UserID != nil {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIndex))
		args = append(args, *req.UserID)
		argIndex++
	}

	if req.Action != nil {
		conditions = append(conditions, fmt.Sprintf("action ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Action+"%")
		argIndex++
	}

	if req.Resource != nil {
		conditions = append(conditions, fmt.Sprintf("resource ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Resource+"%")
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM operation_logs WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count operation logs: %w", err)
	}

	// Query logs
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, user_id, user_type, username, action, resource, resource_id, method, path, ip_address, user_agent, request_body, response_code, error_message, duration, created_at
		FROM operation_logs
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query operation logs: %w", err)
	}
	defer rows.Close()

	var logs []*OperationLog
	for rows.Next() {
		var l OperationLog
		if err := rows.Scan(&l.ID, &l.TenantID, &l.UserID, &l.UserType, &l.Username, &l.Action, &l.Resource,
			&l.ResourceID, &l.Method, &l.Path, &l.IPAddress, &l.UserAgent, &l.RequestBody, &l.ResponseCode,
			&l.ErrorMessage, &l.Duration, &l.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan operation log: %w", err)
		}
		logs = append(logs, &l)
	}

	return logs, total, nil
}

// GetOperationLogStats retrieves operation log statistics
func (s *Store) GetOperationLogStats(ctx context.Context, tenantID *int64, startDate, endDate *string) (*OperationLogStatsResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}

	if startDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *startDate)
		argIndex++
	}

	if endDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *endDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_logs,
			COUNT(CASE WHEN response_code < 400 THEN 1 END) as success_count,
			COUNT(CASE WHEN response_code >= 400 THEN 1 END) as failure_count,
			COALESCE(AVG(duration), 0) as avg_duration
		FROM operation_logs
		WHERE %s
	`, whereClause)

	var stats OperationLogStatsResponse
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&stats.TotalLogs, &stats.SuccessCount, &stats.FailureCount, &stats.AvgDuration,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get operation log stats: %w", err)
	}

	// Get top actions
	topActionsQuery := fmt.Sprintf(`
		SELECT action, COUNT(*) as count
		FROM operation_logs
		WHERE %s
		GROUP BY action
		ORDER BY count DESC
		LIMIT 10
	`, whereClause)

	rows, err := s.pool.Query(ctx, topActionsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query top actions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ac ActionCount
		if err := rows.Scan(&ac.Action, &ac.Count); err != nil {
			return nil, fmt.Errorf("failed to scan action count: %w", err)
		}
		stats.TopActions = append(stats.TopActions, ac)
	}

	return &stats, nil
}

// GetOperationLogByID retrieves an operation log by ID
func (s *Store) GetOperationLogByID(ctx context.Context, id int64) (*OperationLog, error) {
	query := `
		SELECT id, tenant_id, user_id, user_type, username, action, resource, resource_id, method, path, ip_address, user_agent, request_body, response_code, error_message, duration, created_at
		FROM operation_logs
		WHERE id = $1
	`

	var l OperationLog
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&l.ID, &l.TenantID, &l.UserID, &l.UserType, &l.Username, &l.Action, &l.Resource, &l.ResourceID,
		&l.Method, &l.Path, &l.IPAddress, &l.UserAgent, &l.RequestBody, &l.ResponseCode, &l.ErrorMessage,
		&l.Duration, &l.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("operation log not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query operation log: %w", err)
	}

	return &l, nil
}

// LLM Model Config Methods

// ListLLMModelConfigs retrieves a paginated list of LLM model configs
func (s *Store) ListLLMModelConfigs(ctx context.Context, req LLMModelConfigListRequest) ([]*LLMModelConfig, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.FunctionType != nil {
		conditions = append(conditions, fmt.Sprintf("function_type = $%d", argIndex))
		args = append(args, *req.FunctionType)
		argIndex++
	}

	if req.Provider != nil {
		conditions = append(conditions, fmt.Sprintf("provider = $%d", argIndex))
		args = append(args, *req.Provider)
		argIndex++
	}

	if req.IsActive != nil {
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM llm_model_configs WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count LLM model configs: %w", err)
	}

	// Query configs
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id,
		       COALESCE(tenant_id, 0),
		       COALESCE(model_code, ''),
		       COALESCE(function_type, 'general'),
		       COALESCE(model_name, ''),
		       COALESCE(provider, ''),
		       COALESCE(NULLIF(api_endpoint, ''), COALESCE(api_base_url, '')),
		       api_base_url,
		       COALESCE(api_key, ''),
		       COALESCE(model_params, extra_params, '{}'::json),
		       COALESCE(extra_params, model_params, '{}'::json),
		       input_token_price,
		       output_token_price,
		       daily_limit,
		       monthly_limit,
		       COALESCE(is_default, false),
		       COALESCE(is_active, true),
		       description,
		       COALESCE(created_by, 0),
		       created_at,
		       updated_at
		FROM llm_model_configs
		WHERE %s
		ORDER BY is_default DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query LLM model configs: %w", err)
	}
	defer rows.Close()

	var configs []*LLMModelConfig
	for rows.Next() {
		var c LLMModelConfig
		if err := rows.Scan(&c.ID, &c.TenantID, &c.ModelCode, &c.FunctionType, &c.ModelName, &c.Provider, &c.APIEndpoint, &c.APIBaseURL, &c.APIKey, &c.ModelParams,
			&c.ExtraParams, &c.InputTokenPrice, &c.OutputTokenPrice, &c.DailyLimit, &c.MonthlyLimit, &c.IsDefault, &c.IsActive, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan LLM model config: %w", err)
		}
		configs = append(configs, &c)
	}

	return configs, total, nil
}

// GetLLMModelConfigByID retrieves an LLM model config by ID
func (s *Store) GetLLMModelConfigByID(ctx context.Context, id int64) (*LLMModelConfig, error) {
	query := `
		SELECT id,
		       COALESCE(tenant_id, 0),
		       COALESCE(model_code, ''),
		       COALESCE(function_type, 'general'),
		       COALESCE(model_name, ''),
		       COALESCE(provider, ''),
		       COALESCE(NULLIF(api_endpoint, ''), COALESCE(api_base_url, '')),
		       api_base_url,
		       COALESCE(api_key, ''),
		       COALESCE(model_params, extra_params, '{}'::json),
		       COALESCE(extra_params, model_params, '{}'::json),
		       input_token_price,
		       output_token_price,
		       daily_limit,
		       monthly_limit,
		       COALESCE(is_default, false),
		       COALESCE(is_active, true),
		       description,
		       COALESCE(created_by, 0),
		       created_at,
		       updated_at
		FROM llm_model_configs
		WHERE id = $1 AND deleted_at IS NULL
	`

	var c LLMModelConfig
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.TenantID, &c.ModelCode, &c.FunctionType, &c.ModelName, &c.Provider, &c.APIEndpoint, &c.APIBaseURL, &c.APIKey, &c.ModelParams,
		&c.ExtraParams, &c.InputTokenPrice, &c.OutputTokenPrice, &c.DailyLimit, &c.MonthlyLimit, &c.IsDefault, &c.IsActive, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("LLM model config not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query LLM model config: %w", err)
	}

	return &c, nil
}

// CreateLLMModelConfig creates a new LLM model config
func (s *Store) CreateLLMModelConfig(ctx context.Context, createdBy int64, req CreateLLMModelConfigRequest) (*LLMModelConfig, error) {
	tenantID := int64(0)
	if req.TenantID != nil {
		tenantID = *req.TenantID
	}

	// If this is set as default, unset other defaults
	if req.IsDefault {
		_, err := s.pool.Exec(ctx, `
			UPDATE llm_model_configs
			SET is_default = false
			WHERE deleted_at IS NULL
			  AND tenant_id = $1
			  AND function_type = $2
			  AND is_default = true
		`, tenantID, req.FunctionType)
		if err != nil {
			return nil, fmt.Errorf("failed to unset default configs: %w", err)
		}
	}

	query := `
		INSERT INTO llm_model_configs (
			tenant_id, model_code, function_type, model_name, provider,
			api_endpoint, api_base_url, api_key, model_params, extra_params,
			input_token_price, output_token_price, daily_limit, monthly_limit,
			is_default, is_active, description, created_by, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, NOW(), NOW())
		RETURNING id,
		          COALESCE(tenant_id, 0),
		          COALESCE(model_code, ''),
		          COALESCE(function_type, 'general'),
		          COALESCE(model_name, ''),
		          COALESCE(provider, ''),
		          COALESCE(NULLIF(api_endpoint, ''), COALESCE(api_base_url, '')),
		          api_base_url,
		          COALESCE(api_key, ''),
		          COALESCE(model_params, '{}'::json),
		          COALESCE(extra_params, '{}'::json),
		          input_token_price,
		          output_token_price,
		          daily_limit,
		          monthly_limit,
		          COALESCE(is_default, false),
		          COALESCE(is_active, true),
		          description,
		          COALESCE(created_by, 0),
		          created_at,
		          updated_at
	`

	apiBaseURL := req.APIEndpoint
	if req.APIBaseURL != nil {
		apiBaseURL = strings.TrimSpace(*req.APIBaseURL)
	}

	var c LLMModelConfig
	err := s.pool.QueryRow(ctx, query, tenantID, req.ModelCode, req.FunctionType, req.ModelName, req.Provider, req.APIEndpoint, apiBaseURL, req.APIKey, req.ModelParams, req.ExtraParams,
		req.InputTokenPrice, req.OutputTokenPrice, req.DailyLimit, req.MonthlyLimit,
		req.IsDefault, req.IsActive, req.Description, createdBy).Scan(
		&c.ID, &c.TenantID, &c.ModelCode, &c.FunctionType, &c.ModelName, &c.Provider, &c.APIEndpoint, &c.APIBaseURL, &c.APIKey, &c.ModelParams,
		&c.ExtraParams, &c.InputTokenPrice, &c.OutputTokenPrice, &c.DailyLimit, &c.MonthlyLimit, &c.IsDefault, &c.IsActive, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create LLM model config: %w", err)
	}

	return &c, nil
}

// UpdateLLMModelConfig updates an LLM model config
func (s *Store) UpdateLLMModelConfig(ctx context.Context, id int64, req UpdateLLMModelConfigRequest) (*LLMModelConfig, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.TenantID != nil {
		setClauses = append(setClauses, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.ModelCode != nil {
		setClauses = append(setClauses, fmt.Sprintf("model_code = $%d", argIndex))
		args = append(args, *req.ModelCode)
		argIndex++
	}

	if req.FunctionType != nil {
		setClauses = append(setClauses, fmt.Sprintf("function_type = $%d", argIndex))
		args = append(args, *req.FunctionType)
		argIndex++
	}

	if req.ModelName != nil {
		setClauses = append(setClauses, fmt.Sprintf("model_name = $%d", argIndex))
		args = append(args, *req.ModelName)
		argIndex++
	}

	if req.Provider != nil {
		setClauses = append(setClauses, fmt.Sprintf("provider = $%d", argIndex))
		args = append(args, *req.Provider)
		argIndex++
	}

	if req.APIEndpoint != nil {
		setClauses = append(setClauses, fmt.Sprintf("api_endpoint = $%d", argIndex))
		args = append(args, *req.APIEndpoint)
		argIndex++
		setClauses = append(setClauses, fmt.Sprintf("api_base_url = $%d", argIndex))
		args = append(args, *req.APIEndpoint)
		argIndex++
	}

	if req.APIBaseURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("api_base_url = $%d", argIndex))
		args = append(args, *req.APIBaseURL)
		argIndex++
		if req.APIEndpoint == nil {
			setClauses = append(setClauses, fmt.Sprintf("api_endpoint = $%d", argIndex))
			args = append(args, *req.APIBaseURL)
			argIndex++
		}
	}

	if req.APIKey != nil {
		setClauses = append(setClauses, fmt.Sprintf("api_key = $%d", argIndex))
		args = append(args, *req.APIKey)
		argIndex++
	}

	if req.ModelParams != nil {
		setClauses = append(setClauses, fmt.Sprintf("model_params = $%d", argIndex))
		args = append(args, req.ModelParams)
		argIndex++
	}

	if req.ExtraParams != nil {
		setClauses = append(setClauses, fmt.Sprintf("extra_params = $%d", argIndex))
		args = append(args, req.ExtraParams)
		argIndex++
	} else if req.ModelParams != nil {
		setClauses = append(setClauses, fmt.Sprintf("extra_params = $%d", argIndex))
		args = append(args, req.ModelParams)
		argIndex++
	}

	if req.InputTokenPrice != nil {
		setClauses = append(setClauses, fmt.Sprintf("input_token_price = $%d", argIndex))
		args = append(args, *req.InputTokenPrice)
		argIndex++
	}

	if req.OutputTokenPrice != nil {
		setClauses = append(setClauses, fmt.Sprintf("output_token_price = $%d", argIndex))
		args = append(args, *req.OutputTokenPrice)
		argIndex++
	}

	if req.DailyLimit != nil {
		setClauses = append(setClauses, fmt.Sprintf("daily_limit = $%d", argIndex))
		args = append(args, *req.DailyLimit)
		argIndex++
	}

	if req.MonthlyLimit != nil {
		setClauses = append(setClauses, fmt.Sprintf("monthly_limit = $%d", argIndex))
		args = append(args, *req.MonthlyLimit)
		argIndex++
	}

	if req.IsDefault != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_default = $%d", argIndex))
		args = append(args, *req.IsDefault)
		argIndex++
	}

	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, *req.Description)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetLLMModelConfigByID(ctx, id)
	}

	if req.IsDefault != nil && *req.IsDefault {
		targetTenantID := int64(0)
		targetFunctionType := "general"
		if err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(tenant_id, 0), COALESCE(function_type, 'general')
			FROM llm_model_configs
			WHERE id = $1 AND deleted_at IS NULL
		`, id).Scan(&targetTenantID, &targetFunctionType); err != nil {
			if err == pgx.ErrNoRows {
				return nil, fmt.Errorf("LLM model config not found")
			}
			return nil, fmt.Errorf("failed to load target config scope: %w", err)
		}
		if req.TenantID != nil {
			targetTenantID = *req.TenantID
		}
		if req.FunctionType != nil && strings.TrimSpace(*req.FunctionType) != "" {
			targetFunctionType = strings.TrimSpace(*req.FunctionType)
		}
		if _, err := s.pool.Exec(ctx, `
			UPDATE llm_model_configs
			SET is_default = false
			WHERE deleted_at IS NULL
			  AND tenant_id = $1
			  AND function_type = $2
			  AND id <> $3
			  AND is_default = true
		`, targetTenantID, targetFunctionType, id); err != nil {
			return nil, fmt.Errorf("failed to unset scoped default configs: %w", err)
		}
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE llm_model_configs
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id,
		          COALESCE(tenant_id, 0),
		          COALESCE(model_code, ''),
		          COALESCE(function_type, 'general'),
		          COALESCE(model_name, ''),
		          COALESCE(provider, ''),
		          COALESCE(NULLIF(api_endpoint, ''), COALESCE(api_base_url, '')),
		          api_base_url,
		          COALESCE(api_key, ''),
		          COALESCE(model_params, extra_params, '{}'::json),
		          COALESCE(extra_params, model_params, '{}'::json),
		          input_token_price,
		          output_token_price,
		          daily_limit,
		          monthly_limit,
		          COALESCE(is_default, false),
		          COALESCE(is_active, true),
		          description,
		          COALESCE(created_by, 0),
		          created_at,
		          updated_at
	`, strings.Join(setClauses, ", "), argIndex)

	var c LLMModelConfig
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&c.ID, &c.TenantID, &c.ModelCode, &c.FunctionType, &c.ModelName, &c.Provider, &c.APIEndpoint, &c.APIBaseURL, &c.APIKey, &c.ModelParams,
		&c.ExtraParams, &c.InputTokenPrice, &c.OutputTokenPrice, &c.DailyLimit, &c.MonthlyLimit, &c.IsDefault, &c.IsActive, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("LLM model config not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update LLM model config: %w", err)
	}

	return &c, nil
}

// DeleteLLMModelConfig soft deletes an LLM model config
func (s *Store) DeleteLLMModelConfig(ctx context.Context, id int64) error {
	query := `
		UPDATE llm_model_configs
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete LLM model config: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("LLM model config not found")
	}

	return nil
}

// SetDefaultLLMModelConfig sets an LLM model config as default
func (s *Store) SetDefaultLLMModelConfig(ctx context.Context, id int64) error {
	// Start transaction
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var tenantID int64
	var functionType string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(tenant_id, 0), COALESCE(function_type, 'general')
		FROM llm_model_configs
		WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&tenantID, &functionType); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("LLM model config not found")
		}
		return fmt.Errorf("failed to load target config scope: %w", err)
	}

	// Unset defaults in the same scope only (tenant + function_type).
	_, err = tx.Exec(ctx, `
		UPDATE llm_model_configs
		SET is_default = false
		WHERE deleted_at IS NULL
		  AND tenant_id = $1
		  AND function_type = $2
		  AND is_default = true
	`, tenantID, functionType)
	if err != nil {
		return fmt.Errorf("failed to unset scoped default configs: %w", err)
	}

	// Set new default
	result, err := tx.Exec(ctx, "UPDATE llm_model_configs SET is_default = true, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		return fmt.Errorf("failed to set default config: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("LLM model config not found")
	}

	return tx.Commit(ctx)
}

// LLM Call Record Methods

// CreateLLMCallRecord creates an LLM call record
func (s *Store) CreateLLMCallRecord(ctx context.Context, record *LLMCallRecord) error {
	query := `
		INSERT INTO llm_call_records (tenant_id, user_id, model_config_id, model_name, provider, prompt_tokens, completion_tokens, total_tokens, cost, duration, status, error_message, request_payload, response_payload, purpose, related_id, related_type, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, NOW())
		RETURNING id, created_at
	`

	return s.pool.QueryRow(ctx, query,
		record.TenantID, record.UserID, record.ModelConfigID, record.ModelName, record.Provider,
		record.PromptTokens, record.CompletionTokens, record.TotalTokens, record.Cost, record.Duration,
		record.Status, record.ErrorMessage, record.RequestPayload, record.ResponsePayload,
		record.Purpose, record.RelatedID, record.RelatedType,
	).Scan(&record.ID, &record.CreatedAt)
}

// ListLLMCallRecords retrieves a paginated list of LLM call records
func (s *Store) ListLLMCallRecords(ctx context.Context, req LLMCallRecordListRequest) ([]*LLMCallRecord, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.UserID != nil {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIndex))
		args = append(args, *req.UserID)
		argIndex++
	}

	if req.ModelConfigID != nil {
		conditions = append(conditions, fmt.Sprintf("model_config_id = $%d", argIndex))
		args = append(args, *req.ModelConfigID)
		argIndex++
	}

	if req.Provider != nil {
		conditions = append(conditions, fmt.Sprintf("provider = $%d", argIndex))
		args = append(args, *req.Provider)
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.Purpose != nil {
		conditions = append(conditions, fmt.Sprintf("purpose = $%d", argIndex))
		args = append(args, *req.Purpose)
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM llm_call_records WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count LLM call records: %w", err)
	}

	// Query records
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, user_id, model_config_id, model_name, provider, prompt_tokens, completion_tokens, total_tokens, cost, duration, status, error_message, purpose, related_id, related_type, created_at
		FROM llm_call_records
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query LLM call records: %w", err)
	}
	defer rows.Close()

	var records []*LLMCallRecord
	for rows.Next() {
		var r LLMCallRecord
		if err := rows.Scan(&r.ID, &r.TenantID, &r.UserID, &r.ModelConfigID, &r.ModelName, &r.Provider,
			&r.PromptTokens, &r.CompletionTokens, &r.TotalTokens, &r.Cost, &r.Duration, &r.Status,
			&r.ErrorMessage, &r.Purpose, &r.RelatedID, &r.RelatedType, &r.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan LLM call record: %w", err)
		}
		records = append(records, &r)
	}

	return records, total, nil
}

// GetLLMCallRecordStats retrieves LLM call record statistics
func (s *Store) GetLLMCallRecordStats(ctx context.Context, tenantID *int64, startDate, endDate *string) (*LLMCallRecordStatsResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}

	if startDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *startDate)
		argIndex++
	}

	if endDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *endDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_calls,
			COUNT(CASE WHEN status = 'success' THEN 1 END) as success_calls,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_calls,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(cost), 0) as total_cost,
			COALESCE(AVG(duration), 0) as avg_duration
		FROM llm_call_records
		WHERE %s
	`, whereClause)

	var stats LLMCallRecordStatsResponse
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&stats.TotalCalls, &stats.SuccessCalls, &stats.FailedCalls,
		&stats.TotalTokens, &stats.TotalCost, &stats.AvgDuration,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM call record stats: %w", err)
	}

	return &stats, nil
}

// GetLLMCallRecordByID retrieves an LLM call record by ID
func (s *Store) GetLLMCallRecordByID(ctx context.Context, id int64) (*LLMCallRecord, error) {
	query := `
		SELECT id, tenant_id, user_id, model_config_id, model_name, provider, prompt_tokens, completion_tokens, total_tokens, cost, duration, status, error_message, request_payload, response_payload, purpose, related_id, related_type, created_at
		FROM llm_call_records
		WHERE id = $1
	`

	var r LLMCallRecord
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&r.ID, &r.TenantID, &r.UserID, &r.ModelConfigID, &r.ModelName, &r.Provider,
		&r.PromptTokens, &r.CompletionTokens, &r.TotalTokens, &r.Cost, &r.Duration, &r.Status,
		&r.ErrorMessage, &r.RequestPayload, &r.ResponsePayload, &r.Purpose, &r.RelatedID, &r.RelatedType, &r.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("LLM call record not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query LLM call record: %w", err)
	}

	return &r, nil
}

// GetLLMCostByTenant retrieves LLM cost for a tenant
func (s *Store) GetLLMCostByTenant(ctx context.Context, tenantID int64, startDate, endDate *string) (*LLMCostTenantResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
	args = append(args, tenantID)
	argIndex++

	if startDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *startDate)
		argIndex++
	}

	if endDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *endDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(cost), 0) as total_cost,
			COUNT(*) as total_calls,
			COALESCE(SUM(total_tokens), 0) as total_tokens
		FROM llm_call_records
		WHERE %s
	`, whereClause)

	var response LLMCostTenantResponse
	response.TenantID = tenantID

	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&response.TotalCost, &response.TotalCalls, &response.TotalTokens,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM cost by tenant: %w", err)
	}

	return &response, nil
}

// GetLLMCostSummary retrieves overall LLM cost summary
func (s *Store) GetLLMCostSummary(ctx context.Context, startDate, endDate *string) (*LLMCostSummaryResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

	if startDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *startDate)
		argIndex++
	}

	if endDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *endDate)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(cost), 0) as total_cost,
			COUNT(*) as total_calls,
			COALESCE(SUM(total_tokens), 0) as total_tokens
		FROM llm_call_records
		WHERE %s
	`, whereClause)

	var response LLMCostSummaryResponse

	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&response.TotalCost, &response.TotalCalls, &response.TotalTokens,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM cost summary: %w", err)
	}

	return &response, nil
}

func validateTableName(tableName string) error {
	if tableName == "" {
		return fmt.Errorf("table name is required")
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z_][a-zA-Z0-9_]*$`, tableName)
	if !matched {
		return fmt.Errorf("invalid table name")
	}
	return nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Data browser methods

func (s *Store) ListTables(ctx context.Context) ([]string, error) {
	query := `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, t)
	}
	return tables, nil
}

func (s *Store) GetTableStructure(ctx context.Context, tableName string) ([]map[string]interface{}, error) {
	if err := validateTableName(tableName); err != nil {
		return nil, err
	}

	query := `
		SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position
	`
	rows, err := s.pool.Query(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get table structure: %w", err)
	}
	defer rows.Close()

	result := []map[string]interface{}{}
	for rows.Next() {
		var colName, dataType, isNullable string
		var colDefault *string
		if err := rows.Scan(&colName, &dataType, &isNullable, &colDefault); err != nil {
			return nil, fmt.Errorf("failed to scan column info: %w", err)
		}
		result = append(result, map[string]interface{}{
			"column_name":    colName,
			"data_type":      dataType,
			"is_nullable":    isNullable == "YES",
			"column_default": colDefault,
		})
	}
	return result, nil
}

func (s *Store) GetTableData(ctx context.Context, tableName string, page, pageSize int) ([]map[string]interface{}, int64, error) {
	if err := validateTableName(tableName); err != nil {
		return nil, 0, err
	}

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	quoted := quoteIdent(tableName)
	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quoted)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count table rows: %w", err)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT * FROM %s LIMIT $1 OFFSET $2", quoted)
	rows, err := s.pool.Query(ctx, query, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query table data: %w", err)
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	cols := make([]string, len(fieldDescs))
	for i, f := range fieldDescs {
		cols[i] = string(f.Name)
	}

	result := []map[string]interface{}{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read row values: %w", err)
		}
		item := make(map[string]interface{}, len(cols))
		for i := range cols {
			v := values[i]
			switch tv := v.(type) {
			case []byte:
				item[cols[i]] = string(tv)
			default:
				item[cols[i]] = tv
			}
		}
		result = append(result, item)
	}

	return result, total, nil
}

func (s *Store) ExportTableDataCSV(ctx context.Context, tableName string, limit int) (string, int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	data, total, err := s.GetTableData(ctx, tableName, 1, limit)
	if err != nil {
		return "", 0, err
	}
	if len(data) == 0 {
		return "", total, nil
	}

	// Stabilize column order by first row keys.
	columns := make([]string, 0, len(data[0]))
	for k := range data[0] {
		columns = append(columns, k)
	}
	// deterministic order
	for i := 0; i < len(columns); i++ {
		for j := i + 1; j < len(columns); j++ {
			if columns[j] < columns[i] {
				columns[i], columns[j] = columns[j], columns[i]
			}
		}
	}

	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)
	if err := w.Write(columns); err != nil {
		return "", 0, fmt.Errorf("failed to write csv header: %w", err)
	}
	for _, row := range data {
		record := make([]string, len(columns))
		for i, c := range columns {
			if row[c] == nil {
				record[i] = ""
			} else {
				record[i] = fmt.Sprintf("%v", row[c])
			}
		}
		if err := w.Write(record); err != nil {
			return "", 0, fmt.Errorf("failed to write csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", 0, fmt.Errorf("failed to flush csv writer: %w", err)
	}
	return buf.String(), total, nil
}

func (s *Store) GetDatabaseStatistics(ctx context.Context) (map[string]interface{}, error) {
	var totalTables int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
	`).Scan(&totalTables); err != nil {
		return nil, fmt.Errorf("failed to count tables: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT relname, COALESCE(n_live_tup, 0)::bigint
		FROM pg_stat_user_tables
		ORDER BY relname
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query table stats: %w", err)
	}
	defer rows.Close()

	var totalRows int64
	tableStats := []map[string]interface{}{}
	for rows.Next() {
		var tableName string
		var rowCount int64
		if err := rows.Scan(&tableName, &rowCount); err != nil {
			return nil, fmt.Errorf("failed to scan table stats: %w", err)
		}
		totalRows += rowCount
		tableStats = append(tableStats, map[string]interface{}{
			"table_name": tableName,
			"row_count":  rowCount,
		})
	}

	return map[string]interface{}{
		"total_tables": totalTables,
		"total_rows":   totalRows,
		"table_stats":  tableStats,
	}, nil
}

func (s *Store) TruncateTable(ctx context.Context, tableName string) error {
	if err := validateTableName(tableName); err != nil {
		return err
	}
	// Safety check: only import/staging/temp tables can be truncated.
	allowed := strings.HasPrefix(tableName, "import_") || strings.HasSuffix(tableName, "_staging") || strings.HasPrefix(tableName, "tmp_")
	if !allowed {
		return fmt.Errorf("table %s is protected; only import_/tmp_/*_staging tables are allowed", tableName)
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", quoteIdent(tableName)))
	if err != nil {
		return fmt.Errorf("failed to truncate table: %w", err)
	}
	return nil
}

func (s *Store) ClearImportData(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name LIKE 'import_%'
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to list import tables: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return count, fmt.Errorf("failed to scan import table: %w", err)
		}
		// CASCADE prevents FK-linked import tables from failing mid-cleanup.
		if _, err := s.pool.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", quoteIdent(t))); err != nil {
			return count, fmt.Errorf("failed to truncate %s: %w", t, err)
		}
		count++
	}
	return count, nil
}

// Visit methods (backed by recordings as visit source)

func (s *Store) ListVisits(ctx context.Context, tenantID *int64, page, pageSize int) ([]map[string]interface{}, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	conditions := []string{"1=1"}
	args := []interface{}{}
	argIndex := 1
	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("r.tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}
	whereClause := strings.Join(conditions, " AND ")

	var total int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM recordings r WHERE %s", whereClause)
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count visits: %w", err)
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT r.id, r.tenant_id, r.employee_id, COALESCE(e.full_name, ''), r.customer_id, COALESCE(c.name, ''),
		       r.scene, r.recorded_at, r.created_at
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE %s
		ORDER BY r.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	args = append(args, pageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list visits: %w", err)
	}
	defer rows.Close()

	result := []map[string]interface{}{}
	for rows.Next() {
		var id, tenant, employeeID int64
		var employeeName string
		var customerID *int64
		var customerName string
		var scene *string
		var recordedAt, createdAt interface{}
		if err := rows.Scan(&id, &tenant, &employeeID, &employeeName, &customerID, &customerName, &scene, &recordedAt, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan visit: %w", err)
		}
		result = append(result, map[string]interface{}{
			"id":            id,
			"tenant_id":     tenant,
			"employee_id":   employeeID,
			"employee_name": employeeName,
			"customer_id":   customerID,
			"customer_name": customerName,
			"scene":         scene,
			"recorded_at":   fmt.Sprintf("%v", recordedAt),
			"created_at":    fmt.Sprintf("%v", createdAt),
		})
	}
	return result, total, nil
}

func (s *Store) GetVisitByID(ctx context.Context, id int64) (map[string]interface{}, error) {
	query := `
		SELECT r.id, r.tenant_id, r.employee_id, COALESCE(e.full_name, ''), r.customer_id, COALESCE(c.name, ''),
		       r.scene, r.notes, r.status, r.recorded_at, r.created_at
		FROM recordings r
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.id = $1
	`
	var visitID, tenantID, employeeID int64
	var employeeName string
	var customerID *int64
	var customerName string
	var scene, notes, status *string
	var recordedAt, createdAt interface{}
	if err := s.pool.QueryRow(ctx, query, id).Scan(
		&visitID, &tenantID, &employeeID, &employeeName, &customerID, &customerName,
		&scene, &notes, &status, &recordedAt, &createdAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("visit not found")
		}
		return nil, fmt.Errorf("failed to query visit: %w", err)
	}
	return map[string]interface{}{
		"id":            visitID,
		"tenant_id":     tenantID,
		"employee_id":   employeeID,
		"employee_name": employeeName,
		"customer_id":   customerID,
		"customer_name": customerName,
		"scene":         scene,
		"notes":         notes,
		"status":        status,
		"recorded_at":   fmt.Sprintf("%v", recordedAt),
		"created_at":    fmt.Sprintf("%v", createdAt),
	}, nil
}

func (s *Store) GetVisitStatistics(ctx context.Context, tenantID *int64) (map[string]int64, error) {
	conditions := []string{"1=1"}
	args := []interface{}{}
	argIndex := 1
	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *tenantID)
		argIndex++
	}
	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE created_at >= NOW()::date)::bigint,
			COUNT(*) FILTER (WHERE created_at >= date_trunc('week', NOW()))::bigint,
			COUNT(*) FILTER (WHERE created_at >= date_trunc('month', NOW()))::bigint
		FROM recordings
		WHERE %s
	`, whereClause)
	var total, today, week, month int64
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&total, &today, &week, &month); err != nil {
		return nil, fmt.Errorf("failed to get visit stats: %w", err)
	}
	return map[string]int64{
		"total_visits":      total,
		"visits_today":      today,
		"visits_this_week":  week,
		"visits_this_month": month,
	}, nil
}

func (s *Store) GetVisitFilters(ctx context.Context, tenantID *int64) (map[string]interface{}, error) {
	args := []interface{}{}
	whereTenant := ""
	if tenantID != nil {
		whereTenant = "AND tenant_id = $1"
		args = append(args, *tenantID)
	}

	departments := []string{}
	rows1, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT DISTINCT COALESCE(d.name, '')
		FROM employees e
		LEFT JOIN departments d ON d.id = e.department_id
		WHERE e.deleted_at IS NULL %s
		ORDER BY 1
	`, whereTenant), args...)
	if err == nil {
		defer rows1.Close()
		for rows1.Next() {
			var n string
			if err := rows1.Scan(&n); err == nil && n != "" {
				departments = append(departments, n)
			}
		}
	}

	doctors := []string{}
	rows2, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT DISTINCT full_name
		FROM employees
		WHERE deleted_at IS NULL %s
		ORDER BY full_name
	`, whereTenant), args...)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var n string
			if err := rows2.Scan(&n); err == nil && n != "" {
				doctors = append(doctors, n)
			}
		}
	}

	statuses := []string{"uploaded", "pending", "completed", "failed"}
	return map[string]interface{}{
		"departments": departments,
		"doctors":     doctors,
		"statuses":    statuses,
	}, nil
}

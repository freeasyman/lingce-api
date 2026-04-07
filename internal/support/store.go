package support

import (
	"context"
	"fmt"
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
		SELECT id, model_name, provider, api_endpoint, api_key, model_params, is_default, is_active, description, created_by, created_at, updated_at
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
		if err := rows.Scan(&c.ID, &c.ModelName, &c.Provider, &c.APIEndpoint, &c.APIKey, &c.ModelParams,
			&c.IsDefault, &c.IsActive, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan LLM model config: %w", err)
		}
		configs = append(configs, &c)
	}

	return configs, total, nil
}

// GetLLMModelConfigByID retrieves an LLM model config by ID
func (s *Store) GetLLMModelConfigByID(ctx context.Context, id int64) (*LLMModelConfig, error) {
	query := `
		SELECT id, model_name, provider, api_endpoint, api_key, model_params, is_default, is_active, description, created_by, created_at, updated_at
		FROM llm_model_configs
		WHERE id = $1 AND deleted_at IS NULL
	`

	var c LLMModelConfig
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.ModelName, &c.Provider, &c.APIEndpoint, &c.APIKey, &c.ModelParams,
		&c.IsDefault, &c.IsActive, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
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
	// If this is set as default, unset other defaults
	if req.IsDefault {
		_, err := s.pool.Exec(ctx, "UPDATE llm_model_configs SET is_default = false WHERE is_default = true")
		if err != nil {
			return nil, fmt.Errorf("failed to unset default configs: %w", err)
		}
	}

	query := `
		INSERT INTO llm_model_configs (model_name, provider, api_endpoint, api_key, model_params, is_default, is_active, description, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		RETURNING id, model_name, provider, api_endpoint, api_key, model_params, is_default, is_active, description, created_by, created_at, updated_at
	`

	var c LLMModelConfig
	err := s.pool.QueryRow(ctx, query, req.ModelName, req.Provider, req.APIEndpoint, req.APIKey, req.ModelParams,
		req.IsDefault, req.IsActive, req.Description, createdBy).Scan(
		&c.ID, &c.ModelName, &c.Provider, &c.APIEndpoint, &c.APIKey, &c.ModelParams,
		&c.IsDefault, &c.IsActive, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
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

	if req.ModelName != nil {
		setClauses = append(setClauses, fmt.Sprintf("model_name = $%d", argIndex))
		args = append(args, *req.ModelName)
		argIndex++
	}

	if req.APIEndpoint != nil {
		setClauses = append(setClauses, fmt.Sprintf("api_endpoint = $%d", argIndex))
		args = append(args, *req.APIEndpoint)
		argIndex++
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

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE llm_model_configs
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, model_name, provider, api_endpoint, api_key, model_params, is_default, is_active, description, created_by, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)

	var c LLMModelConfig
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&c.ID, &c.ModelName, &c.Provider, &c.APIEndpoint, &c.APIKey, &c.ModelParams,
		&c.IsDefault, &c.IsActive, &c.Description, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
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

	// Unset all defaults
	_, err = tx.Exec(ctx, "UPDATE llm_model_configs SET is_default = false WHERE is_default = true")
	if err != nil {
		return fmt.Errorf("failed to unset default configs: %w", err)
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

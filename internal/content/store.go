package content

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Topic Methods

// ListTopics retrieves a paginated list of topics
func (s *Store) ListTopics(ctx context.Context, req TopicListRequest) ([]*ContentTopic, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.Category != nil {
		conditions = append(conditions, fmt.Sprintf("category = $%d", argIndex))
		args = append(args, *req.Category)
		argIndex++
	}

	if req.Source != nil {
		conditions = append(conditions, fmt.Sprintf("source = $%d", argIndex))
		args = append(args, *req.Source)
		argIndex++
	}

	if req.CreatedBy != nil {
		conditions = append(conditions, fmt.Sprintf("created_by = $%d", argIndex))
		args = append(args, *req.CreatedBy)
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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM content_topics WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count topics: %w", err)
	}

	// Query topics
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, title, description, category, tags, source, status, priority,
		       target_date, extra_data, created_by, created_at, updated_at
		FROM content_topics
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query topics: %w", err)
	}
	defer rows.Close()

	var topics []*ContentTopic
	for rows.Next() {
		var t ContentTopic
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Title, &t.Description, &t.Category, &t.Tags,
			&t.Source, &t.Status, &t.Priority, &t.TargetDate, &t.ExtraData, &t.CreatedBy,
			&t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan topic: %w", err)
		}
		topics = append(topics, &t)
	}

	return topics, total, nil
}

// GetTopicByID retrieves a topic by ID
func (s *Store) GetTopicByID(ctx context.Context, id int64) (*ContentTopic, error) {
	query := `
		SELECT id, tenant_id, title, description, category, tags, source, status, priority,
		       target_date, extra_data, created_by, created_at, updated_at
		FROM content_topics
		WHERE id = $1 AND deleted_at IS NULL
	`

	var t ContentTopic
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.TenantID, &t.Title, &t.Description, &t.Category, &t.Tags,
		&t.Source, &t.Status, &t.Priority, &t.TargetDate, &t.ExtraData, &t.CreatedBy,
		&t.CreatedAt, &t.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("topic not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query topic: %w", err)
	}

	return &t, nil
}

// CreateTopic creates a new topic
func (s *Store) CreateTopic(ctx context.Context, tenantID, createdBy int64, req CreateTopicRequest) (*ContentTopic, error) {
	query := `
		INSERT INTO content_topics (tenant_id, title, description, category, tags, source, status,
		                            priority, target_date, extra_data, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'draft', $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, tenant_id, title, description, category, tags, source, status, priority,
		          target_date, extra_data, created_by, created_at, updated_at
	`

	var t ContentTopic
	err := s.pool.QueryRow(ctx, query, tenantID, req.Title, req.Description, req.Category, req.Tags,
		req.Source, req.Priority, req.TargetDate, req.ExtraData, createdBy).Scan(
		&t.ID, &t.TenantID, &t.Title, &t.Description, &t.Category, &t.Tags,
		&t.Source, &t.Status, &t.Priority, &t.TargetDate, &t.ExtraData, &t.CreatedBy,
		&t.CreatedAt, &t.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create topic: %w", err)
	}

	return &t, nil
}

// UpdateTopic updates a topic
func (s *Store) UpdateTopic(ctx context.Context, id int64, req UpdateTopicRequest) (*ContentTopic, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", argIndex))
		args = append(args, *req.Title)
		argIndex++
	}

	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, *req.Description)
		argIndex++
	}

	if req.Category != nil {
		setClauses = append(setClauses, fmt.Sprintf("category = $%d", argIndex))
		args = append(args, *req.Category)
		argIndex++
	}

	if req.Tags != nil {
		setClauses = append(setClauses, fmt.Sprintf("tags = $%d", argIndex))
		args = append(args, req.Tags)
		argIndex++
	}

	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", argIndex))
		args = append(args, *req.Priority)
		argIndex++
	}

	if req.TargetDate != nil {
		setClauses = append(setClauses, fmt.Sprintf("target_date = $%d", argIndex))
		args = append(args, *req.TargetDate)
		argIndex++
	}

	if req.ExtraData != nil {
		setClauses = append(setClauses, fmt.Sprintf("extra_data = $%d", argIndex))
		args = append(args, req.ExtraData)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetTopicByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE content_topics
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, title, description, category, tags, source, status, priority,
		          target_date, extra_data, created_by, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)

	var t ContentTopic
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&t.ID, &t.TenantID, &t.Title, &t.Description, &t.Category, &t.Tags,
		&t.Source, &t.Status, &t.Priority, &t.TargetDate, &t.ExtraData, &t.CreatedBy,
		&t.CreatedAt, &t.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("topic not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update topic: %w", err)
	}

	return &t, nil
}

// DeleteTopic soft deletes a topic
func (s *Store) DeleteTopic(ctx context.Context, id int64) error {
	query := `
		UPDATE content_topics
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete topic: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("topic not found")
	}

	return nil
}

// Content Methods

// ListContents retrieves a paginated list of content items
func (s *Store) ListContents(ctx context.Context, req ContentListRequest) ([]*ContentItem, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.TopicID != nil {
		conditions = append(conditions, fmt.Sprintf("topic_id = $%d", argIndex))
		args = append(args, *req.TopicID)
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.Category != nil {
		conditions = append(conditions, fmt.Sprintf("category = $%d", argIndex))
		args = append(args, *req.Category)
		argIndex++
	}

	if req.CreatedBy != nil {
		conditions = append(conditions, fmt.Sprintf("created_by = $%d", argIndex))
		args = append(args, *req.CreatedBy)
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

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM content_items WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count contents: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, topic_id, title, content, summary, category, tags, status,
		       published_at, unpublished_at, view_count, like_count, share_count, images,
		       extra_data, created_by, created_at, updated_at
		FROM content_items
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query contents: %w", err)
	}
	defer rows.Close()

	var contents []*ContentItem
	for rows.Next() {
		var item ContentItem
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.TopicID, &item.Title, &item.Content, &item.Summary,
			&item.Category, &item.Tags, &item.Status, &item.PublishedAt, &item.UnpublishedAt,
			&item.ViewCount, &item.LikeCount, &item.ShareCount, &item.Images, &item.ExtraData,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan content item: %w", err)
		}
		contents = append(contents, &item)
	}

	return contents, total, nil
}

// GetContentByID retrieves a content item by ID
func (s *Store) GetContentByID(ctx context.Context, id int64) (*ContentItem, error) {
	query := `
		SELECT id, tenant_id, topic_id, title, content, summary, category, tags, status,
		       published_at, unpublished_at, view_count, like_count, share_count, images,
		       extra_data, created_by, created_at, updated_at
		FROM content_items
		WHERE id = $1 AND deleted_at IS NULL
	`

	var item ContentItem
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&item.ID, &item.TenantID, &item.TopicID, &item.Title, &item.Content, &item.Summary,
		&item.Category, &item.Tags, &item.Status, &item.PublishedAt, &item.UnpublishedAt,
		&item.ViewCount, &item.LikeCount, &item.ShareCount, &item.Images, &item.ExtraData,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("content not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query content: %w", err)
	}

	return &item, nil
}

// CreateContent creates a new content item
func (s *Store) CreateContent(ctx context.Context, tenantID, createdBy int64, req CreateContentRequest) (*ContentItem, error) {
	query := `
		INSERT INTO content_items (tenant_id, topic_id, title, content, summary, category, tags, status,
		                           images, extra_data, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'draft', $8, $9, $10, NOW(), NOW())
		RETURNING id, tenant_id, topic_id, title, content, summary, category, tags, status,
		          published_at, unpublished_at, view_count, like_count, share_count, images,
		          extra_data, created_by, created_at, updated_at
	`

	var item ContentItem
	err := s.pool.QueryRow(
		ctx, query, tenantID, req.TopicID, req.Title, req.Content, req.Summary, req.Category,
		req.Tags, req.Images, req.ExtraData, createdBy,
	).Scan(
		&item.ID, &item.TenantID, &item.TopicID, &item.Title, &item.Content, &item.Summary,
		&item.Category, &item.Tags, &item.Status, &item.PublishedAt, &item.UnpublishedAt,
		&item.ViewCount, &item.LikeCount, &item.ShareCount, &item.Images, &item.ExtraData,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create content: %w", err)
	}

	return &item, nil
}

// UpdateContent updates a content item
func (s *Store) UpdateContent(ctx context.Context, id int64, req UpdateContentRequest) (*ContentItem, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", argIndex))
		args = append(args, *req.Title)
		argIndex++
	}
	if req.Content != nil {
		setClauses = append(setClauses, fmt.Sprintf("content = $%d", argIndex))
		args = append(args, *req.Content)
		argIndex++
	}
	if req.Summary != nil {
		setClauses = append(setClauses, fmt.Sprintf("summary = $%d", argIndex))
		args = append(args, *req.Summary)
		argIndex++
	}
	if req.Category != nil {
		setClauses = append(setClauses, fmt.Sprintf("category = $%d", argIndex))
		args = append(args, *req.Category)
		argIndex++
	}
	if req.Tags != nil {
		setClauses = append(setClauses, fmt.Sprintf("tags = $%d", argIndex))
		args = append(args, req.Tags)
		argIndex++
	}
	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}
	if req.Images != nil {
		setClauses = append(setClauses, fmt.Sprintf("images = $%d", argIndex))
		args = append(args, req.Images)
		argIndex++
	}
	if req.ExtraData != nil {
		setClauses = append(setClauses, fmt.Sprintf("extra_data = $%d", argIndex))
		args = append(args, req.ExtraData)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetContentByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE content_items
		SET %s
		WHERE id = $%d AND deleted_at IS NULL
		RETURNING id, tenant_id, topic_id, title, content, summary, category, tags, status,
		          published_at, unpublished_at, view_count, like_count, share_count, images,
		          extra_data, created_by, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)

	var item ContentItem
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&item.ID, &item.TenantID, &item.TopicID, &item.Title, &item.Content, &item.Summary,
		&item.Category, &item.Tags, &item.Status, &item.PublishedAt, &item.UnpublishedAt,
		&item.ViewCount, &item.LikeCount, &item.ShareCount, &item.Images, &item.ExtraData,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("content not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update content: %w", err)
	}

	return &item, nil
}

// DeleteContent soft deletes a content item
func (s *Store) DeleteContent(ctx context.Context, id int64) error {
	query := `
		UPDATE content_items
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`
	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete content: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("content not found")
	}
	return nil
}

// PublishContent publishes a content item
func (s *Store) PublishContent(ctx context.Context, id int64) (*ContentItem, error) {
	query := `
		UPDATE content_items
		SET status = 'published', published_at = NOW(), unpublished_at = NULL, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, tenant_id, topic_id, title, content, summary, category, tags, status,
		          published_at, unpublished_at, view_count, like_count, share_count, images,
		          extra_data, created_by, created_at, updated_at
	`

	var item ContentItem
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&item.ID, &item.TenantID, &item.TopicID, &item.Title, &item.Content, &item.Summary,
		&item.Category, &item.Tags, &item.Status, &item.PublishedAt, &item.UnpublishedAt,
		&item.ViewCount, &item.LikeCount, &item.ShareCount, &item.Images, &item.ExtraData,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("content not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to publish content: %w", err)
	}
	return &item, nil
}

// UnpublishContent unpublishes a content item
func (s *Store) UnpublishContent(ctx context.Context, id int64) (*ContentItem, error) {
	query := `
		UPDATE content_items
		SET status = 'unpublished', unpublished_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, tenant_id, topic_id, title, content, summary, category, tags, status,
		          published_at, unpublished_at, view_count, like_count, share_count, images,
		          extra_data, created_by, created_at, updated_at
	`

	var item ContentItem
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&item.ID, &item.TenantID, &item.TopicID, &item.Title, &item.Content, &item.Summary,
		&item.Category, &item.Tags, &item.Status, &item.PublishedAt, &item.UnpublishedAt,
		&item.ViewCount, &item.LikeCount, &item.ShareCount, &item.Images, &item.ExtraData,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("content not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to unpublish content: %w", err)
	}
	return &item, nil
}

// Publish Task Methods

// ListPublishTasks retrieves a paginated list of publish tasks.
func (s *Store) ListPublishTasks(ctx context.Context, req PublishTaskListRequest) ([]*ContentPublishTask, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "deleted_at IS NULL")

	if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}
	if req.ContentID != nil {
		conditions = append(conditions, fmt.Sprintf("content_id = $%d", argIndex))
		args = append(args, *req.ContentID)
		argIndex++
	}
	if req.Platform != nil {
		conditions = append(conditions, fmt.Sprintf("platform = $%d", argIndex))
		args = append(args, *req.Platform)
		argIndex++
	}
	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}
	if req.CreatedBy != nil {
		conditions = append(conditions, fmt.Sprintf("created_by = $%d", argIndex))
		args = append(args, *req.CreatedBy)
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

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM content_publish_tasks WHERE %s", whereClause)
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count publish tasks: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, content_id, platform, status, scheduled_at, published_at, error_msg,
		       extra_data, created_by, created_at, updated_at
		FROM content_publish_tasks
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query publish tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*ContentPublishTask
	for rows.Next() {
		var task ContentPublishTask
		if err := rows.Scan(
			&task.ID, &task.TenantID, &task.ContentID, &task.Platform, &task.Status, &task.ScheduledAt,
			&task.PublishedAt, &task.ErrorMsg, &task.ExtraData, &task.CreatedBy, &task.CreatedAt, &task.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan publish task: %w", err)
		}
		tasks = append(tasks, &task)
	}

	return tasks, total, nil
}

// GetPublishTaskByID retrieves a publish task by ID.
func (s *Store) GetPublishTaskByID(ctx context.Context, id int64) (*ContentPublishTask, error) {
	query := `
		SELECT id, tenant_id, content_id, platform, status, scheduled_at, published_at, error_msg,
		       extra_data, created_by, created_at, updated_at
		FROM content_publish_tasks
		WHERE id = $1 AND deleted_at IS NULL
	`
	var task ContentPublishTask
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&task.ID, &task.TenantID, &task.ContentID, &task.Platform, &task.Status, &task.ScheduledAt,
		&task.PublishedAt, &task.ErrorMsg, &task.ExtraData, &task.CreatedBy, &task.CreatedAt, &task.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("publish task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query publish task: %w", err)
	}
	return &task, nil
}

// CreatePublishTask creates a publish task.
func (s *Store) CreatePublishTask(ctx context.Context, tenantID, createdBy int64, req CreatePublishTaskRequest) (*ContentPublishTask, error) {
	var scheduledAt *time.Time
	if req.ScheduledAt != nil && *req.ScheduledAt != "" {
		parsed, err := time.Parse(time.RFC3339, *req.ScheduledAt)
		if err != nil {
			return nil, fmt.Errorf("invalid scheduled_at format: %w", err)
		}
		scheduledAt = &parsed
	}

	query := `
		INSERT INTO content_publish_tasks (tenant_id, content_id, platform, status, scheduled_at, extra_data, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6, NOW(), NOW())
		RETURNING id, tenant_id, content_id, platform, status, scheduled_at, published_at, error_msg,
		          extra_data, created_by, created_at, updated_at
	`
	var task ContentPublishTask
	err := s.pool.QueryRow(ctx, query, tenantID, req.ContentID, req.Platform, scheduledAt, req.ExtraData, createdBy).Scan(
		&task.ID, &task.TenantID, &task.ContentID, &task.Platform, &task.Status, &task.ScheduledAt,
		&task.PublishedAt, &task.ErrorMsg, &task.ExtraData, &task.CreatedBy, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create publish task: %w", err)
	}
	return &task, nil
}

// UpdatePublishTaskStatus updates task status.
func (s *Store) UpdatePublishTaskStatus(ctx context.Context, id int64, status string) (*ContentPublishTask, error) {
	query := `
		UPDATE content_publish_tasks
		SET status = $2,
		    published_at = CASE WHEN $2 = 'completed' THEN NOW() ELSE published_at END,
		    updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, tenant_id, content_id, platform, status, scheduled_at, published_at, error_msg,
		          extra_data, created_by, created_at, updated_at
	`
	var task ContentPublishTask
	err := s.pool.QueryRow(ctx, query, id, status).Scan(
		&task.ID, &task.TenantID, &task.ContentID, &task.Platform, &task.Status, &task.ScheduledAt,
		&task.PublishedAt, &task.ErrorMsg, &task.ExtraData, &task.CreatedBy, &task.CreatedAt, &task.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("publish task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update publish task status: %w", err)
	}
	return &task, nil
}

// DeletePublishTask soft deletes a publish task.
func (s *Store) DeletePublishTask(ctx context.Context, id int64) error {
	query := `
		UPDATE content_publish_tasks
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`
	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete publish task: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("publish task not found")
	}
	return nil
}

// CountPublishTasksByStatus returns task counts grouped by status.
func (s *Store) CountPublishTasksByStatus(ctx context.Context, tenantID *int64) (map[string]int64, error) {
	args := []interface{}{}
	where := "deleted_at IS NULL"
	if tenantID != nil {
		where = "deleted_at IS NULL AND tenant_id = $1"
		args = append(args, *tenantID)
	}

	query := fmt.Sprintf(`
		SELECT status, COUNT(*)
		FROM content_publish_tasks
		WHERE %s
		GROUP BY status
	`, where)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to count publish tasks by status: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan status count: %w", err)
		}
		result[status] = count
	}
	return result, nil
}

// Content Seed Methods - Similar pattern
// (ListSeeds, GetSeedByID, GetSeedStats, GenerateDraftFromSeed, DismissSeed)

// Prompt Template Methods - Similar pattern
// (ListTemplates, GetTemplateByID, CreateTemplate, UpdateTemplate, DeleteTemplate, CreateVersion, ListVersions, PublishVersion)

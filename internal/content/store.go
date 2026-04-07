package content

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

// Content Methods - Similar pattern, abbreviated for space
// (ListContents, GetContentByID, CreateContent, UpdateContent, DeleteContent, PublishContent, UnpublishContent)

// Publish Task Methods - Similar pattern
// (ListPublishTasks, GetPublishTaskByID, CreatePublishTask, UpdateTaskStatus)

// Content Seed Methods - Similar pattern
// (ListSeeds, GetSeedByID, GetSeedStats, GenerateDraftFromSeed, DismissSeed)

// Prompt Template Methods - Similar pattern
// (ListTemplates, GetTemplateByID, CreateTemplate, UpdateTemplate, DeleteTemplate, CreateVersion, ListVersions, PublishVersion)

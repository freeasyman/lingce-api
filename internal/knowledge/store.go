package knowledge

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

func (s *Store) List(ctx context.Context, req ListRequest) ([]*KnowledgeItem, int, error) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, "1=1")
	if req.TenantID > 0 {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, req.TenantID)
		argIdx++
	}

	if req.Scope != "" {
		conditions = append(conditions, fmt.Sprintf("scope = $%d", argIdx))
		args = append(args, req.Scope)
		argIdx++
	}
	if req.Category != "" {
		conditions = append(conditions, fmt.Sprintf("category = $%d", argIdx))
		args = append(args, req.Category)
		argIdx++
	}
	if req.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, req.Status)
		argIdx++
	}
	if req.ProductName != "" {
		conditions = append(conditions, fmt.Sprintf("product_name = $%d", argIdx))
		args = append(args, req.ProductName)
		argIdx++
	}
	if req.SourceType != "" {
		conditions = append(conditions, fmt.Sprintf("source_type = $%d", argIdx))
		args = append(args, req.SourceType)
		argIdx++
	}
	if req.Keyword != "" {
		conditions = append(conditions, fmt.Sprintf("(title ILIKE $%d OR content ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+req.Keyword+"%")
		argIdx++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM knowledge_items WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count knowledge_items: %w", err)
	}

	// Query
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, scope, category, title, content, tags,
		       product_name, source_type, source_ref, status, expires_at,
		       created_at, updated_at
		FROM knowledge_items
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query knowledge_items: %w", err)
	}
	defer rows.Close()

	var items []*KnowledgeItem
	for rows.Next() {
		item := &KnowledgeItem{}
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.Scope, &item.Category,
			&item.Title, &item.Content, &item.Tags,
			&item.ProductName, &item.SourceType, &item.SourceRef,
			&item.Status, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan knowledge_item: %w", err)
		}
		items = append(items, item)
	}
	return items, total, nil
}

func (s *Store) GetByID(ctx context.Context, id int64) (*KnowledgeItem, error) {
	query := `
		SELECT id, tenant_id, scope, category, title, content, tags,
		       product_name, source_type, source_ref, status, expires_at,
		       created_at, updated_at
		FROM knowledge_items WHERE id = $1
	`
	item := &KnowledgeItem{}
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&item.ID, &item.TenantID, &item.Scope, &item.Category,
		&item.Title, &item.Content, &item.Tags,
		&item.ProductName, &item.SourceType, &item.SourceRef,
		&item.Status, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get knowledge_item %d: %w", id, err)
	}
	return item, nil
}

func (s *Store) Create(ctx context.Context, req CreateRequest) (*KnowledgeItem, error) {
	status := req.Status
	if status == "" {
		status = StatusActive
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}

	query := `
		INSERT INTO knowledge_items
			(tenant_id, scope, category, title, content, tags, product_name,
			 source_type, source_ref, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`
	item := &KnowledgeItem{
		TenantID:    req.TenantID,
		Scope:       req.Scope,
		Category:    req.Category,
		Title:       req.Title,
		Content:     req.Content,
		Tags:        tags,
		ProductName: req.ProductName,
		SourceType:  req.SourceType,
		SourceRef:   req.SourceRef,
		Status:      status,
		ExpiresAt:   req.ExpiresAt,
	}
	err := s.pool.QueryRow(ctx, query,
		req.TenantID, req.Scope, req.Category, req.Title, req.Content, tags,
		req.ProductName, req.SourceType, req.SourceRef, status, req.ExpiresAt,
	).Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create knowledge_item: %w", err)
	}
	return item, nil
}

func (s *Store) Update(ctx context.Context, id int64, req UpdateRequest) (*KnowledgeItem, error) {
	var sets []string
	var args []interface{}
	argIdx := 1

	if req.Scope != nil {
		sets = append(sets, fmt.Sprintf("scope = $%d", argIdx))
		args = append(args, *req.Scope)
		argIdx++
	}
	if req.Category != nil {
		sets = append(sets, fmt.Sprintf("category = $%d", argIdx))
		args = append(args, *req.Category)
		argIdx++
	}
	if req.Title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", argIdx))
		args = append(args, *req.Title)
		argIdx++
	}
	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d", argIdx))
		args = append(args, *req.Content)
		argIdx++
	}
	if req.Tags != nil {
		sets = append(sets, fmt.Sprintf("tags = $%d", argIdx))
		args = append(args, req.Tags)
		argIdx++
	}
	if req.ProductName != nil {
		sets = append(sets, fmt.Sprintf("product_name = $%d", argIdx))
		args = append(args, *req.ProductName)
		argIdx++
	}
	if req.ExpiresAt != nil {
		sets = append(sets, fmt.Sprintf("expires_at = $%d", argIdx))
		args = append(args, *req.ExpiresAt)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, id)
	}

	sets = append(sets, "updated_at = NOW()")
	query := fmt.Sprintf(`
		UPDATE knowledge_items SET %s WHERE id = $%d
		RETURNING id, tenant_id, scope, category, title, content, tags,
		          product_name, source_type, source_ref, status, expires_at,
		          created_at, updated_at
	`, strings.Join(sets, ", "), argIdx)
	args = append(args, id)

	item := &KnowledgeItem{}
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&item.ID, &item.TenantID, &item.Scope, &item.Category,
		&item.Title, &item.Content, &item.Tags,
		&item.ProductName, &item.SourceType, &item.SourceRef,
		&item.Status, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update knowledge_item %d: %w", id, err)
	}
	return item, nil
}

func (s *Store) UpdateStatus(ctx context.Context, id int64, status string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE knowledge_items SET status = $1, updated_at = NOW() WHERE id = $2",
		status, id,
	)
	return err
}

func (s *Store) BatchUpdateStatus(ctx context.Context, ids []int64, status string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		"UPDATE knowledge_items SET status = $1, updated_at = NOW() WHERE id = ANY($2)",
		status, ids,
	)
	if err != nil {
		return 0, fmt.Errorf("batch update status: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM knowledge_items WHERE id = $1", id)
	return err
}

// ExistsByTitleAndTenant checks if a knowledge item with the same title exists for dedup
func (s *Store) ExistsByTitleAndTenant(ctx context.Context, tenantID int64, title string, excludeID int64) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM knowledge_items WHERE tenant_id = $1 AND title = $2 AND id != $3 AND status != 'archived')`
	var exists bool
	err := s.pool.QueryRow(ctx, query, tenantID, title, excludeID).Scan(&exists)
	return exists, err
}

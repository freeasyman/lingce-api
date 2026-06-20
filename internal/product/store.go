package product

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

func (s *Store) List(ctx context.Context, req ListRequest) ([]*Product, int, error) {
	conditions := []string{"1=1"}
	args := make([]any, 0, 8)
	argIdx := 1

	if req.TenantID > 0 {
		conditions = append(conditions, fmt.Sprintf("p.tenant_id = $%d", argIdx))
		args = append(args, req.TenantID)
		argIdx++
	}
	if strings.TrimSpace(req.Industry) != "" {
		conditions = append(conditions, fmt.Sprintf("p.industry = $%d", argIdx))
		args = append(args, req.Industry)
		argIdx++
	}
	if strings.TrimSpace(req.Status) != "" {
		conditions = append(conditions, fmt.Sprintf("p.status = $%d", argIdx))
		args = append(args, req.Status)
		argIdx++
	}
	if req.IsTemplate != nil {
		conditions = append(conditions, fmt.Sprintf("p.is_template = $%d", argIdx))
		args = append(args, *req.IsTemplate)
		argIdx++
	}
	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		conditions = append(conditions, fmt.Sprintf("(p.name ILIKE $%d OR p.aliases::text ILIKE $%d OR p.applicable ILIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, "%"+keyword+"%")
		argIdx++
	}

	whereClause := strings.Join(conditions, " AND ")

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM products p WHERE %s", whereClause)
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT p.id, p.tenant_id, COALESCE(t.name, ''), p.industry, p.name, p.aliases,
		       p.price, p.applicable, p.selling_point, p.upgrade_to, p.combine_with,
		       p.status, p.is_template, p.created_at, p.updated_at
		FROM products p
		LEFT JOIN tenants t ON t.id = p.tenant_id
		WHERE %s
		ORDER BY p.is_template DESC, p.updated_at DESC, p.id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query products: %w", err)
	}
	defer rows.Close()

	items := make([]*Product, 0, req.PageSize)
	for rows.Next() {
		item := &Product{}
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.TenantName, &item.Industry, &item.Name, &item.Aliases,
			&item.Price, &item.Applicable, &item.SellingPoint, &item.UpgradeTo, &item.CombineWith,
			&item.Status, &item.IsTemplate, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan product: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate products: %w", err)
	}
	return items, total, nil
}

func (s *Store) GetByID(ctx context.Context, id int64) (*Product, error) {
	query := `
		SELECT p.id, p.tenant_id, COALESCE(t.name, ''), p.industry, p.name, p.aliases,
		       p.price, p.applicable, p.selling_point, p.upgrade_to, p.combine_with,
		       p.status, p.is_template, p.created_at, p.updated_at
		FROM products p
		LEFT JOIN tenants t ON t.id = p.tenant_id
		WHERE p.id = $1
	`
	item := &Product{}
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&item.ID, &item.TenantID, &item.TenantName, &item.Industry, &item.Name, &item.Aliases,
		&item.Price, &item.Applicable, &item.SellingPoint, &item.UpgradeTo, &item.CombineWith,
		&item.Status, &item.IsTemplate, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get product %d: %w", id, err)
	}
	return item, nil
}

func (s *Store) Create(ctx context.Context, req CreateRequest) (*Product, error) {
	query := `
		INSERT INTO products (
			tenant_id, industry, name, aliases, price, applicable, selling_point,
			upgrade_to, combine_with, status, is_template, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, NOW(), NOW()
		)
		RETURNING id, created_at, updated_at
	`
	item := &Product{
		TenantID:     req.TenantID,
		Industry:     req.Industry,
		Name:         req.Name,
		Aliases:      req.Aliases,
		Price:        req.Price,
		Applicable:   req.Applicable,
		SellingPoint: req.SellingPoint,
		UpgradeTo:    req.UpgradeTo,
		CombineWith:  req.CombineWith,
		Status:       req.Status,
		IsTemplate:   req.IsTemplate,
	}
	if err := s.pool.QueryRow(ctx, query,
		req.TenantID, req.Industry, req.Name, req.Aliases, req.Price, req.Applicable, req.SellingPoint,
		req.UpgradeTo, req.CombineWith, req.Status, req.IsTemplate,
	).Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	return s.GetByID(ctx, item.ID)
}

func (s *Store) Update(ctx context.Context, id int64, req UpdateRequest) (*Product, error) {
	sets := make([]string, 0, 10)
	args := make([]any, 0, 12)
	argIdx := 1

	if req.Industry != nil {
		sets = append(sets, fmt.Sprintf("industry = $%d", argIdx))
		args = append(args, *req.Industry)
		argIdx++
	}
	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Aliases != nil {
		sets = append(sets, fmt.Sprintf("aliases = $%d", argIdx))
		args = append(args, *req.Aliases)
		argIdx++
	}
	if req.Price != nil {
		sets = append(sets, fmt.Sprintf("price = $%d", argIdx))
		args = append(args, *req.Price)
		argIdx++
	}
	if req.Applicable != nil {
		sets = append(sets, fmt.Sprintf("applicable = $%d", argIdx))
		args = append(args, *req.Applicable)
		argIdx++
	}
	if req.SellingPoint != nil {
		sets = append(sets, fmt.Sprintf("selling_point = $%d", argIdx))
		args = append(args, *req.SellingPoint)
		argIdx++
	}
	if req.UpgradeTo != nil {
		sets = append(sets, fmt.Sprintf("upgrade_to = $%d", argIdx))
		args = append(args, *req.UpgradeTo)
		argIdx++
	}
	if req.CombineWith != nil {
		sets = append(sets, fmt.Sprintf("combine_with = $%d", argIdx))
		args = append(args, *req.CombineWith)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.IsTemplate != nil {
		sets = append(sets, fmt.Sprintf("is_template = $%d", argIdx))
		args = append(args, *req.IsTemplate)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, id)
	}

	sets = append(sets, "updated_at = NOW()")
	query := fmt.Sprintf("UPDATE products SET %s WHERE id = $%d", strings.Join(sets, ", "), argIdx)
	args = append(args, id)
	if _, err := s.pool.Exec(ctx, query, args...); err != nil {
		return nil, fmt.Errorf("update product %d: %w", id, err)
	}
	return s.GetByID(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, "DELETE FROM products WHERE id = $1", id); err != nil {
		return fmt.Errorf("delete product %d: %w", id, err)
	}
	return nil
}

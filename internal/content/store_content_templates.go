package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ListContentPromptTemplates(ctx context.Context, req TemplateListRequest) ([]TemplateResponse, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	var conditions []string
	var args []interface{}
	argIndex := 1

	if req.IsActive == nil {
		conditions = append(conditions, "is_active = TRUE")
	}

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("(tenant_id = ANY($%d) OR tenant_id IS NULL)", argIndex))
		args = append(args, req.TenantIDs)
		argIndex++
	} else if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("(tenant_id = $%d OR tenant_id IS NULL)", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.IsActive != nil {
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	if req.FunctionType != nil && strings.TrimSpace(*req.FunctionType) != "" {
		conditions = append(conditions, fmt.Sprintf("function_type = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.FunctionType))
		argIndex++
	} else if req.Category != nil && strings.TrimSpace(*req.Category) != "" {
		conditions = append(conditions, fmt.Sprintf("function_type = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.Category))
		argIndex++
	}

	if req.Search != nil && strings.TrimSpace(*req.Search) != "" {
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d OR code ILIKE $%d)", argIndex, argIndex))
		args = append(args, "%"+strings.TrimSpace(*req.Search)+"%")
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM content_prompt_templates WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count content prompt templates: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, code, description, prompt_template, variables, function_type,
		       version, is_active, is_system, usage_count, created_at, updated_at, created_by
		FROM content_prompt_templates
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query content prompt templates: %w", err)
	}
	defer rows.Close()

	items := make([]TemplateResponse, 0)
	for rows.Next() {
		item, err := scanContentPromptTemplate(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, nil
}

func (s *Store) GetContentPromptTemplateByID(ctx context.Context, id int64) (*TemplateResponse, error) {
	query := `
		SELECT id, tenant_id, name, code, description, prompt_template, variables, function_type,
		       version, is_active, is_system, usage_count, created_at, updated_at, created_by
		FROM content_prompt_templates
		WHERE id = $1
	`
	row := s.pool.QueryRow(ctx, query, id)
	item, err := scanContentPromptTemplateRow(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("template not found")
		}
		return nil, fmt.Errorf("failed to query content prompt template: %w", err)
	}
	return item, nil
}

func (s *Store) CreateContentPromptTemplate(ctx context.Context, tenantID *int64, createdBy int64, req CreateTemplateRequest) (*TemplateResponse, error) {
	code := strings.TrimSpace(req.Code)
	if code == "" {
		code = fmt.Sprintf("content_tpl_%d", time.Now().UnixNano())
	}

	promptText := strings.TrimSpace(req.Template)
	if promptText == "" && req.PromptTemplate != nil {
		promptText = strings.TrimSpace(*req.PromptTemplate)
	}
	if promptText == "" {
		return nil, fmt.Errorf("template is required")
	}

	functionType := req.FunctionType
	if functionType == nil || strings.TrimSpace(*functionType) == "" {
		functionType = req.Category
	}
	if functionType == nil || strings.TrimSpace(*functionType) == "" {
		defaultType := "content_article"
		functionType = &defaultType
	}

	version := "1.0"
	if req.Version != nil && strings.TrimSpace(*req.Version) != "" {
		version = strings.TrimSpace(*req.Version)
	}

	variablesJSON, _ := json.Marshal(req.Variables)
	query := `
		INSERT INTO content_prompt_templates (
			tenant_id, name, code, description, prompt_template, variables, function_type, version,
			is_active, is_system, usage_count, created_at, updated_at, created_by
		)
		VALUES ($1,$2,$3,$4,$5,$6::json,$7,$8,TRUE,FALSE,0,NOW(),NOW(),$9)
		RETURNING id, tenant_id, name, code, description, prompt_template, variables, function_type,
		          version, is_active, is_system, usage_count, created_at, updated_at, created_by
	`
	row := s.pool.QueryRow(ctx, query, tenantID, req.Name, code, req.Description, promptText, string(variablesJSON), *functionType, version, createdBy)
	item, err := scanContentPromptTemplateRow(row)
	if err != nil {
		return nil, fmt.Errorf("failed to create content prompt template: %w", err)
	}
	return item, nil
}

func (s *Store) UpdateContentPromptTemplate(ctx context.Context, id int64, req UpdateTemplateRequest) (*TemplateResponse, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.Name))
		argIndex++
	}
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, req.Description)
		argIndex++
	}
	if req.FunctionType != nil {
		setClauses = append(setClauses, fmt.Sprintf("function_type = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.FunctionType))
		argIndex++
	} else if req.Category != nil {
		setClauses = append(setClauses, fmt.Sprintf("function_type = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.Category))
		argIndex++
	}
	if req.Version != nil {
		setClauses = append(setClauses, fmt.Sprintf("version = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.Version))
		argIndex++
	}
	if req.PromptTemplate != nil {
		setClauses = append(setClauses, fmt.Sprintf("prompt_template = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.PromptTemplate))
		argIndex++
	} else if req.Template != nil {
		setClauses = append(setClauses, fmt.Sprintf("prompt_template = $%d", argIndex))
		args = append(args, strings.TrimSpace(*req.Template))
		argIndex++
	}
	if req.Variables != nil {
		variablesJSON, _ := json.Marshal(req.Variables)
		setClauses = append(setClauses, fmt.Sprintf("variables = $%d::json", argIndex))
		args = append(args, string(variablesJSON))
		argIndex++
	}
	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetContentPromptTemplateByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf(`
		UPDATE content_prompt_templates
		SET %s
		WHERE id = $%d
		RETURNING id, tenant_id, name, code, description, prompt_template, variables, function_type,
		          version, is_active, is_system, usage_count, created_at, updated_at, created_by
	`, strings.Join(setClauses, ", "), argIndex)

	row := s.pool.QueryRow(ctx, query, args...)
	item, err := scanContentPromptTemplateRow(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("template not found")
		}
		return nil, fmt.Errorf("failed to update content prompt template: %w", err)
	}
	return item, nil
}

func (s *Store) DeleteContentPromptTemplate(ctx context.Context, id int64) error {
	result, err := s.pool.Exec(ctx, "DELETE FROM content_prompt_templates WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete content prompt template: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("template not found")
	}
	return nil
}

func (s *Store) CloneContentPromptTemplate(ctx context.Context, id int64, createdBy int64) (*TemplateResponse, error) {
	src, err := s.GetContentPromptTemplateByID(ctx, id)
	if err != nil {
		return nil, err
	}
	name := src.Name + " (副本)"
	req := CreateTemplateRequest{
		Name:           name,
		Code:           src.Code + "_copy",
		Description:    src.Description,
		FunctionType:   src.FunctionType,
		Version:        src.Version,
		PromptTemplate: &src.PromptTemplate,
		Variables:      src.Variables,
	}
	return s.CreateContentPromptTemplate(ctx, src.TenantID, createdBy, req)
}

func scanContentPromptTemplate(rows pgx.Rows) (TemplateResponse, error) {
	item, err := scanContentPromptTemplateRow(rows)
	if err != nil {
		return TemplateResponse{}, err
	}
	return *item, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanContentPromptTemplateRow(row rowScanner) (*TemplateResponse, error) {
	var (
		resp         TemplateResponse
		variablesRaw []byte
		createdByPtr *int64
		createdAt    *time.Time
		updatedAt    *time.Time
	)
	err := row.Scan(
		&resp.ID,
		&resp.TenantID,
		&resp.Name,
		&resp.Code,
		&resp.Description,
		&resp.PromptTemplate,
		&variablesRaw,
		&resp.FunctionType,
		&resp.Version,
		&resp.IsActive,
		&resp.IsSystem,
		&resp.UsageCount,
		&createdAt,
		&updatedAt,
		&createdByPtr,
	)
	if err != nil {
		return nil, err
	}
	if len(variablesRaw) > 0 {
		_ = json.Unmarshal(variablesRaw, &resp.Variables)
	}
	if createdByPtr != nil {
		resp.CreatedBy = *createdByPtr
	}
	if createdAt != nil {
		resp.CreatedAt = createdAt.Format(time.RFC3339)
	} else {
		resp.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if updatedAt != nil {
		resp.UpdatedAt = updatedAt.Format(time.RFC3339)
	} else {
		resp.UpdatedAt = resp.CreatedAt
	}
	resp.Template = resp.PromptTemplate
	resp.Category = resp.FunctionType
	biz := "content"
	table := "content_prompt_templates"
	resp.BusinessType = &biz
	resp.SourceTable = &table
	return &resp, nil
}

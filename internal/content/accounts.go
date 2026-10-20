package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ContentAccount struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	Name         string     `json:"name"`
	Platform     string     `json:"platform"`
	AccountType  string     `json:"account_type"`
	Status       string     `json:"status"`
	OwnerName    *string    `json:"owner_name,omitempty"`
	ReviewerName *string    `json:"reviewer_name,omitempty"`
	Audience     *string    `json:"audience,omitempty"`
	Forms        []string   `json:"forms"`
	Notes        *string    `json:"notes,omitempty"`
	ExtraData    JSONObject `json:"extra_data,omitempty"`
	CreatedBy    int64      `json:"created_by"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

type CreateContentAccountRequest struct {
	Name         string   `json:"name"`
	Platform     string   `json:"platform"`
	AccountType  string   `json:"account_type"`
	Status       string   `json:"status"`
	OwnerName    *string  `json:"owner_name"`
	ReviewerName *string  `json:"reviewer_name"`
	Audience     *string  `json:"audience"`
	Forms        []string `json:"forms"`
	Notes        *string  `json:"notes"`
}

type UpdateContentAccountRequest struct {
	Name         *string   `json:"name"`
	Platform     *string   `json:"platform"`
	AccountType  *string   `json:"account_type"`
	Status       *string   `json:"status"`
	OwnerName    *string   `json:"owner_name"`
	ReviewerName *string   `json:"reviewer_name"`
	Audience     *string   `json:"audience"`
	Forms        *[]string `json:"forms"`
	Notes        *string   `json:"notes"`
}

type ContentColumn struct {
	ID           int64      `json:"id"`
	TenantID     int64      `json:"tenant_id"`
	AccountID    int64      `json:"account_id"`
	Name         string     `json:"name"`
	Form         string     `json:"form"`
	Goal         *string    `json:"goal,omitempty"`
	Cadence      string     `json:"cadence"`
	WeeklyTarget int        `json:"weekly_target"`
	SortOrder    int        `json:"sort_order"`
	IsActive     bool       `json:"is_active"`
	ExtraData    JSONObject `json:"extra_data,omitempty"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

type CreateContentColumnRequest struct {
	Name         string  `json:"name"`
	Form         string  `json:"form"`
	Goal         *string `json:"goal"`
	Cadence      string  `json:"cadence"`
	WeeklyTarget int     `json:"weekly_target"`
	SortOrder    int     `json:"sort_order"`
}

type UpdateContentColumnRequest struct {
	Name         *string `json:"name"`
	Form         *string `json:"form"`
	Goal         *string `json:"goal"`
	Cadence      *string `json:"cadence"`
	WeeklyTarget *int    `json:"weekly_target"`
	SortOrder    *int    `json:"sort_order"`
	IsActive     *bool   `json:"is_active"`
}

type ContentPlanSlot struct {
	ID             int64   `json:"id"`
	TenantID       int64   `json:"tenant_id"`
	AccountID      int64   `json:"account_id"`
	ColumnID       int64   `json:"column_id"`
	WeekStart      string  `json:"week_start"`
	TargetCount    int     `json:"target_count"`
	ScheduledCount int     `json:"scheduled_count"`
	Gap            int     `json:"gap"`
	Status         string  `json:"status"`
	Notes          *string `json:"notes,omitempty"`
	AccountName    string  `json:"account_name"`
	Platform       string  `json:"platform"`
	ColumnName     string  `json:"column_name"`
	Form           string  `json:"form"`
}

func (s *Service) ListContentAccounts(ctx context.Context, tenantID int64) ([]ContentAccount, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT id, tenant_id, name, platform, account_type, status, owner_name, reviewer_name, audience,
		       forms, notes, COALESCE(extra_data,'{}'::jsonb), created_by, created_at, updated_at
		FROM content_accounts WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY updated_at DESC, id DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list content accounts: %w", err)
	}
	defer rows.Close()
	items := make([]ContentAccount, 0, 8)
	for rows.Next() {
		item, err := scanContentAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Service) GetContentAccount(ctx context.Context, tenantID, id int64) (*ContentAccount, error) {
	row := s.store.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, platform, account_type, status, owner_name, reviewer_name, audience,
		       forms, notes, COALESCE(extra_data,'{}'::jsonb), created_by, created_at, updated_at
		FROM content_accounts WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
	`, id, tenantID)
	return scanContentAccount(row)
}

func (s *Service) CreateContentAccount(ctx context.Context, tenantID, createdBy int64, req CreateContentAccountRequest) (*ContentAccount, error) {
	name := strings.TrimSpace(req.Name)
	platform := strings.TrimSpace(req.Platform)
	if name == "" || platform == "" {
		return nil, fmt.Errorf("name and platform are required")
	}
	accountType := strings.TrimSpace(req.AccountType)
	if accountType == "" {
		accountType = "institution"
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "normal"
	}
	forms := req.Forms
	if forms == nil {
		forms = []string{}
	}
	formsJSON, _ := json.Marshal(forms)
	row := s.store.pool.QueryRow(ctx, `
		INSERT INTO content_accounts
			(tenant_id, name, platform, account_type, status, owner_name, reviewer_name, audience, forms, notes, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11)
		RETURNING id, tenant_id, name, platform, account_type, status, owner_name, reviewer_name, audience,
		          forms, notes, COALESCE(extra_data,'{}'::jsonb), created_by, created_at, updated_at
	`, tenantID, name, platform, accountType, status, req.OwnerName, req.ReviewerName, req.Audience, string(formsJSON), req.Notes, createdBy)
	return scanContentAccount(row)
}

func (s *Service) UpdateContentAccount(ctx context.Context, tenantID, id int64, req UpdateContentAccountRequest) (*ContentAccount, error) {
	var formsJSON *string
	if req.Forms != nil {
		raw, _ := json.Marshal(*req.Forms)
		value := string(raw)
		formsJSON = &value
	}
	row := s.store.pool.QueryRow(ctx, `
		UPDATE content_accounts SET
			name=COALESCE($3,name), platform=COALESCE($4,platform), account_type=COALESCE($5,account_type),
			status=COALESCE($6,status), owner_name=COALESCE($7,owner_name), reviewer_name=COALESCE($8,reviewer_name),
			audience=COALESCE($9,audience), forms=COALESCE($10::jsonb,forms), notes=COALESCE($11,notes), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		RETURNING id, tenant_id, name, platform, account_type, status, owner_name, reviewer_name, audience,
		          forms, notes, COALESCE(extra_data,'{}'::jsonb), created_by, created_at, updated_at
	`, id, tenantID, req.Name, req.Platform, req.AccountType, req.Status, req.OwnerName, req.ReviewerName, req.Audience, formsJSON, req.Notes)
	return scanContentAccount(row)
}

func (s *Service) DeleteContentAccount(ctx context.Context, tenantID, id int64) error {
	result, err := s.store.pool.Exec(ctx, `UPDATE content_accounts SET deleted_at=NOW(), updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete content account: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("account not found")
	}
	return nil
}

func (s *Service) ListContentColumns(ctx context.Context, tenantID, accountID int64) ([]ContentColumn, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT id, tenant_id, account_id, name, form, goal, cadence, weekly_target, sort_order, is_active,
		       COALESCE(extra_data,'{}'::jsonb), created_at, updated_at
		FROM content_account_columns
		WHERE tenant_id=$1 AND ($2=0 OR account_id=$2) AND deleted_at IS NULL
		ORDER BY account_id, sort_order, id
	`, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("list content columns: %w", err)
	}
	defer rows.Close()
	items := make([]ContentColumn, 0, 8)
	for rows.Next() {
		item, err := scanContentColumn(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Service) CreateContentColumn(ctx context.Context, tenantID, accountID int64, req CreateContentColumnRequest) (*ContentColumn, error) {
	name := strings.TrimSpace(req.Name)
	form := strings.TrimSpace(req.Form)
	if name == "" || form == "" {
		return nil, fmt.Errorf("name and form are required")
	}
	if _, err := s.GetContentAccount(ctx, tenantID, accountID); err != nil {
		return nil, fmt.Errorf("account not found")
	}
	cadence := strings.TrimSpace(req.Cadence)
	if cadence == "" {
		cadence = "weekly"
	}
	target := req.WeeklyTarget
	if target <= 0 {
		target = 1
	}
	row := s.store.pool.QueryRow(ctx, `
		INSERT INTO content_account_columns
			(tenant_id, account_id, name, form, goal, cadence, weekly_target, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, tenant_id, account_id, name, form, goal, cadence, weekly_target, sort_order, is_active,
		          COALESCE(extra_data,'{}'::jsonb), created_at, updated_at
	`, tenantID, accountID, name, form, req.Goal, cadence, target, req.SortOrder)
	return scanContentColumn(row)
}

func (s *Service) UpdateContentColumn(ctx context.Context, tenantID, id int64, req UpdateContentColumnRequest) (*ContentColumn, error) {
	row := s.store.pool.QueryRow(ctx, `
		UPDATE content_account_columns SET
			name=COALESCE($3,name), form=COALESCE($4,form), goal=COALESCE($5,goal), cadence=COALESCE($6,cadence),
			weekly_target=COALESCE($7,weekly_target), sort_order=COALESCE($8,sort_order),
			is_active=COALESCE($9,is_active), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		RETURNING id, tenant_id, account_id, name, form, goal, cadence, weekly_target, sort_order, is_active,
		          COALESCE(extra_data,'{}'::jsonb), created_at, updated_at
	`, id, tenantID, req.Name, req.Form, req.Goal, req.Cadence, req.WeeklyTarget, req.SortOrder, req.IsActive)
	return scanContentColumn(row)
}

func (s *Service) DeleteContentColumn(ctx context.Context, tenantID, id int64) error {
	result, err := s.store.pool.Exec(ctx, `UPDATE content_account_columns SET deleted_at=NOW(), updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete content column: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("column not found")
	}
	return nil
}

func (s *Service) ListContentPlanSlots(ctx context.Context, tenantID int64, weekStart string) ([]ContentPlanSlot, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT s.id, s.tenant_id, s.account_id, s.column_id, s.week_start::text, s.target_count,
		       COUNT(ci.id) FILTER (WHERE ci.deleted_at IS NULL AND (ci.extra_data->>'plan_slot_id') ~ '^[0-9]+$'
		            AND (ci.extra_data->>'plan_slot_id')::bigint = s.id) AS scheduled_count,
		       GREATEST(s.target_count - COUNT(ci.id) FILTER (WHERE ci.deleted_at IS NULL AND (ci.extra_data->>'plan_slot_id') ~ '^[0-9]+$'
		            AND (ci.extra_data->>'plan_slot_id')::bigint = s.id), 0) AS gap,
		       s.status, s.notes, a.name, a.platform, c.name, c.form
		FROM content_plan_slots s
		JOIN content_accounts a ON a.id=s.account_id AND a.tenant_id=s.tenant_id AND a.deleted_at IS NULL
		JOIN content_account_columns c ON c.id=s.column_id AND c.tenant_id=s.tenant_id AND c.deleted_at IS NULL
		LEFT JOIN content_items ci ON ci.tenant_id=s.tenant_id
		WHERE s.tenant_id=$1 AND s.week_start=$2::date
		GROUP BY s.id, s.tenant_id, s.account_id, s.column_id, s.week_start, s.target_count, s.status, s.notes, a.name, a.platform, c.name, c.form, c.sort_order
		ORDER BY a.name, c.sort_order, s.id
	`, tenantID, weekStart)
	if err != nil {
		return nil, fmt.Errorf("list content plan slots: %w", err)
	}
	defer rows.Close()
	items := make([]ContentPlanSlot, 0, 8)
	for rows.Next() {
		var item ContentPlanSlot
		if err := rows.Scan(&item.ID, &item.TenantID, &item.AccountID, &item.ColumnID, &item.WeekStart, &item.TargetCount, &item.ScheduledCount, &item.Gap, &item.Status, &item.Notes, &item.AccountName, &item.Platform, &item.ColumnName, &item.Form); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GenerateContentPlanWeek(ctx context.Context, tenantID, createdBy int64, weekStart string) ([]ContentPlanSlot, error) {
	if _, err := time.Parse("2006-01-02", weekStart); err != nil {
		return nil, fmt.Errorf("week_start must be YYYY-MM-DD")
	}
	_, err := s.store.pool.Exec(ctx, `
		INSERT INTO content_plan_slots (tenant_id, account_id, column_id, week_start, target_count, status, created_by)
		SELECT a.tenant_id, a.id, c.id, $2::date, c.weekly_target, 'planned', $3
		FROM content_accounts a
		JOIN content_account_columns c ON c.account_id=a.id AND c.tenant_id=a.tenant_id AND c.is_active AND c.deleted_at IS NULL
		WHERE a.tenant_id=$1 AND a.deleted_at IS NULL AND a.status='normal'
		ON CONFLICT (tenant_id, account_id, column_id, week_start) DO NOTHING
	`, tenantID, weekStart, createdBy)
	if err != nil {
		return nil, fmt.Errorf("generate content plan week: %w", err)
	}
	return s.ListContentPlanSlots(ctx, tenantID, weekStart)
}

func (s *Service) UpdateContentPlanSlot(ctx context.Context, tenantID, id int64, targetCount *int, status *string, notes *string) (*ContentPlanSlot, error) {
	_, err := s.store.pool.Exec(ctx, `
		UPDATE content_plan_slots SET target_count=COALESCE($3,target_count), status=COALESCE($4,status),
		       notes=COALESCE($5,notes), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
	`, id, tenantID, targetCount, status, notes)
	if err != nil {
		return nil, fmt.Errorf("update content plan slot: %w", err)
	}
	var weekStart string
	if err := s.store.pool.QueryRow(ctx, `SELECT week_start::text FROM content_plan_slots WHERE id=$1 AND tenant_id=$2`, id, tenantID).Scan(&weekStart); err != nil {
		return nil, fmt.Errorf("plan slot not found")
	}
	slots, err := s.ListContentPlanSlots(ctx, tenantID, weekStart)
	if err != nil {
		return nil, err
	}
	for _, slot := range slots {
		if slot.ID == id {
			return &slot, nil
		}
	}
	return nil, fmt.Errorf("plan slot not found")
}

func (s *Service) AssignContentToPlanSlot(ctx context.Context, tenantID, slotID, contentID int64) error {
	var accountID, columnID int64
	if err := s.store.pool.QueryRow(ctx, `SELECT account_id, column_id FROM content_plan_slots WHERE id=$1 AND tenant_id=$2`, slotID, tenantID).Scan(&accountID, &columnID); err != nil {
		return fmt.Errorf("plan slot not found")
	}
	result, err := s.store.pool.Exec(ctx, `
		UPDATE content_items
		SET extra_data = COALESCE(extra_data,'{}'::jsonb) || jsonb_build_object(
			'plan_slot_id', $3::bigint, 'account_id', $4::bigint, 'column_id', $5::bigint
		), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
	`, contentID, tenantID, slotID, accountID, columnID)
	if err != nil {
		return fmt.Errorf("assign content to plan slot: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("content not found")
	}
	_, _ = s.store.pool.Exec(ctx, `UPDATE content_plan_slots SET updated_at=NOW() WHERE id=$1`, slotID)
	return nil
}

type accountScanner interface{ Scan(dest ...any) error }

func scanContentAccount(row accountScanner) (*ContentAccount, error) {
	var item ContentAccount
	var formsRaw []byte
	var extraRaw []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(&item.ID, &item.TenantID, &item.Name, &item.Platform, &item.AccountType, &item.Status, &item.OwnerName, &item.ReviewerName, &item.Audience, &formsRaw, &item.Notes, &extraRaw, &item.CreatedBy, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("scan content account: %w", err)
	}
	if len(formsRaw) > 0 {
		_ = json.Unmarshal(formsRaw, &item.Forms)
	}
	if len(extraRaw) > 0 {
		_ = json.Unmarshal(extraRaw, &item.ExtraData)
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	return &item, nil
}

func scanContentColumn(row accountScanner) (*ContentColumn, error) {
	var item ContentColumn
	var extraRaw []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(&item.ID, &item.TenantID, &item.AccountID, &item.Name, &item.Form, &item.Goal, &item.Cadence, &item.WeeklyTarget, &item.SortOrder, &item.IsActive, &extraRaw, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("scan content column: %w", err)
	}
	if len(extraRaw) > 0 {
		_ = json.Unmarshal(extraRaw, &item.ExtraData)
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	return &item, nil
}

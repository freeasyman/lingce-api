package content

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type PlanItem struct {
	ID                 int64   `json:"id"`
	TenantID           int64   `json:"tenant_id"`
	AccountID          int64   `json:"account_id"`
	ColumnID           *int64  `json:"column_id,omitempty"`
	ColumnNameSnapshot string  `json:"column_name_snapshot"`
	MonthStart         string  `json:"month_start"`
	PlannedDate        *string `json:"planned_date,omitempty"`
	Title              string  `json:"title"`
	TopicType          string  `json:"topic_type"`
	TopicID            *int64  `json:"topic_id,omitempty"`
	Status             string  `json:"status"`
	ContentID          *int64  `json:"content_id,omitempty"`
	ContentTitle       string  `json:"content_title,omitempty"`
	ContentStatus      string  `json:"content_status,omitempty"`
	SortOrder          int     `json:"sort_order"`
	AccountName        string  `json:"account_name"`
	ColumnName         string  `json:"column_name,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func (h *Handler) registerPlanItemRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	router.Register(mux, []router.Route{
		{Method: http.MethodGet, Path: "/api/v1/content-plans/items", Handler: h.ListPlanItems, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPost, Path: "/api/v1/content-plans/items", Handler: h.CreatePlanItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPut, Path: "/api/v1/content-plans/items/{id}", Handler: h.UpdatePlanItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodDelete, Path: "/api/v1/content-plans/items/{id}", Handler: h.DeletePlanItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPost, Path: "/api/v1/content-plans/items/{id}/link-content", Handler: h.LinkPlanItemContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodDelete, Path: "/api/v1/content-plans/items/{id}/content", Handler: h.UnlinkPlanItemContent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, deps)
}

func (s *Service) ListPlanItems(ctx context.Context, tenantID int64, month string, weekStart string) ([]PlanItem, error) {
	if len(month) == 7 {
		month += "-01"
	}
	rows, err := s.store.pool.Query(ctx, `
		SELECT p.id, p.tenant_id, p.account_id, p.column_id, p.column_name_snapshot, p.month_start::text,
		       p.planned_date::text, p.title, p.topic_type, p.topic_id, p.status, p.content_id,
		       COALESCE(ci.title,''), COALESCE(ci.status,''), p.sort_order,
		       a.name, COALESCE(c.name,''), p.created_at::text, p.updated_at::text
		FROM content_plan_items p
		JOIN content_accounts a ON a.id=p.account_id AND a.tenant_id=p.tenant_id
		LEFT JOIN content_account_columns c ON c.id=p.column_id
		LEFT JOIN content_items ci ON ci.id=p.content_id AND ci.deleted_at IS NULL
		WHERE p.tenant_id=$1
		  AND ($2='' OR p.month_start=$2::date)
		  AND ($3='' OR p.planned_date >= $3::date)
		ORDER BY a.name, COALESCE(c.sort_order,999), p.planned_date NULLS LAST, p.sort_order, p.id
	`, tenantID, month, weekStart)
	if err != nil {
		return nil, fmt.Errorf("list plan items: %w", err)
	}
	defer rows.Close()
	items := []PlanItem{}
	for rows.Next() {
		var item PlanItem
		if err := rows.Scan(&item.ID, &item.TenantID, &item.AccountID, &item.ColumnID, &item.ColumnNameSnapshot, &item.MonthStart,
			&item.PlannedDate, &item.Title, &item.TopicType, &item.TopicID, &item.Status, &item.ContentID,
			&item.ContentTitle, &item.ContentStatus, &item.SortOrder, &item.AccountName, &item.ColumnName,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) ListPlanItems(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListPlanItems(r.Context(), tenantID, strings.TrimSpace(r.URL.Query().Get("month")), strings.TrimSpace(r.URL.Query().Get("week_start")))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) CreatePlanItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	var req struct {
		AccountID   int64   `json:"account_id"`
		ColumnID    *int64  `json:"column_id"`
		MonthStart  string  `json:"month_start"`
		PlannedDate *string `json:"planned_date"`
		Title       string  `json:"title"`
		TopicType   string  `json:"topic_type"`
		TopicID     *int64  `json:"topic_id"`
		Status      string  `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	defer func() {
		if recover() != nil {
			httputil.WriteBadRequest(w, "Invalid request body")
		}
	}()
	item, err := h.service.CreatePlanItem(r.Context(), tenantID, claims.UserID, req.AccountID, req.ColumnID, req.MonthStart, req.PlannedDate, strings.TrimSpace(req.Title), req.TopicType, req.TopicID, req.Status)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) UpdatePlanItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		ColumnID    *int64  `json:"column_id"`
		PlannedDate *string `json:"planned_date"`
		Title       *string `json:"title"`
		Status      *string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.UpdatePlanItem(r.Context(), tenantID, id, req.ColumnID, req.PlannedDate, req.Title, req.Status)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) DeletePlanItem(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := h.service.DeletePlanItem(r.Context(), tenantID, id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"status": "deleted", "id": id})
}

func (h *Handler) LinkPlanItemContent(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		ContentID int64 `json:"content_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || req.ContentID <= 0 {
		httputil.WriteBadRequest(w, "content_id is required")
		return
	}
	if err := h.service.LinkPlanItemContent(r.Context(), tenantID, id, req.ContentID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"status": "linked", "id": id, "content_id": req.ContentID})
}

func (h *Handler) UnlinkPlanItemContent(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := h.service.UnlinkPlanItemContent(r.Context(), tenantID, id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"status": "unlinked", "id": id})
}

func (s *Service) CreatePlanItem(ctx context.Context, tenantID, createdBy, accountID int64, columnID *int64, monthStart string, plannedDate *string, title, topicType string, topicID *int64, status string) (*PlanItem, error) {
	if accountID <= 0 || title == "" {
		return nil, fmt.Errorf("account_id and title are required")
	}
	if status == "" {
		status = "todo"
	}
	if topicType == "" {
		topicType = "manual"
	}
	var accountExists bool
	if err := s.store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM content_accounts WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL)`, accountID, tenantID).Scan(&accountExists); err != nil || !accountExists {
		return nil, fmt.Errorf("account not found")
	}
	columnName := ""
	if columnID != nil && *columnID > 0 {
		if err := s.store.pool.QueryRow(ctx, `SELECT name FROM content_account_columns WHERE id=$1 AND account_id=$2 AND tenant_id=$3 AND deleted_at IS NULL`, *columnID, accountID, tenantID).Scan(&columnName); err != nil {
			return nil, fmt.Errorf("column not found")
		}
	}
	var id int64
	err := s.store.pool.QueryRow(ctx, `
		INSERT INTO content_plan_items (tenant_id, account_id, column_id, column_name_snapshot, month_start, planned_date, title, topic_type, topic_id, status, created_by)
		VALUES ($1,$2,$3,$4,$5::date,$6::date,$7,$8,$9,$10,$11) RETURNING id
	`, tenantID, accountID, columnID, columnName, monthStart, plannedDate, title, topicType, topicID, status, createdBy).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create plan item: %w", err)
	}
	return s.GetPlanItem(ctx, tenantID, id)
}

func (s *Service) GetPlanItem(ctx context.Context, tenantID, id int64) (*PlanItem, error) {
	items, err := s.ListPlanItems(ctx, tenantID, "", "")
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ID == id {
			return &item, nil
		}
	}
	return nil, fmt.Errorf("plan item not found")
}

func (s *Service) UpdatePlanItem(ctx context.Context, tenantID, id int64, columnID *int64, plannedDate *string, title *string, status *string) (*PlanItem, error) {
	if title != nil && strings.TrimSpace(*title) == "" {
		return nil, fmt.Errorf("title cannot be empty")
	}
	columnName := ""
	if columnID != nil && *columnID > 0 {
		if err := s.store.pool.QueryRow(ctx, `SELECT name FROM content_account_columns WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`, *columnID, tenantID).Scan(&columnName); err != nil {
			return nil, fmt.Errorf("column not found")
		}
	}
	_, err := s.store.pool.Exec(ctx, `
		UPDATE content_plan_items SET
		  column_id=CASE WHEN $3::bigint IS NULL THEN column_id WHEN $3::bigint = 0 THEN NULL ELSE $3::bigint END,
		  column_name_snapshot=CASE WHEN $3::bigint IS NULL THEN column_name_snapshot WHEN $3::bigint = 0 THEN '' ELSE $4 END,
		  planned_date=CASE WHEN $5::text IS NULL THEN planned_date WHEN $5::text = '' THEN NULL ELSE $5::date END,
		  title=COALESCE($6,title), status=COALESCE($7,status), updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
	`, id, tenantID, columnID, columnName, plannedDate, title, status)
	if err != nil {
		return nil, fmt.Errorf("update plan item: %w", err)
	}
	return s.GetPlanItem(ctx, tenantID, id)
}

func (s *Service) DeletePlanItem(ctx context.Context, tenantID, id int64) error {
	result, err := s.store.pool.Exec(ctx, `DELETE FROM content_plan_items WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete plan item: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("plan item not found")
	}
	return nil
}

func (s *Service) LinkPlanItemContent(ctx context.Context, tenantID, itemID, contentID int64) error {
	var exists bool
	if err := s.store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM content_items WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL)`, contentID, tenantID).Scan(&exists); err != nil || !exists {
		return fmt.Errorf("content not found")
	}
	if err := s.store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM content_plan_items WHERE content_id=$1 AND id<>$2)`, contentID, itemID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("这条内容已经安排到其他计划项")
	}
	result, err := s.store.pool.Exec(ctx, `UPDATE content_plan_items SET content_id=$3, status=CASE WHEN status='todo' THEN 'drafting' ELSE status END, updated_at=NOW() WHERE id=$1 AND tenant_id=$2`, itemID, tenantID, contentID)
	if err != nil {
		return fmt.Errorf("link content: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("plan item not found")
	}
	return nil
}

func (s *Service) UnlinkPlanItemContent(ctx context.Context, tenantID, itemID int64) error {
	_, err := s.store.pool.Exec(ctx, `UPDATE content_plan_items SET content_id=NULL, updated_at=NOW() WHERE id=$1 AND tenant_id=$2`, itemID, tenantID)
	return err
}

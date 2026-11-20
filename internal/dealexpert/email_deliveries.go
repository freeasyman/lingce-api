package dealexpert

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// EmailDeliveryItem 是邮件发送记录的只读投影；不返回邮件正文。
type EmailDeliveryItem struct {
	ID                  int64      `json:"id"`
	CreatedAt           time.Time  `json:"created_at"`
	TenantID            int64      `json:"tenant_id"`
	TenantName          string     `json:"tenant_name"`
	EventID             int64      `json:"event_id"`
	RecordingID         int64      `json:"recording_id"`
	EventType           string     `json:"event_type"`
	CustomerName        string     `json:"customer_name"`
	ConsultantName      string     `json:"consultant_name"`
	RecipientEmployeeID int64      `json:"recipient_employee_id"`
	RecipientName       string     `json:"recipient_name"`
	Email               string     `json:"email"`
	Kind                string     `json:"kind"`
	Status              string     `json:"status"`
	Error               string     `json:"error"`
	MessageID           string     `json:"message_id"`
	SentAt              *time.Time `json:"sent_at"`
}

type EmailDeliveryQuery struct {
	TenantID int64
	From     time.Time
	To       time.Time
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

// ListEmailDeliveries 查询最近 180 天投递记录；tenantID=0 表示运营跨租户。
func (s *Store) ListEmailDeliveries(ctx context.Context, q EmailDeliveryQuery) ([]*EmailDeliveryItem, int, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 || q.PageSize > 100 {
		q.PageSize = 20
	}
	if q.From.IsZero() {
		q.From = time.Now().AddDate(0, 0, -180)
	}
	if q.To.IsZero() {
		q.To = time.Now().AddDate(0, 0, 1)
	}
	keyword := "%" + strings.TrimSpace(q.Keyword) + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.created_at, d.tenant_id, COALESCE(t.name, ''),
		       d.event_id, COALESCE(e.recording_id, 0),
		       COALESCE(e.payload->>'recipient_role', 'consultant'),
		       COALESCE(c.name, ''), COALESCE(cons.name, ''),
		       d.recipient_employee_id, COALESCE(emp.name, ''),
		       d.email, d.kind, d.status, d.error, d.message_id, d.sent_at,
		       COUNT(*) OVER() AS total
		FROM deal_email_deliveries d
		JOIN deal_push_events e ON e.id = d.event_id AND e.tenant_id = d.tenant_id
		LEFT JOIN tenants t ON t.id = d.tenant_id
		LEFT JOIN employees emp ON emp.id = d.recipient_employee_id AND emp.tenant_id = d.tenant_id
		LEFT JOIN recordings rec ON rec.id = e.recording_id AND rec.tenant_id = d.tenant_id
		LEFT JOIN employees cons ON cons.id = rec.employee_id AND cons.tenant_id = d.tenant_id
		LEFT JOIN customers c ON c.id = rec.customer_id
		WHERE d.created_at >= $1 AND d.created_at < $2
		  AND ($3 = 0 OR d.tenant_id = $3)
		  AND ($4 = '' OR d.status = $4)
		  AND ($5 = '' OR COALESCE(emp.name, '') ILIKE $5 OR d.email ILIKE $5
		       OR COALESCE(c.name, '') ILIKE $5 OR COALESCE(cons.name, '') ILIKE $5)
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT $6 OFFSET $7
	`, q.From, q.To, q.TenantID, q.Status, keyword, q.PageSize, (q.Page-1)*q.PageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list email deliveries: %w", err)
	}
	defer rows.Close()
	items := make([]*EmailDeliveryItem, 0, q.PageSize)
	total := 0
	for rows.Next() {
		var item EmailDeliveryItem
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.TenantID, &item.TenantName,
			&item.EventID, &item.RecordingID, &item.EventType,
			&item.CustomerName, &item.ConsultantName,
			&item.RecipientEmployeeID, &item.RecipientName,
			&item.Email, &item.Kind, &item.Status, &item.Error, &item.MessageID, &item.SentAt,
			&total); err != nil {
			return nil, 0, fmt.Errorf("scan email delivery: %w", err)
		}
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Service) ListEmailDeliveries(ctx context.Context, q EmailDeliveryQuery) ([]*EmailDeliveryItem, int, error) {
	return s.store.ListEmailDeliveries(ctx, q)
}

func parseEmailDeliveryQuery(r *http.Request) EmailDeliveryQuery {
	q := EmailDeliveryQuery{
		Status:  strings.TrimSpace(r.URL.Query().Get("status")),
		Keyword: strings.TrimSpace(r.URL.Query().Get("keyword")),
	}
	if q.Status != "sent" && q.Status != "failed" && q.Status != "skipped" {
		q.Status = ""
	}
	q.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	q.PageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		q.TenantID, _ = strconv.ParseInt(v, 10, 64)
	}
	q.From = parseDeliveryDate(r.URL.Query().Get("from"), time.Now().AddDate(0, 0, -180))
	if to := parseDeliveryDate(r.URL.Query().Get("to"), time.Time{}); !to.IsZero() {
		q.To = to.AddDate(0, 0, 1)
	}
	return q
}

func parseDeliveryDate(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	t, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return fallback
	}
	return t
}

// ListEmailDeliveries 机构管理员查看本机构投递记录。
func (h *Handler) ListEmailDeliveries(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	if !scope.Manager {
		writeTenantError(w, &tenantError{status: http.StatusForbidden, message: "只有机构管理员可以查看邮件发送记录"})
		return
	}
	q := parseEmailDeliveryQuery(r)
	q.TenantID = scope.TenantID
	items, total, err := h.service.ListEmailDeliveries(r.Context(), q)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "page": q.Page, "page_size": q.PageSize})
}

// ListEmailDeliveriesForOperation 运营后台跨租户查看投递记录。
func (h *Handler) ListEmailDeliveriesForOperation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserType != auth.UserTypeAdmin {
		httputil.WriteError(w, http.StatusForbidden, "FORBIDDEN", "only operations admins can view email deliveries", nil)
		return
	}
	q := parseEmailDeliveryQuery(r)
	items, total, err := h.service.ListEmailDeliveries(r.Context(), q)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "page": q.Page, "page_size": q.PageSize})
}

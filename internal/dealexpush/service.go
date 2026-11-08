package dealexpush

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrShortLinkNotFound = errors.New("short link not found")
	ErrShortLinkExpired  = errors.New("short link expired")
)

// ShortLinkTTL 短链有效期：7 天。
const ShortLinkTTL = 7 * 24 * time.Hour

type Service struct {
	pool *pgxpool.Pool
	cfg  config.DealPushConfig
	log  *slog.Logger
}

func NewService(pool *pgxpool.Pool, cfg config.DealPushConfig, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{pool: pool, cfg: cfg, log: log}
}

// PushEvent 是一次推送事件（当前阶段只有 dry_run 状态）。
type PushEvent struct {
	ID                  int64           `json:"id"`
	TenantID            int64           `json:"tenant_id"`
	RecordingID         int64           `json:"recording_id"`
	AnalysisVersion     string          `json:"analysis_version"`
	RecipientEmployeeID int64           `json:"recipient_employee_id"`
	Channel             string          `json:"channel"`
	TemplateCode        string          `json:"template_code"`
	ShortCode           string          `json:"short_code"`
	Payload             json.RawMessage `json:"payload"`
	Status              string          `json:"status"`
	Error               string          `json:"error"`
	CreatedAt           time.Time       `json:"created_at"`
	SentAt              *time.Time      `json:"sent_at,omitempty"`
	ReadAt              *time.Time      `json:"read_at,omitempty"`
	HandledAt           *time.Time      `json:"handled_at,omitempty"`
}

type ResolvedLink struct {
	Code        string
	TenantID    int64
	RecordingID int64
	EmployeeID  int64
	ExpiresAt   time.Time
}

// CreateForAnalysis 在分析保存成功后生成推送事件（干跑：只记录、不发送）。
//
// TODO(real send)：产品确认推送内容后，在此处按 cfg.Enabled && !cfg.IsDryRun() 调用
// 短信/企微发送接口，并更新事件状态（dry_run → sent）。当前阶段绝不发送。
func (s *Service) CreateForAnalysis(ctx context.Context, tenantID, recordingID int64, analysisVersion string) error {
	if tenantID <= 0 || recordingID <= 0 {
		return fmt.Errorf("tenant_id and recording_id are required")
	}
	var employeeID int64
	var customerName, gender string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(r.employee_id, 0), COALESCE(c.name, ''), COALESCE(c.gender, '')
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.id = $1 AND r.tenant_id = $2 AND r.deleted_at IS NULL
	`, recordingID, tenantID).Scan(&employeeID, &customerName, &gender)
	if err != nil {
		return fmt.Errorf("load recording for push: %w", err)
	}
	if employeeID <= 0 {
		return fmt.Errorf("recording has no consultant, skip push")
	}
	version := strings.TrimSpace(analysisVersion)
	if version == "" {
		version = "v0.16"
	}

	// 幂等：同一 recording+version+recipient 已存在事件则直接跳过，不重复生成短链。
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM deal_push_events
		              WHERE tenant_id=$1 AND recording_id=$2 AND analysis_version=$3 AND recipient_employee_id=$4)
	`, tenantID, recordingID, version, employeeID).Scan(&exists); err != nil {
		return fmt.Errorf("check push event exists: %w", err)
	}
	if exists {
		s.log.Info("DRY RUN push: event already exists, skip", "recording_id", recordingID, "recipient", employeeID)
		return nil
	}

	code, err := s.createShortLink(ctx, tenantID, recordingID, employeeID)
	if err != nil {
		return fmt.Errorf("create short link: %w", err)
	}

	// 本期只生成咨询师模板事件；负责人模板/P0 信号留待产品确认（TODO）。
	// channel 先固定 sms；企微通道后续扩展。
	channel := "sms"
	template := s.pickTemplate()
	payload := map[string]any{
		"name":     salutationFor(customerName, gender),
		"code":     code,
		"link":     s.shortURL(code),
		"template": template,
		"sign":     s.cfg.SMSSignName,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO deal_push_events
		  (tenant_id, recording_id, analysis_version, recipient_employee_id, channel, template_code, short_code, payload, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,'dry_run')
		ON CONFLICT (tenant_id, recording_id, analysis_version, recipient_employee_id) DO NOTHING
	`, tenantID, recordingID, version, employeeID, channel, template, code, string(raw))
	if err != nil {
		return fmt.Errorf("insert push event: %w", err)
	}

	// 干跑日志：只记录摘要，不调用任何发送接口。
	s.log.Info("DRY RUN push",
		"tenant_id", tenantID,
		"recording_id", recordingID,
		"analysis_version", version,
		"recipient", employeeID,
		"channel", channel,
		"template", template,
		"short_code", code,
		"link", s.shortURL(code),
		"name", salutationFor(customerName, gender),
	)
	return nil
}

// pickTemplate 选择模板：默认咨询师模板，未配置时用兜底模板。
func (s *Service) pickTemplate() string {
	if template := strings.TrimSpace(s.cfg.SMSTemplateConsultant); template != "" {
		return template
	}
	return strings.TrimSpace(s.cfg.SMSTemplateFallback)
}

// shortURL 拼接完整短链地址。
func (s *Service) shortURL(code string) string {
	base := strings.TrimRight(strings.TrimSpace(s.cfg.ShortLinkBase), "/")
	if base == "" {
		base = "https://khgl.xyz/s"
	}
	return base + "/" + code
}

// createShortLink 生成 8 位短链码（首位英文，其余字母数字）并写入 deal_short_links；撞码自动重试。
func (s *Service) createShortLink(ctx context.Context, tenantID, recordingID, employeeID int64) (string, error) {
	for attempt := 0; attempt < 6; attempt++ {
		code := newShortCode()
		_, err := s.pool.Exec(ctx, `
			INSERT INTO deal_short_links (code, tenant_id, recording_id, employee_id, expires_at)
			VALUES ($1,$2,$3,$4,$5)
		`, code, tenantID, recordingID, employeeID, time.Now().Add(ShortLinkTTL))
		if err == nil {
			return code, nil
		}
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			continue
		}
		return "", fmt.Errorf("insert short link: %w", err)
	}
	return "", fmt.Errorf("short code collision after retries")
}

var shortCodeFirst = []byte("ABCDEFGHJKLMNPQRSTUVWXYZ")
var shortCodeRest = []byte("ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789")

func newShortCode() string {
	buf := make([]byte, 8)
	first := make([]byte, 1)
	if _, err := rand.Read(first); err == nil {
		buf[0] = shortCodeFirst[int(first[0])%len(shortCodeFirst)]
	} else {
		buf[0] = 'A'
	}
	rest := make([]byte, 7)
	if _, err := rand.Read(rest); err != nil {
		for i := range rest {
			rest[i] = '0'
		}
	}
	for i := 0; i < 7; i++ {
		buf[i+1] = shortCodeRest[int(rest[i])%len(shortCodeRest)]
	}
	return string(buf)
}

// salutationFor 生成称呼：姓名 ≤5 字直接使用；否则取姓 + 女士/先生。
func salutationFor(name, gender string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "客户"
	}
	runes := []rune(name)
	if len(runes) <= 5 {
		return name
	}
	suffix := "先生"
	lower := strings.ToLower(strings.TrimSpace(gender))
	if strings.Contains(lower, "女") || strings.Contains(lower, "female") || strings.Contains(lower, "f") {
		suffix = "女士"
	}
	return string(runes[0]) + suffix
}

// ResolveShortLink 校验短链存在且未过期；不存在返回 ErrShortLinkNotFound，过期返回 ErrShortLinkExpired。
func (s *Service) ResolveShortLink(ctx context.Context, code string) (*ResolvedLink, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrShortLinkNotFound
	}
	var item ResolvedLink
	err := s.pool.QueryRow(ctx, `
		SELECT code, tenant_id, recording_id, employee_id, expires_at
		FROM deal_short_links WHERE code = $1
	`, code).Scan(&item.Code, &item.TenantID, &item.RecordingID, &item.EmployeeID, &item.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrShortLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resolve short link: %w", err)
	}
	if time.Now().After(item.ExpiresAt) {
		return nil, ErrShortLinkExpired
	}
	return &item, nil
}

// ListEvents 返回租户推送事件（管理员只读，供验收）；recordingID>0 时按录音过滤。
func (s *Service) ListEvents(ctx context.Context, tenantID, recordingID int64) ([]*PushEvent, error) {
	query := `
		SELECT id, tenant_id, recording_id, analysis_version, recipient_employee_id,
		       channel, template_code, short_code, payload, status, error,
		       created_at, sent_at, read_at, handled_at
		FROM deal_push_events
		WHERE tenant_id = $1`
	args := []any{tenantID}
	if recordingID > 0 {
		args = append(args, recordingID)
		query += fmt.Sprintf(" AND recording_id = $%d", len(args))
	}
	query += " ORDER BY id DESC LIMIT 100"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list push events: %w", err)
	}
	defer rows.Close()
	items := make([]*PushEvent, 0)
	for rows.Next() {
		var item PushEvent
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RecordingID, &item.AnalysisVersion, &item.RecipientEmployeeID,
			&item.Channel, &item.TemplateCode, &item.ShortCode, &item.Payload, &item.Status, &item.Error,
			&item.CreatedAt, &item.SentAt, &item.ReadAt, &item.HandledAt); err != nil {
			return nil, fmt.Errorf("scan push event: %w", err)
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

// EmployeeIsManager 判断员工是否为负责人角色（决定短链落到咨询师页还是负责人介入页）。
func (s *Service) EmployeeIsManager(ctx context.Context, employeeID int64) bool {
	var role string
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(role, '') FROM employees WHERE id = $1`, employeeID).Scan(&role); err != nil {
		return false
	}
	switch role {
	case "admin", "manager", "operating_manager":
		return true
	default:
		return false
	}
}

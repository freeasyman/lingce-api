package dealexpert

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConsultantSMSNotifier 在分析成功后，把“院内窗口 + 有可执行动作”的卡片以短信触达咨询师。
// 测试实现只落库、不真正发短信；生产实现替换为阿里云短信通道。
type ConsultantSMSNotifier interface {
	NotifyConsultantCard(ctx context.Context, tenantID, recordingID int64, card map[string]any) error
}

// TestConsultantSMSNotifier 测试通道：写 deal_sms_outbox，status=test_sent，便于联调与演示。
type TestConsultantSMSNotifier struct {
	pool       *pgxpool.Pool
	log        *slog.Logger
	webBaseURL string
}

func NewTestConsultantSMSNotifier(pool *pgxpool.Pool, log *slog.Logger, webBaseURL string) *TestConsultantSMSNotifier {
	return &TestConsultantSMSNotifier{pool: pool, log: log, webBaseURL: strings.TrimRight(strings.TrimSpace(webBaseURL), "/")}
}

func (n *TestConsultantSMSNotifier) ensureSchema(ctx context.Context) error {
	_, err := n.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_sms_outbox (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			employee_phone TEXT NOT NULL DEFAULT '',
			customer_name TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'test_sent',
			reason TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_deal_sms_outbox_recording
			ON deal_sms_outbox (tenant_id, recording_id, id DESC);
	`)
	return err
}

// inHospitalWindow 院内判定：录音刚结束（30 分钟内）且尚未产生任何结果事件。
func (n *TestConsultantSMSNotifier) inHospitalWindow(ctx context.Context, tenantID, recordingID int64) (bool, string) {
	var recordedAt *time.Time
	var encounterID int64
	if err := n.pool.QueryRow(ctx, `
		SELECT recorded_at, COALESCE(encounter_id, 0)
		FROM recordings WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, recordingID).Scan(&recordedAt, &encounterID); err != nil {
		return false, "recording not found"
	}
	if recordedAt == nil {
		return false, "recorded_at is null"
	}
	if time.Since(*recordedAt) > 30*time.Minute {
		return false, "recording older than 30 minutes"
	}
	var count int
	if err := n.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM deal_outcome_events
		WHERE tenant_id = $1 AND voided_at IS NULL AND (encounter_id = $2 OR recording_id = $3)
	`, tenantID, encounterID, recordingID).Scan(&count); err != nil {
		return false, "outcome check failed"
	}
	if count > 0 {
		return false, "outcome events already exist"
	}
	return true, ""
}

func (n *TestConsultantSMSNotifier) NotifyConsultantCard(ctx context.Context, tenantID, recordingID int64, card map[string]any) error {
	if n == nil || n.pool == nil {
		return nil
	}
	if err := n.ensureSchema(ctx); err != nil {
		return err
	}
	actionText := firstTextInCard(card, "primary_action", "action")
	if strings.TrimSpace(actionText) == "" {
		return nil // 无可执行动作：按规则不发短信
	}
	inHospital, reason := n.inHospitalWindow(ctx, tenantID, recordingID)
	if !inHospital {
		_, _ = n.pool.Exec(ctx, `INSERT INTO deal_sms_outbox (tenant_id, recording_id, status, reason) VALUES ($1,$2,'skipped',$3)`, tenantID, recordingID, reason)
		return nil
	}
	var employeeID int64
	var customerName string
	if err := n.pool.QueryRow(ctx, `
		SELECT COALESCE(employee_id, 0), COALESCE(customer_name, '')
		FROM recordings WHERE tenant_id = $1 AND id = $2
	`, tenantID, recordingID).Scan(&employeeID, &customerName); err != nil {
		return err
	}
	var phone, fullName string
	if employeeID > 0 {
		_ = n.pool.QueryRow(ctx, `SELECT COALESCE(phone,''), COALESCE(full_name, name, '') FROM employees WHERE tenant_id=$1 AND id=$2`, tenantID, employeeID).Scan(&phone, &fullName)
	}
	customer := strings.TrimSpace(customerName)
	if customer == "" {
		customer = "客户"
	}
	message := fmt.Sprintf("【灵策智能】%s的接待有需要现在处理的事项，请点短链查看。", customer)
	url := fmt.Sprintf("%s/m/recordings/%d", n.webBaseURL, recordingID)
	_, err := n.pool.Exec(ctx, `
		INSERT INTO deal_sms_outbox (tenant_id, recording_id, employee_phone, customer_name, message, url, status)
		VALUES ($1,$2,$3,$4,$5,$6,'test_sent')
	`, tenantID, recordingID, phone, customer, message, url)
	if err != nil {
		return err
	}
	if n.log != nil {
		n.log.Info("deal consultant sms test sent", "recording_id", recordingID, "phone", phone, "actor", fullName, "url", url)
	}
	_ = json.Valid([]byte("{}"))
	return nil
}

func firstTextInCard(card map[string]any, path ...string) string {
	var current any = card
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	text, _ := current.(string)
	return text
}

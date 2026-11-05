package dealexpert

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// InstantCard 即时卡：接待结束 1–2 分钟内的轻量三字段。
type InstantCard struct {
	ID            int64      `json:"id"`
	TenantID      int64      `json:"tenant_id"`
	RecordingID   int64      `json:"recording_id"`
	Status        string     `json:"status"`
	DecisionState string     `json:"decision_state"`
	MainBlocker   string     `json:"main_blocker"`
	OneAction     string     `json:"one_action"`
	EvidenceQuote string     `json:"evidence_quote"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
}

const instantSystemPrompt = `你是赢客即时分析员。只输出 JSON 对象，字段固定为：
{"decision_state":"还没选择我们/已经选择我们/已不选择/无法判断 四选一","main_blocker":"客户最卡的一点，用客户原话概括，不超过40字","one_action":"咨询师现在可以补的一个动作，不超过40字","evidence_quote":"支撑 main_blocker 的客户原话，不超过60字"}
禁止输出 JSON 以外的任何内容。`

var jsonFence = regexp.MustCompile(`(?s)\{.*\}`)

// InstantAnalyzer 轻量即时分析：只出三字段，控制时延。
type InstantAnalyzer struct {
	pool *pgxpool.Pool
	llm  *llmgateway.Client
	log  *slog.Logger
}

func NewInstantAnalyzer(pool *pgxpool.Pool, llm *llmgateway.Client, log *slog.Logger) *InstantAnalyzer {
	return &InstantAnalyzer{pool: pool, llm: llm, log: log}
}

func (a *InstantAnalyzer) ensureSchema(ctx context.Context) error {
	_, err := a.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_instant_cards (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			status TEXT NOT NULL DEFAULT 'analyzing',
			decision_state TEXT NOT NULL DEFAULT '',
			main_blocker TEXT NOT NULL DEFAULT '',
			one_action TEXT NOT NULL DEFAULT '',
			evidence_quote TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (tenant_id, recording_id)
		);
	`)
	return err
}

// RunInstant 执行轻量即时分析并落库；失败写入 error_message，不影响主链路。
func (a *InstantAnalyzer) RunInstant(ctx context.Context, tenantID, recordingID int64) {
	if a == nil || a.llm == nil || a.pool == nil {
		return
	}
	if err := a.ensureSchema(ctx); err != nil {
		return
	}
	var transcript string
	_ = a.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(cleaned_transcription::text, 'null'), transcription_text, '')
		FROM recordings WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, recordingID).Scan(&transcript)
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		a.save(ctx, tenantID, recordingID, "failed", "", "", "", "", "transcript empty")
		return
	}
	runes := []rune(transcript)
	if len(runes) > 1800 {
		transcript = string(runes[:1800])
	}
	start := time.Now()
	resp, err := a.llm.TextInference(ctx, llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  "deal-expert.instant",
		TraceID:       fmt.Sprintf("deal-instant-%d", recordingID),
		FunctionType:  AnalysisFunctionType,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "deal_expert",
			BusinessObjectType: "recording",
			BusinessObjectID:   recordingID,
			BillingSubject:     "deal_expert_analysis",
			BillingScene:       "instant",
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: instantSystemPrompt},
			{Role: "user", Content: "转写文本：\n" + transcript},
		},
		Params: &llmgateway.Params{Temperature: 0.1, MaxTokens: 256, TimeoutSeconds: 100, ResponseFormat: "json"},
	})
	if err != nil {
		a.save(ctx, tenantID, recordingID, "failed", "", "", "", "", fmt.Sprintf("llm: %v", err))
		return
	}
	raw := strings.TrimSpace(resp.Content)
	match := jsonFence.FindString(raw)
	var parsed struct {
		DecisionState string `json:"decision_state"`
		MainBlocker   string `json:"main_blocker"`
		OneAction     string `json:"one_action"`
		EvidenceQuote string `json:"evidence_quote"`
	}
	if json.Unmarshal([]byte(match), &parsed) != nil || strings.TrimSpace(parsed.DecisionState) == "" {
		a.save(ctx, tenantID, recordingID, "failed", "", "", "", "", "invalid json output")
		return
	}
	a.save(ctx, tenantID, recordingID, "analyzed", parsed.DecisionState, parsed.MainBlocker, parsed.OneAction, parsed.EvidenceQuote, "")
	if a.log != nil {
		a.log.Info("deal instant card saved", "recording_id", recordingID, "duration_ms", time.Since(start).Milliseconds())
	}
}

func (a *InstantAnalyzer) save(ctx context.Context, tenantID, recordingID int64, status, decision, blocker, action, quote, errMsg string) {
	_, _ = a.pool.Exec(ctx, `
		INSERT INTO deal_instant_cards (tenant_id, recording_id, status, decision_state, main_blocker, one_action, evidence_quote, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (tenant_id, recording_id)
		DO UPDATE SET status = EXCLUDED.status, decision_state = EXCLUDED.decision_state,
			main_blocker = EXCLUDED.main_blocker, one_action = EXCLUDED.one_action,
			evidence_quote = EXCLUDED.evidence_quote, error_message = EXCLUDED.error_message,
			updated_at = NOW()
	`, tenantID, recordingID, status, decision, blocker, action, quote, errMsg)
}

// LatestInstant 供前端手机卡读取即时结果。
func (a *InstantAnalyzer) LatestInstant(ctx context.Context, tenantID, recordingID int64) (*InstantCard, error) {
	if a == nil || a.pool == nil {
		return nil, fmt.Errorf("instant analyzer unavailable")
	}
	if err := a.ensureSchema(ctx); err != nil {
		return nil, err
	}
	var item InstantCard
	err := a.pool.QueryRow(ctx, `
		SELECT id, tenant_id, recording_id, status, decision_state, main_blocker, one_action, evidence_quote, created_at
		FROM deal_instant_cards WHERE tenant_id = $1 AND recording_id = $2
	`, tenantID, recordingID).Scan(&item.ID, &item.TenantID, &item.RecordingID, &item.Status,
		&item.DecisionState, &item.MainBlocker, &item.OneAction, &item.EvidenceQuote, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

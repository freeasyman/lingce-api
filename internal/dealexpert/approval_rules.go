package dealexpert

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ApprovalRules 经理/院长可配置的批复规则。
type ApprovalRules struct {
	PriceDiscountLimit int64  `json:"price_discount_limit"` // 批价上限（元）
	EscalationMinutes  int    `json:"escalation_minutes"`   // 未处理升级分钟数
	EscalationChain    string `json:"escalation_chain"`     // 升级链，逗号分隔
	P0Keywords         string `json:"p0_keywords"`          // P0 触发词，逗号分隔
}

func (s *Store) EnsureApprovalRulesTable(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_approval_rules (
			tenant_id BIGINT PRIMARY KEY,
			rules JSONB NOT NULL DEFAULT '{}'::jsonb,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	return err
}

func (s *Store) GetApprovalRules(ctx context.Context, tenantID int64) (map[string]any, error) {
	if err := s.EnsureApprovalRulesTable(ctx); err != nil {
		return nil, err
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(rules, '{}'::jsonb) FROM deal_approval_rules WHERE tenant_id = $1`, tenantID).Scan(&raw)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func (s *Store) SaveApprovalRules(ctx context.Context, tenantID int64, rules map[string]any) error {
	if err := s.EnsureApprovalRulesTable(ctx); err != nil {
		return err
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO deal_approval_rules (tenant_id, rules, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET rules = EXCLUDED.rules, updated_at = NOW()
	`, tenantID, raw)
	return err
}

func (h *Handler) GetApprovalRules(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	rules, err := h.service.GetApprovalRules(r.Context(), scope.TenantID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"rules": rules})
}

func (h *Handler) SaveApprovalRules(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	var body struct {
		Rules map[string]any `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Rules == nil {
		httputil.WriteBadRequest(w, "rules is required")
		return
	}
	if err := h.service.SaveApprovalRules(r.Context(), scope.TenantID, body.Rules); err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"saved": true})
}

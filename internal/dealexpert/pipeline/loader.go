package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// LoadModelConfig resolves the same tenant/default precedence as the existing
// product, but freezes all effective parameters and the selected row identity.
// It has no model-name fallback: a missing/unsafe configuration stops the run.
func (s *AuditStore) LoadModelConfig(ctx context.Context, tenantID int64) (ModelConfig, error) {
	if s == nil || s.db == nil || tenantID <= 0 {
		return ModelConfig{}, fmt.Errorf("audit store and positive tenant required")
	}
	var id, scope int64
	var function, provider, model string
	var modelRaw, extraRaw []byte
	err := s.db.QueryRow(ctx, `
 SELECT id,tenant_id,function_type,COALESCE(provider,''),COALESCE(model_code,''),
        COALESCE(model_params,'{}'::json)::text,COALESCE(extra_params,'{}'::json)::text
 FROM llm_model_configs
 WHERE deleted_at IS NULL AND COALESCE(is_active,true)=true
   AND function_type='deal_expert_analysis' AND tenant_id IN ($1,0)
 ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END,
          CASE WHEN COALESCE(is_default,false) THEN 0 ELSE 1 END,updated_at DESC,id DESC
 LIMIT 1`, tenantID).Scan(&id, &scope, &function, &provider, &model, &modelRaw, &extraRaw)
	if err != nil {
		return ModelConfig{}, fmt.Errorf("load effective model config: %w", err)
	}
	cfg, err := resolveModelConfig(function, provider, model, modelRaw, extraRaw)
	if err != nil {
		return ModelConfig{}, err
	}
	if id <= 0 || scope < 0 || scope != 0 && scope != tenantID {
		return ModelConfig{}, fmt.Errorf("invalid model config scope")
	}
	cfg.SourceConfigID = id
	cfg.SourceTenantID = &scope
	// The raw config projection is also bound, so edits to values that happen to
	// resolve to the same defaults remain distinguishable in the audit manifest.
	raw, _ := json.Marshal(struct {
		ID, Scope                 int64
		Function, Provider, Model string
		Params, Extra             []byte
	}{id, scope, function, provider, model, modelRaw, extraRaw})
	cfg.SourceHash = hashBytes(raw)
	return cfg, nil
}

func resolveModelConfig(function, provider, model string, modelRaw, extraRaw []byte) (ModelConfig, error) {
	cfg := ModelConfig{FunctionType: function, Provider: provider, ModelCode: model, Params: llmgateway.Params{Temperature: .1, MaxTokens: 4096, TimeoutSeconds: 180, ResponseFormat: "json"}}
	if function != "deal_expert_analysis" || strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" {
		return ModelConfig{}, fmt.Errorf("incomplete effective model config")
	}
	merged := map[string]json.RawMessage{}
	// Explicit precedence: model_params override extra_params, never silently
	// discard non-empty extra parameters simply because model_params is {}.
	for _, raw := range [][]byte{extraRaw, modelRaw} {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
			return ModelConfig{}, fmt.Errorf("model parameters must be a JSON object")
		}
		for k, v := range obj {
			merged[k] = v
		}
	}
	for key, raw := range merged {
		var dest any
		switch key {
		case "temperature":
			dest = &cfg.Params.Temperature
		case "max_tokens":
			dest = &cfg.Params.MaxTokens
		case "timeout_seconds":
			dest = &cfg.Params.TimeoutSeconds
		case "response_format":
			dest = &cfg.Params.ResponseFormat
		default:
			return ModelConfig{}, fmt.Errorf("untracked model parameter %q", key)
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return ModelConfig{}, fmt.Errorf("null model parameter %q", key)
		}
		if err := json.Unmarshal(raw, dest); err != nil {
			return ModelConfig{}, fmt.Errorf("model parameter %s: %w", key, err)
		}
	}
	p := cfg.Params
	if p.Temperature < 0 || p.Temperature > 2 || p.MaxTokens <= 0 || p.TimeoutSeconds <= 0 || p.ResponseFormat != "json" {
		return ModelConfig{}, fmt.Errorf("unsafe effective model parameters")
	}
	return cfg, nil
}

// LoadRuntimeConfig accepts an explicit immutable bundle ID. Mutable legacy
// prompt rows are not runtime release pointers and are never substituted here.
func (s *AuditStore) LoadRuntimeConfig(ctx context.Context, tenantID int64, bundleID string) (Bundle, ModelConfig, error) {
	b, err := s.LoadBundle(ctx, tenantID, bundleID)
	if err != nil {
		return Bundle{}, ModelConfig{}, err
	}
	m, err := s.LoadModelConfig(ctx, tenantID)
	if err != nil {
		return Bundle{}, ModelConfig{}, err
	}
	return b, m, nil
}

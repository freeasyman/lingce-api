package pipeline

import (
	"strings"
	"testing"
)

func TestResolveModelParameters(t *testing.T) {
	cfg, err := resolveModelConfig("deal_expert_analysis", "dashscope", "configured-model", []byte(`{"temperature":0,"max_tokens":8192}`), []byte(`{"temperature":0.8,"timeout_seconds":200}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Params.Temperature != 0 || cfg.Params.MaxTokens != 8192 || cfg.Params.TimeoutSeconds != 200 || cfg.Params.ResponseFormat != "json" {
		t.Fatalf("wrong effective params: %+v", cfg.Params)
	}
	if cfg.ModelCode != "configured-model" {
		t.Fatal("hardcoded model replaced database selection")
	}
}

func TestResolveModelRejectsUnsafeOrUntrackedParameters(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `{"temperature":null}`, `{"temperature":3}`, `{"max_tokens":0}`, `{"max_tokens":1.5}`, `{"timeout_seconds":-1}`, `{"response_format":"text"}`, `{"enable_search":true}`, `{"output_schema":{}}`, `{"top_p":0.9}`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := resolveModelConfig("deal_expert_analysis", "dashscope", "configured-model", []byte(raw), []byte(`{}`)); err == nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
	if _, err := resolveModelConfig("deal_expert_analysis", "dashscope", "configured-model", []byte(`{}`), []byte(`{"top_p":0.9}`)); err == nil || !strings.Contains(err.Error(), "untracked") {
		t.Fatal("extra parameter discarded", err)
	}
}

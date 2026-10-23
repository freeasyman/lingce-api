package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/freeasyman/lingce-api/pkg/oss"
	"github.com/jackc/pgx/v5"
)

// Service 内容获客服务（新模型核心，旧内容中心代码已随 Phase 3 清理）。
type Service struct {
	store     *Store
	llmClient *llmgateway.Client
	ossClient *oss.Client
}

func NewService(store *Store, llmClient *llmgateway.Client, ossClient *oss.Client) *Service {
	return &Service{store: store, llmClient: llmClient, ossClient: ossClient}
}

type llmModelSelection struct {
	FunctionType string
	Provider     string
	ModelCode    string
	ModelParams  JSONObject
}

const aiBillingRuleVersionV1 = "v1"

func newBillingMetadata(domain, objectType string, objectID int64, subject, scene string) *llmgateway.BillingMetadata {
	return &llmgateway.BillingMetadata{
		BusinessDomain:     domain,
		BusinessObjectType: objectType,
		BusinessObjectID:   objectID,
		BillingSubject:     subject,
		BillingScene:       scene,
		BillingRuleVersion: aiBillingRuleVersionV1,
	}
}

func contentCallerModule(subject string) string {
	switch strings.TrimSpace(subject) {
	case "topic_generation":
		return "content.topic_generation"
	case "content_generation":
		return "content.content_generation"
	case "graphic_note_repair":
		return "content.graphic_note_repair"
	default:
		return "content"
	}
}

func (s *Service) resolveLLMModelConfig(ctx context.Context, tenantID int64, functionTypeCandidates []string) (*llmModelSelection, error) {
	if len(functionTypeCandidates) == 0 {
		return nil, fmt.Errorf("function type candidates are required")
	}
	for _, functionType := range functionTypeCandidates {
		functionType = strings.TrimSpace(functionType)
		if functionType == "" {
			continue
		}
		query := `
			SELECT
				COALESCE(function_type, ''),
				COALESCE(provider, ''),
				COALESCE(model_code, ''),
				COALESCE(model_params, extra_params, '{}'::json)
			FROM llm_model_configs
			WHERE deleted_at IS NULL
			  AND COALESCE(is_active, true) = true
			  AND function_type = $1
			  AND tenant_id IN ($2, 0)
			ORDER BY
			  CASE WHEN tenant_id = $2 THEN 0 ELSE 1 END,
			  CASE WHEN COALESCE(is_default, false) THEN 0 ELSE 1 END,
			  updated_at DESC,
			  id DESC
			LIMIT 1
		`

		selection := &llmModelSelection{}
		if err := s.store.pool.QueryRow(ctx, query, functionType, tenantID).Scan(
			&selection.FunctionType,
			&selection.Provider,
			&selection.ModelCode,
			&selection.ModelParams,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("query llm config failed for function_type=%s: %w", functionType, err)
		}
		if strings.TrimSpace(selection.ModelCode) == "" {
			continue
		}
		if strings.TrimSpace(selection.FunctionType) == "" {
			selection.FunctionType = functionType
		}
		return selection, nil
	}
	return nil, fmt.Errorf("no active llm config found for tenant=%d candidates=%v", tenantID, functionTypeCandidates)
}

func applyModelParamsToLLMRequest(req *llmgateway.TextInferenceRequest, modelParams JSONObject) {
	if req == nil || len(modelParams) == 0 {
		return
	}
	if req.Params == nil {
		req.Params = &llmgateway.Params{}
	}
	if value, ok := modelParams["temperature"]; ok {
		if v, ok := toFloat64(value); ok {
			req.Params.Temperature = v
		}
	}
	if value, ok := modelParams["max_tokens"]; ok {
		if v, ok := toInt(value); ok && v > 0 {
			req.Params.MaxTokens = v
		}
	}
	if value, ok := modelParams["timeout_seconds"]; ok {
		if v, ok := toInt(value); ok && v > 0 {
			req.Params.TimeoutSeconds = v
		}
	}
	if value, ok := modelParams["response_format"]; ok {
		if v, ok := value.(string); ok {
			v = strings.TrimSpace(v)
			if v != "" {
				req.Params.ResponseFormat = v
			}
		}
	}
}

func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func toInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case int32:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i), true
		}
		f, ferr := v.Float64()
		if ferr != nil {
			return 0, false
		}
		return int(f), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

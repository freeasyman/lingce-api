package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client represents an LLM gateway client
type Client struct {
	baseURL     string
	apiKey      string
	client      *http.Client
	batchClient *http.Client
}

// NewClient creates a new LLM gateway client
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			// 业务请求通过 Params.TimeoutSeconds 控制 Gateway 内部超时；
			// HTTP 客户端必须略大于当前最长业务超时，否则 API 会在
			// Gateway 返回前先中止请求。质检场景使用 300 秒。
			//
			// 署名：Codex
			// 时间：2026-09-14
			Timeout: 360 * time.Second,
		},
		batchClient: &http.Client{
			// Batch Chat 是同步排队接口，最长可能等待 1 小时。
			Timeout: 3900 * time.Second,
		},
	}
}

// TextInferenceRequest represents a text inference request
type TextInferenceRequest struct {
	TenantID      int64            `json:"tenant_id"`
	CallerService string           `json:"caller_service"`
	CallerModule  string           `json:"caller_module"`
	TraceID       string           `json:"trace_id"`
	FunctionType  string           `json:"function_type"`
	Provider      string           `json:"provider,omitempty"`
	ModelCode     string           `json:"model_code,omitempty"`
	Billing       *BillingMetadata `json:"billing,omitempty"`
	Messages      []Message        `json:"messages"`
	Params        *Params          `json:"params,omitempty"`
	Batch         bool             `json:"batch,omitempty"`
}

// ImageInferenceRequest is the business-facing contract for gateway-managed
// multimodal inference. Business modules must send image data here and never
// call a provider directly.
type ImageInferenceRequest struct {
	TenantID      int64            `json:"tenant_id"`
	CallerService string           `json:"caller_service"`
	CallerModule  string           `json:"caller_module"`
	TraceID       string           `json:"trace_id"`
	FunctionType  string           `json:"function_type"`
	Provider      string           `json:"provider,omitempty"`
	ModelCode     string           `json:"model_code,omitempty"`
	Billing       *BillingMetadata `json:"billing,omitempty"`
	Messages      []ImageMessage   `json:"messages"`
	Params        *Params          `json:"params,omitempty"`
}

type ImageMessage struct {
	Role    string             `json:"role"`
	Content []ImageContentPart `json:"content"`
}

type ImageContentPart struct {
	Image string `json:"image,omitempty"`
	Text  string `json:"text,omitempty"`
}

// BillingMetadata represents business attribution for AI calls.
type BillingMetadata struct {
	BusinessDomain     string `json:"business_domain,omitempty"`
	BusinessObjectType string `json:"business_object_type,omitempty"`
	BusinessObjectID   int64  `json:"business_object_id,omitempty"`

	BillingSubject     string `json:"billing_subject,omitempty"`
	BillingScene       string `json:"billing_scene,omitempty"`
	BillingRuleVersion string `json:"billing_rule_version,omitempty"`

	RecordingID       int64 `json:"recording_id,omitempty"`
	ContentID         int64 `json:"content_id,omitempty"`
	TopicID           int64 `json:"topic_id,omitempty"`
	CustomerID        int64 `json:"customer_id,omitempty"`
	AnalysisRunID     int64 `json:"analysis_run_id,omitempty"`
	AnalysisStepRunID int64 `json:"analysis_step_run_id,omitempty"`
	GenerationTaskID  int64 `json:"generation_task_id,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"` // system, user, assistant
	Content string `json:"content"`
}

// OutputSchema carries a provider JSON Schema for structured output.
// When set, the gateway emits response_format=json_schema so the model is
// forced to return the exact structure instead of drifting.
type OutputSchema struct {
	Name   string                 `json:"name"`
	Strict bool                   `json:"strict"`
	Schema map[string]interface{} `json:"schema"`
}

// Params represents optional parameters
type Params struct {
	Temperature    float64       `json:"temperature,omitempty"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	TimeoutSeconds int           `json:"timeout_seconds,omitempty"`
	ResponseFormat string        `json:"response_format,omitempty"` // text, json
	OutputSchema   *OutputSchema `json:"output_schema,omitempty"`
	// EnableSearch 开启 provider 的联网搜索（查实时公开信息）。
	EnableSearch *bool `json:"enable_search,omitempty"`
}

func (p Params) MarshalJSON() ([]byte, error) {
	payload := struct {
		Temperature    float64       `json:"temperature"`
		MaxTokens      int           `json:"max_tokens,omitempty"`
		TimeoutSeconds int           `json:"timeout_seconds,omitempty"`
		ResponseFormat string        `json:"response_format,omitempty"`
		OutputSchema   *OutputSchema `json:"output_schema,omitempty"`
		EnableSearch   *bool         `json:"enable_search,omitempty"`
	}{
		Temperature:    p.Temperature,
		MaxTokens:      p.MaxTokens,
		TimeoutSeconds: p.TimeoutSeconds,
		ResponseFormat: p.ResponseFormat,
		OutputSchema:   p.OutputSchema,
		EnableSearch:   p.EnableSearch,
	}
	return json.Marshal(payload)
}

// TextInferenceResponse represents a text inference response
type TextInferenceResponse struct {
	RequestID      string   `json:"request_id"`
	Content        string   `json:"content"`
	Usage          Usage    `json:"usage"`
	Cost           Cost     `json:"cost"`
	LatencyMS      int      `json:"latency_ms"`
	Provider       string   `json:"provider"`
	ModelCode      string   `json:"model_code"`
	PriceVersion   string   `json:"price_version,omitempty"`
	SearchQueries  []string `json:"search_queries,omitempty"`
	PricingMissing bool     `json:"pricing_missing,omitempty"`
}

// TextInferenceWithRaw 返回结构化响应及网关原始响应，供需要完整审计链路的业务使用。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-12
func (c *Client) TextInferenceWithRaw(ctx context.Context, req TextInferenceRequest) (*TextInferenceResponse, []byte, []byte, error) {
	if req.TraceID == "" {
		req.TraceID = uuid.New().String()
	}
	if req.CallerService == "" {
		req.CallerService = "lingce-api"
	}
	if req.FunctionType == "" {
		req.FunctionType = "chat"
	}

	requestBody, err := json.Marshal(req)
	if err != nil {
		return nil, requestBody, nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/inference/text", bytes.NewReader(requestBody))
	if err != nil {
		return nil, requestBody, nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	httpClient := c.client
	if req.Batch {
		httpClient = c.batchClient
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, requestBody, nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, requestBody, responseBody, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(responseBody, &errResp); err != nil {
			return nil, requestBody, responseBody, fmt.Errorf("LLM gateway error (status %d): %s", resp.StatusCode, string(responseBody))
		}
		return nil, requestBody, responseBody, fmt.Errorf("LLM gateway error: %s - %s", errResp.Error.Code, errResp.Error.Message)
	}

	var result TextInferenceResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, requestBody, responseBody, fmt.Errorf("failed to parse response: %w", err)
	}
	return &result, requestBody, responseBody, nil
}

// ImageInference sends provider-neutral image content through the private
// LLM gateway. The gateway owns provider credentials and model routing.
func (c *Client) ImageInference(ctx context.Context, req ImageInferenceRequest) (*TextInferenceResponse, error) {
	if req.TraceID == "" {
		req.TraceID = uuid.New().String()
	}
	if req.CallerService == "" {
		req.CallerService = "lingce-api"
	}
	if req.FunctionType == "" {
		req.FunctionType = "image_understanding"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal image request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/inference/image", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create image request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send image request: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM gateway image error (status %d): %s", resp.StatusCode, string(responseBody))
	}
	var result TextInferenceResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse image response: %w", err)
	}
	return &result, nil
}

// Usage represents token usage
type Usage struct {
	InputTokens              int    `json:"input_tokens"`
	OutputTokens             int    `json:"output_tokens"`
	TotalTokens              int    `json:"total_tokens"`
	CachedInputTokens        int    `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens,omitempty"`
	CacheType                string `json:"cache_type,omitempty"`
}

// Cost represents cost information
type Cost struct {
	InputCost         float64 `json:"input_cost,omitempty"`
	CachedInputCost   float64 `json:"cached_input_cost,omitempty"`
	CacheCreationCost float64 `json:"cache_creation_cost,omitempty"`
	OutputCost        float64 `json:"output_cost,omitempty"`
	TotalCost         float64 `json:"total_cost"`
	Currency          string  `json:"currency"`
}

// ImageGenerationRequest is the business-facing image generation request.
type ImageGenerationRequest struct {
	TenantID      int64            `json:"tenant_id"`
	CallerService string           `json:"caller_service"`
	CallerModule  string           `json:"caller_module"`
	TraceID       string           `json:"trace_id"`
	FunctionType  string           `json:"function_type"`
	Provider      string           `json:"provider,omitempty"`
	ModelCode     string           `json:"model_code,omitempty"`
	Prompt        string           `json:"prompt"`
	ImageURL      string           `json:"image_url,omitempty"`
	N             int              `json:"n,omitempty"`
	Size          string           `json:"size,omitempty"`
	Billing       *BillingMetadata `json:"billing,omitempty"`
}

// ImageGenerationUsage is the billed image count.
type ImageGenerationUsage struct {
	InputImageCount  int `json:"input_image_count"`
	OutputImageCount int `json:"output_image_count"`
	OutputWidth      int `json:"output_width,omitempty"`
	OutputHeight     int `json:"output_height,omitempty"`
}

// ImageGenerationResponse is the gateway image generation response.
type ImageGenerationResponse struct {
	RequestID      string               `json:"request_id"`
	Images         []string             `json:"images"`
	Usage          ImageGenerationUsage `json:"usage"`
	Cost           Cost                 `json:"cost"`
	LatencyMS      int                  `json:"latency_ms"`
	Provider       string               `json:"provider"`
	ModelCode      string               `json:"model_code"`
	PriceVersion   string               `json:"price_version,omitempty"`
	PricingMissing bool                 `json:"pricing_missing,omitempty"`
}

// GenerateImage calls the gateway image generation endpoint.
func (c *Client) GenerateImage(ctx context.Context, req ImageGenerationRequest) (*ImageGenerationResponse, error) {
	if req.TraceID == "" {
		req.TraceID = uuid.New().String()
	}
	if req.CallerService == "" {
		req.CallerService = "lingce-api"
	}
	if req.FunctionType == "" {
		req.FunctionType = "image_generation"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal image generation request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/inference/image/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway image generation error %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out ImageGenerationResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EmbeddingInferenceRequest is the business-facing embedding request.
type EmbeddingInferenceRequest struct {
	TenantID      int64            `json:"tenant_id"`
	CallerService string           `json:"caller_service"`
	CallerModule  string           `json:"caller_module"`
	TraceID       string           `json:"trace_id"`
	FunctionType  string           `json:"function_type"`
	Provider      string           `json:"provider,omitempty"`
	ModelCode     string           `json:"model_code,omitempty"`
	Input         []string         `json:"input"`
	Billing       *BillingMetadata `json:"billing,omitempty"`
}

// EmbeddingInferenceResponse is the gateway embedding response.
type EmbeddingInferenceResponse struct {
	RequestID      string      `json:"request_id"`
	Embeddings     [][]float32 `json:"embeddings"`
	Usage          Usage       `json:"usage"`
	Cost           Cost        `json:"cost"`
	LatencyMS      int         `json:"latency_ms"`
	Provider       string      `json:"provider"`
	ModelCode      string      `json:"model_code"`
	PriceVersion   string      `json:"price_version,omitempty"`
	PricingMissing bool        `json:"pricing_missing,omitempty"`
}

// CallEmbedding calls the gateway embedding endpoint.
func (c *Client) CallEmbedding(ctx context.Context, req EmbeddingInferenceRequest) (*EmbeddingInferenceResponse, error) {
	if req.TraceID == "" {
		req.TraceID = uuid.New().String()
	}
	if req.CallerService == "" {
		req.CallerService = "lingce-api"
	}
	if req.FunctionType == "" {
		req.FunctionType = "embedding"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/inference/embedding", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway embedding error %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out EmbeddingInferenceResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
	RequestID string `json:"request_id"`
}

// AuditRecordItem represents a gateway audit record in list response.
type AuditRecordItem struct {
	ID                 int64                  `json:"id"`
	RequestID          string                 `json:"request_id"`
	TraceID            string                 `json:"trace_id"`
	TenantID           int64                  `json:"tenant_id"`
	BusinessDomain     string                 `json:"business_domain,omitempty"`
	BusinessObjectType string                 `json:"business_object_type,omitempty"`
	BusinessObjectID   int64                  `json:"business_object_id,omitempty"`
	BillingSubject     string                 `json:"billing_subject,omitempty"`
	BillingScene       string                 `json:"billing_scene,omitempty"`
	BillingRuleVersion string                 `json:"billing_rule_version,omitempty"`
	RecordingID        int64                  `json:"recording_id,omitempty"`
	ContentID          int64                  `json:"content_id,omitempty"`
	TopicID            int64                  `json:"topic_id,omitempty"`
	CustomerID         int64                  `json:"customer_id,omitempty"`
	AnalysisRunID      int64                  `json:"analysis_run_id,omitempty"`
	AnalysisStepRunID  int64                  `json:"analysis_step_run_id,omitempty"`
	GenerationTaskID   int64                  `json:"generation_task_id,omitempty"`
	FunctionType       string                 `json:"function_type"`
	Module             string                 `json:"module"`
	Provider           string                 `json:"provider"`
	ModelCode          string                 `json:"model_code"`
	Success            bool                   `json:"success"`
	InputTokens        int64                  `json:"input_tokens"`
	OutputTokens       int64                  `json:"output_tokens"`
	TotalTokens        int64                  `json:"total_tokens"`
	InputCost          float64                `json:"input_cost"`
	OutputCost         float64                `json:"output_cost"`
	TotalCost          float64                `json:"total_cost"`
	UsageMetadata      map[string]interface{} `json:"usage_metadata,omitempty"`
	LatencyMS          int64                  `json:"latency_ms"`
	CreatedAt          string                 `json:"created_at"`
}

// AuditRecordDetail represents a gateway audit record detail.
type AuditRecordDetail struct {
	AuditRecordItem
	TraceID          string                 `json:"trace_id"`
	AccountID        int64                  `json:"account_id,omitempty"`
	ProductCode      string                 `json:"product_code,omitempty"`
	ActionCode       string                 `json:"action_code,omitempty"`
	SpanID           string                 `json:"span_id,omitempty"`
	ParentSpanID     string                 `json:"parent_span_id,omitempty"`
	AttemptCount     int                    `json:"attempt_count,omitempty"`
	ErrorStage       string                 `json:"error_stage,omitempty"`
	TTFTMs           int                    `json:"ttft_ms,omitempty"`
	Attempts         []AuditAttempt         `json:"attempts,omitempty"`
	CallerService    string                 `json:"caller_service"`
	CallerModule     string                 `json:"caller_module"`
	ErrorCode        string                 `json:"error_code"`
	ErrorMessage     string                 `json:"error_message"`
	UsageMetadata    map[string]interface{} `json:"usage_metadata"`
	RequestMetadata  map[string]interface{} `json:"request_metadata"`
	RequestPreview   string                 `json:"request_preview,omitempty"`
	ResponsePreview  string                 `json:"response_preview,omitempty"`
	PayloadExpiresAt string                 `json:"payload_expires_at,omitempty"`
}

// AuditAttempt is one retry/fallback attempt.
type AuditAttempt struct {
	AttemptNo    int    `json:"attempt_no"`
	Provider     string `json:"provider"`
	ModelCode    string `json:"model_code"`
	Success      bool   `json:"success"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	LatencyMs    int    `json:"latency_ms"`
}

// AuditRecordListResponse represents audit record list response.
type AuditRecordListResponse struct {
	Items []AuditRecordItem `json:"items"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"size"`
	Pages int               `json:"pages"`
}

// AuditRecordStatsResponse represents audit statistics response.
type AuditRecordStatsResponse struct {
	TotalCalls        int64   `json:"total_calls"`
	TotalTokens       int64   `json:"total_tokens"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalAudioSeconds float64 `json:"total_audio_seconds"`
	TotalCost         float64 `json:"total_cost"`
	AvgLatencyMS      float64 `json:"avg_latency_ms"`
	SuccessRate       float64 `json:"success_rate"`
	SuccessCount      int64   `json:"success_count"`
	FailureCount      int64   `json:"failure_count"`
}

// AuditOptionItem is one distinct filter value with its call count.
type AuditOptionItem struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// AuditBreakdownItem is one grouped audit row.
type AuditBreakdownItem struct {
	Key          string  `json:"key"`
	Label        string  `json:"label,omitempty"`
	CallCount    int64   `json:"call_count"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCost    float64 `json:"total_cost"`
	FailureCount int64   `json:"failure_count"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
}

// AuditBreakdownResponse is a grouped audit aggregation.
type AuditBreakdownResponse struct {
	GroupBy string               `json:"group_by"`
	Items   []AuditBreakdownItem `json:"items"`
}

// AuditOptionsResponse contains distinct values used by the audit filter UI.
type AuditOptionsResponse struct {
	FunctionTypes   []AuditOptionItem   `json:"function_types"`
	Modules         []AuditOptionItem   `json:"modules"`
	Providers       []AuditOptionItem   `json:"providers"`
	ModelCodes      []AuditOptionItem   `json:"model_codes"`
	BusinessDomains []AuditOptionItem   `json:"business_domains"`
	BillingSubjects []AuditOptionItem   `json:"billing_subjects"`
	BusinessPairs   []AuditBusinessPair `json:"business_pairs"`
}

// AuditBusinessPair is one product/action combination.
type AuditBusinessPair struct {
	BusinessDomain string `json:"business_domain"`
	BillingSubject string `json:"billing_subject"`
	Count          int64  `json:"count"`
}

// CostByTenantItem represents cost grouped by tenant.
type CostByTenantItem struct {
	TenantID    int64   `json:"tenant_id"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

// AuditCostByGroupItem represents grouped cost item.
type AuditCostByGroupItem struct {
	Key         string  `json:"key"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

// AuditCostSummaryResponse represents overall cost summary.
type AuditCostSummaryResponse struct {
	TotalCost          float64                `json:"total_cost"`
	TotalCalls         int64                  `json:"total_calls"`
	TotalTokens        int64                  `json:"total_tokens"`
	CostByTenant       []CostByTenantItem     `json:"cost_by_tenant"`
	CostByFunctionType []AuditCostByGroupItem `json:"cost_by_function_type"`
	CostByModule       []AuditCostByGroupItem `json:"cost_by_module"`
	CostByModel        []AuditCostByGroupItem `json:"cost_by_model"`
}

// AuditCostByTenantResponse represents cost summary for one tenant.
type AuditCostByTenantResponse struct {
	TenantID           int64                  `json:"tenant_id"`
	TotalCost          float64                `json:"total_cost"`
	TotalCalls         int64                  `json:"total_calls"`
	TotalTokens        int64                  `json:"total_tokens"`
	CostByFunctionType []AuditCostByGroupItem `json:"cost_by_function_type"`
	CostByModule       []AuditCostByGroupItem `json:"cost_by_module"`
	CostByModel        []AuditCostByGroupItem `json:"cost_by_model"`
	CostTrend          []AuditCostByGroupItem `json:"cost_trend"`
}

// TextInference performs text inference
func (c *Client) TextInference(ctx context.Context, req TextInferenceRequest) (*TextInferenceResponse, error) {
	// Generate trace ID if not provided
	if req.TraceID == "" {
		req.TraceID = uuid.New().String()
	}

	// Set defaults
	if req.CallerService == "" {
		req.CallerService = "lingce-api"
	}
	if req.FunctionType == "" {
		req.FunctionType = "chat"
	}

	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/inference/text", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	// Send request
	httpClient := c.client
	if req.Batch {
		httpClient = c.batchClient
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return nil, fmt.Errorf("LLM gateway error (status %d): %s", resp.StatusCode, string(respBody))
		}
		return nil, fmt.Errorf("LLM gateway error: %s - %s", errResp.Error.Code, errResp.Error.Message)
	}

	// Parse response
	var result TextInferenceResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out interface{}) error {
	urlStr := c.baseURL + path
	if len(query) > 0 {
		urlStr += "?" + query.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return fmt.Errorf("LLM gateway error (status %d): %s", resp.StatusCode, string(respBody))
		}
		return fmt.Errorf("LLM gateway error: %s - %s", errResp.Error.Code, errResp.Error.Message)
	}

	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	return nil
}

// ListAuditRecords retrieves audit records from gateway.
func (c *Client) ListAuditRecords(ctx context.Context, params map[string]string) (*AuditRecordListResponse, error) {
	query := make(url.Values)
	for k, v := range params {
		if v != "" {
			query.Set(k, v)
		}
	}
	var out AuditRecordListResponse
	if err := c.get(ctx, "/v1/audit/records", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAuditOptions retrieves distinct filter values from gateway.
func (c *Client) GetAuditOptions(ctx context.Context) (*AuditOptionsResponse, error) {
	var out AuditOptionsResponse
	if err := c.get(ctx, "/v1/audit/records/options", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAuditBreakdown retrieves grouped audit statistics.
func (c *Client) GetAuditBreakdown(ctx context.Context, params map[string]string) (*AuditBreakdownResponse, error) {
	query := make(url.Values)
	for k, v := range params {
		if v != "" {
			query.Set(k, v)
		}
	}
	var out AuditBreakdownResponse
	if err := c.get(ctx, "/v1/audit/records/breakdown", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAuditRecordByRequestID retrieves audit record detail.
func (c *Client) GetAuditRecordByRequestID(ctx context.Context, requestID string) (*AuditRecordDetail, error) {
	var out AuditRecordDetail
	if err := c.get(ctx, "/v1/audit/records/"+url.PathEscape(requestID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAuditRecordStats retrieves audit statistics.
func (c *Client) GetAuditRecordStats(ctx context.Context, params map[string]string) (*AuditRecordStatsResponse, error) {
	query := make(url.Values)
	for k, v := range params {
		if v != "" {
			query.Set(k, v)
		}
	}
	var out AuditRecordStatsResponse
	if err := c.get(ctx, "/v1/audit/records/stats", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAuditCostSummary retrieves overall cost summary.
func (c *Client) GetAuditCostSummary(ctx context.Context, startDate, endDate string) (*AuditCostSummaryResponse, error) {
	query := make(url.Values)
	if startDate != "" {
		query.Set("start_date", startDate)
	}
	if endDate != "" {
		query.Set("end_date", endDate)
	}
	var out AuditCostSummaryResponse
	if err := c.get(ctx, "/v1/audit/cost/summary", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAuditCostByTenant retrieves tenant cost summary.
func (c *Client) GetAuditCostByTenant(ctx context.Context, tenantID int64, startDate, endDate string) (*AuditCostByTenantResponse, error) {
	query := make(url.Values)
	if startDate != "" {
		query.Set("start_date", startDate)
	}
	if endDate != "" {
		query.Set("end_date", endDate)
	}
	path := "/v1/audit/cost/by-tenant/" + strconv.FormatInt(tenantID, 10)
	var out AuditCostByTenantResponse
	if err := c.get(ctx, path, query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchRequest 网关搜索请求（走 /v1/inference/search，统一计量）。
type SearchRequest struct {
	Query         string `json:"query"`
	Count         int    `json:"count"`
	TenantID      int    `json:"tenant_id"`
	TraceID       string `json:"trace_id"`
	CallerService string `json:"caller_service"`
	CallerModule  string `json:"caller_module"`
	FunctionType  string `json:"function_type"`
}

// Search 通过网关执行搜索（DeepSeek 原生搜索），成本由网关统一审计。
func (c *Client) Search(ctx context.Context, req SearchRequest) ([]map[string]any, error) {
	if req.TraceID == "" {
		req.TraceID = uuid.New().String()
	}
	if req.CallerService == "" {
		req.CallerService = "lingce-api"
	}
	if req.FunctionType == "" {
		req.FunctionType = "search"
	}
	if req.Count <= 0 || req.Count > 20 {
		req.Count = 8
	}
	requestBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/inference/search", bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search api status %d: %s", resp.StatusCode, string(raw[:min(len(raw), 300)]))
	}
	var out struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return out.Results, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
	"time"

	"github.com/google/uuid"
)

// Client represents an LLM gateway client
type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewClient creates a new LLM gateway client
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 180 * time.Second,
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

// Params represents optional parameters
type Params struct {
	Temperature    float64 `json:"temperature,omitempty"`
	MaxTokens      int     `json:"max_tokens,omitempty"`
	TimeoutSeconds int     `json:"timeout_seconds,omitempty"`
	ResponseFormat string  `json:"response_format,omitempty"` // text, json
}

// TextInferenceResponse represents a text inference response
type TextInferenceResponse struct {
	RequestID string `json:"request_id"`
	Content   string `json:"content"`
	Usage     Usage  `json:"usage"`
	Cost      Cost   `json:"cost"`
	LatencyMS int    `json:"latency_ms"`
	Provider  string `json:"provider"`
	ModelCode string `json:"model_code"`
}

// Usage represents token usage
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Cost represents cost information
type Cost struct {
	InputCost  float64 `json:"input_cost"`
	OutputCost float64 `json:"output_cost"`
	TotalCost  float64 `json:"total_cost"`
	Currency   string  `json:"currency"`
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
	TraceID         string                 `json:"trace_id"`
	CallerService   string                 `json:"caller_service"`
	CallerModule    string                 `json:"caller_module"`
	ErrorCode       string                 `json:"error_code"`
	ErrorMessage    string                 `json:"error_message"`
	UsageMetadata   map[string]interface{} `json:"usage_metadata"`
	RequestMetadata map[string]interface{} `json:"request_metadata"`
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
	TotalCost         float64 `json:"total_cost"`
	AvgLatencyMS      float64 `json:"avg_latency_ms"`
	SuccessRate       float64 `json:"success_rate"`
	SuccessCount      int64   `json:"success_count"`
	FailureCount      int64   `json:"failure_count"`
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
	resp, err := c.client.Do(httpReq)
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

package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
			Timeout: 60 * time.Second,
		},
	}
}

// TextInferenceRequest represents a text inference request
type TextInferenceRequest struct {
	TenantID      int64     `json:"tenant_id"`
	CallerService string    `json:"caller_service"`
	CallerModule  string    `json:"caller_module"`
	TraceID       string    `json:"trace_id"`
	FunctionType  string    `json:"function_type"`
	Provider      string    `json:"provider,omitempty"`
	ModelCode     string    `json:"model_code,omitempty"`
	Messages      []Message `json:"messages"`
	Params        *Params   `json:"params,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`    // system, user, assistant
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
	RequestID  string  `json:"request_id"`
	Content    string  `json:"content"`
	Usage      Usage   `json:"usage"`
	Cost       Cost    `json:"cost"`
	LatencyMS  int     `json:"latency_ms"`
	Provider   string  `json:"provider"`
	ModelCode  string  `json:"model_code"`
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

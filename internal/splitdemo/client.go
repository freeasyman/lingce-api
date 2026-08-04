package splitdemo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	endpoint := baseURL
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint = endpoint + "/chat/completions"
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	}
	return &Client{
		endpoint: endpoint,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

func (c *Client) ChatCompletion(ctx context.Context, req llmChatRequest) (string, Usage, error) {
	if c.apiKey == "" {
		return "", Usage{}, fmt.Errorf("dashscope api key is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", Usage{}, fmt.Errorf("marshal llm request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", Usage{}, fmt.Errorf("build llm request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", Usage{}, fmt.Errorf("call dashscope: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", Usage{}, fmt.Errorf("read dashscope response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", Usage{}, fmt.Errorf("dashscope http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed llmChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", Usage{}, fmt.Errorf("decode dashscope response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", Usage{}, fmt.Errorf("dashscope response missing choices")
	}
	return parsed.Choices[0].Message.Content, Usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		TotalTokens:      parsed.Usage.TotalTokens,
	}, nil
}

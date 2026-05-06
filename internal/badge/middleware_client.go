package badge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	recordingActionStart = "start"
	recordingActionStop  = "stop"
)

type MiddlewareClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type startRecordingPayload struct {
	OrderNo string `json:"order_no,omitempty"`
}

type middlewareErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
	Code    string `json:"code"`
}

func NewMiddlewareClient(baseURL, token string) *MiddlewareClient {
	return &MiddlewareClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *MiddlewareClient) ControlRecording(ctx context.Context, action, deviceNo string, operatorID *int64, extraData JSONObject) error {
	_ = operatorID

	if action != recordingActionStart && action != recordingActionStop {
		return fmt.Errorf("unsupported recording action: %s", action)
	}
	if c.baseURL == "" {
		return fmt.Errorf("badge-middleware URL is not configured")
	}

	vendorCode := resolveVendorCode(extraData)
	endpoint := fmt.Sprintf("%s/v1/devices/%s/recording/%s?vendor_code=%s",
		c.baseURL,
		url.PathEscape(deviceNo),
		action,
		url.QueryEscape(vendorCode),
	)

	bodyBytes := []byte("{}")
	if action == recordingActionStart {
		payload := startRecordingPayload{
			OrderNo: resolveOrderNo(extraData),
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal badge-middleware request: %w", err)
		}
		bodyBytes = body
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create badge-middleware request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Internal-Token", c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call badge-middleware: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read badge-middleware response: %w", err)
	}

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	var errResp middlewareErrorResponse
	if len(respBody) > 0 && json.Unmarshal(respBody, &errResp) == nil {
		message := firstNonEmpty(errResp.Message, errResp.Error, errResp.Detail, errResp.Code)
		if message != "" {
			return fmt.Errorf("badge-middleware returned status %d: %s", resp.StatusCode, message)
		}
	}

	raw := strings.TrimSpace(string(respBody))
	if len(raw) > 256 {
		raw = raw[:256]
	}
	if raw == "" {
		raw = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("badge-middleware returned status %d: %s", resp.StatusCode, raw)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func resolveVendorCode(extraData JSONObject) string {
	if extraData != nil {
		if raw, ok := extraData["vendor_code"]; ok {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return "dudutalk"
}

func resolveOrderNo(extraData JSONObject) string {
	if extraData != nil {
		if raw, ok := extraData["order_no"]; ok {
			if s, ok := raw.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

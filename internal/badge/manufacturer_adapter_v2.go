package badge

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
)

const (
	adapterRequestTimeout = 10 * time.Second
	adapterRetryMax       = 3
)

type middlewareManufacturerAdapter struct {
	vendorCode string
	client     *MiddlewareClient
}

func newManufacturerAdapter(vendorCode string, client *MiddlewareClient) ManufacturerAdapter {
	vendorCode = strings.TrimSpace(vendorCode)
	if vendorCode == "" {
		vendorCode = "dudutalk"
	}
	return &middlewareManufacturerAdapter{
		vendorCode: vendorCode,
		client:     client,
	}
}

func (a *middlewareManufacturerAdapter) CheckDeviceExists(ctx context.Context, deviceNo string) (bool, error) {
	path := fmt.Sprintf("/v1/vendors/%s/devices/%s", url.PathEscape(a.vendorCode), url.PathEscape(deviceNo))
	status, _, err := a.requestJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		if status == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return status >= http.StatusOK && status < http.StatusMultipleChoices, nil
}

func (a *middlewareManufacturerAdapter) CheckOnline(ctx context.Context, deviceNo string) (bool, *time.Time, error) {
	path := fmt.Sprintf("/v1/vendors/%s/devices/%s/status", url.PathEscape(a.vendorCode), url.PathEscape(deviceNo))
	status, data, err := a.requestJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		if status == http.StatusNotFound {
			return false, nil, nil
		}
		return false, nil, err
	}

	online := false
	if v, ok := readBoolField(data, "online", "is_online"); ok {
		online = v
	} else if v, ok := readStringField(data, "status", "online_status"); ok {
		online = strings.EqualFold(v, "online")
	}
	lastOnline := readTimeField(data, "last_online_at", "online_at", "updated_at")
	return online, lastOnline, nil
}

func (a *middlewareManufacturerAdapter) GetBatteryLevel(ctx context.Context, deviceNo string) (*int, error) {
	path := fmt.Sprintf("/v1/vendors/%s/devices/%s/battery", url.PathEscape(a.vendorCode), url.PathEscape(deviceNo))
	status, data, err := a.requestJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		if status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}

	if level, ok := readIntField(data, "battery_level", "battery", "power"); ok {
		return &level, nil
	}
	return nil, nil
}

func (a *middlewareManufacturerAdapter) StartRecording(ctx context.Context, deviceNo string) error {
	return a.client.ControlRecording(ctx, recordingActionStart, deviceNo, nil, JSONObject{
		"vendor_code": a.vendorCode,
	})
}

func (a *middlewareManufacturerAdapter) StopRecording(ctx context.Context, deviceNo string) error {
	return a.client.ControlRecording(ctx, recordingActionStop, deviceNo, nil, JSONObject{
		"vendor_code": a.vendorCode,
	})
}

func (a *middlewareManufacturerAdapter) SyncDevices(ctx context.Context) ([]string, error) {
	path := fmt.Sprintf("/v1/vendors/%s/devices", url.PathEscape(a.vendorCode))
	_, data, err := a.requestJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return readDeviceNoList(data), nil
}

func (a *middlewareManufacturerAdapter) requestJSON(ctx context.Context, method, path string, body interface{}) (int, map[string]interface{}, error) {
	if a.client == nil || strings.TrimSpace(a.client.baseURL) == "" {
		return 0, nil, fmt.Errorf("badge-middleware URL is not configured")
	}
	fullURL := a.client.baseURL + path

	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		payload = b
	}

	var lastErr error
	var lastCode int
	for attempt := 1; attempt <= adapterRetryMax; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, adapterRequestTimeout)
		req, err := http.NewRequestWithContext(attemptCtx, method, fullURL, bytes.NewReader(payload))
		if err != nil {
			cancel()
			return 0, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if a.client.token != "" {
			req.Header.Set("X-Internal-Token", a.client.token)
		}

		resp, err := a.client.httpClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			if attempt < adapterRetryMax {
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return 0, nil, fmt.Errorf("request failed after retry: %w", lastErr)
		}

		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		lastCode = resp.StatusCode
		if readErr != nil {
			lastErr = readErr
			if attempt < adapterRetryMax {
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return lastCode, nil, fmt.Errorf("failed to read response: %w", readErr)
		}

		if resp.StatusCode >= 500 && attempt < adapterRetryMax {
			lastErr = fmt.Errorf("server status %d", resp.StatusCode)
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(raw))
			if len(msg) > 256 {
				msg = msg[:256]
			}
			if msg == "" {
				msg = http.StatusText(resp.StatusCode)
			}
			return resp.StatusCode, nil, fmt.Errorf("status %d: %s", resp.StatusCode, msg)
		}

		if len(raw) == 0 {
			return resp.StatusCode, map[string]interface{}{}, nil
		}
		data := map[string]interface{}{}
		if err := json.Unmarshal(raw, &data); err != nil {
			return resp.StatusCode, map[string]interface{}{}, nil
		}
		if v, ok := data["data"].(map[string]interface{}); ok {
			return resp.StatusCode, v, nil
		}
		return resp.StatusCode, data, nil
	}

	return lastCode, nil, fmt.Errorf("request failed: %w", lastErr)
}

func readBoolField(data map[string]interface{}, keys ...string) (bool, bool) {
	for _, key := range keys {
		v, ok := data[key]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case bool:
			return t, true
		case string:
			if strings.EqualFold(t, "true") {
				return true, true
			}
			if strings.EqualFold(t, "false") {
				return false, true
			}
		}
	}
	return false, false
}

func readStringField(data map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		if v, ok := data[key].(string); ok {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

func readIntField(data map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		v, ok := data[key]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case float64:
			return int(t), true
		case int:
			return t, true
		case int64:
			return int(t), true
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(t))
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func readTimeField(data map[string]interface{}, keys ...string) *time.Time {
	for _, key := range keys {
		v, ok := data[key]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case string:
			if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(t)); err == nil {
				return &parsed
			}
			if parsed, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(t)); err == nil {
				return &parsed
			}
		case float64:
			sec := int64(t)
			ts := time.Unix(sec, 0)
			return &ts
		}
	}
	return nil
}

func readDeviceNoList(data map[string]interface{}) []string {
	list := make([]string, 0)
	candidates := []string{"items", "devices", "list"}
	for _, key := range candidates {
		raw, ok := data[key]
		if !ok {
			continue
		}
		arr, ok := raw.([]interface{})
		if !ok {
			continue
		}
		for _, item := range arr {
			switch v := item.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					list = append(list, strings.TrimSpace(v))
				}
			case map[string]interface{}:
				if no, ok := readStringField(v, "device_no", "deviceNo", "sn"); ok && no != "" {
					list = append(list, no)
				}
			}
		}
		if len(list) > 0 {
			return list
		}
	}
	return list
}

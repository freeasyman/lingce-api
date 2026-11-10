package dealexpush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	// ExpoPushEndpoint 是 Expo Push 的官方发送地址；不需要密钥，access token 可选。
	ExpoPushEndpoint  = "https://exp.host/--/api/v2/push/send"
	expoPushChunkSize = 100
)

// ExpoMessage 是发往单台设备的一条 Expo Push。
type ExpoMessage struct {
	To    string         `json:"to"`
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Data  map[string]any `json:"data,omitempty"`
	Sound string         `json:"sound,omitempty"`
}

// ExpoTicket 是 Expo 针对单条消息返回的回执。
type ExpoTicket struct {
	Status    string
	ID        string
	Message   string
	ErrorCode string
}

// ExpoSender 抽象 Expo Push 发送，便于单测注入 fake。
type ExpoSender interface {
	Send(ctx context.Context, messages []ExpoMessage) ([]ExpoTicket, error)
}

// ExpoClient 是 Expo Push HTTP 客户端。
type ExpoClient struct {
	endpoint    string
	accessToken string
	httpClient  *http.Client
	log         *slog.Logger
}

// NewExpoClient 创建 Expo Push 客户端；accessToken 为空时不发送 Authorization 头。
func NewExpoClient(accessToken string, log *slog.Logger) *ExpoClient {
	if log == nil {
		log = slog.Default()
	}
	return &ExpoClient{
		endpoint:    ExpoPushEndpoint,
		accessToken: strings.TrimSpace(accessToken),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		log:         log,
	}
}

type expoPushResponse struct {
	Data []expoPushTicket `json:"data"`
}

type expoPushTicket struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

// Send 调用 Expo Push API；按 100 条一批发送，返回与 messages 等长的回执。
// 网络/HTTP/解码错误会返回 error，由调用方把所有 token 记为失败。
func (c *ExpoClient) Send(ctx context.Context, messages []ExpoMessage) ([]ExpoTicket, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("expo push client is not configured")
	}
	endpoint := strings.TrimSpace(c.endpoint)
	if endpoint == "" {
		endpoint = ExpoPushEndpoint
	}

	tickets := make([]ExpoTicket, 0, len(messages))
	for start := 0; start < len(messages); start += expoPushChunkSize {
		end := start + expoPushChunkSize
		if end > len(messages) {
			end = len(messages)
		}
		chunk := messages[start:end]
		chunkTickets, err := c.sendChunk(ctx, endpoint, chunk)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, chunkTickets...)
	}
	return tickets, nil
}

func (c *ExpoClient) sendChunk(ctx context.Context, endpoint string, messages []ExpoMessage) ([]ExpoTicket, error) {
	payload, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("marshal expo push payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create expo push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call expo push api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("expo push api returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result expoPushResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode expo push response: %w", err)
	}

	tickets := make([]ExpoTicket, 0, len(messages))
	for index := range messages {
		if index >= len(result.Data) {
			tickets = append(tickets, ExpoTicket{Status: "error", Message: "expo push response missing ticket"})
			continue
		}
		item := result.Data[index]
		tickets = append(tickets, ExpoTicket{
			Status:    strings.TrimSpace(item.Status),
			ID:        strings.TrimSpace(item.ID),
			Message:   strings.TrimSpace(item.Message),
			ErrorCode: strings.TrimSpace(item.Details.Error),
		})
	}

	for _, ticket := range tickets {
		if !strings.EqualFold(ticket.Status, "ok") {
			c.log.Warn("expo push ticket failed",
				"status", ticket.Status, "error_code", ticket.ErrorCode, "message", ticket.Message)
		}
	}
	c.log.Info("sent expo push notifications", "count", len(messages))
	return tickets, nil
}

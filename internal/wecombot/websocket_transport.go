package wecombot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// WebSocketTransport is the provider adapter for the smart-bot long connection.
// Endpoint is configurable because WeCom may use different gateways by region
// or environment; production must set it from the official configuration.
type WebSocketTransport struct {
	Endpoint     string
	Dialer       *websocket.Dialer
	WriteTimeout time.Duration
	PingInterval time.Duration
	mu           sync.Mutex
	connections  map[string]*websocket.Conn
}

func NewWebSocketTransport(endpoint string) (*WebSocketTransport, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return nil, errors.New("valid ws/wss endpoint is required")
	}
	return &WebSocketTransport{Endpoint: endpoint, Dialer: websocket.DefaultDialer, WriteTimeout: 10 * time.Second, PingInterval: 25 * time.Second, connections: make(map[string]*websocket.Conn)}, nil
}

func (t *WebSocketTransport) Receive(ctx context.Context, credential BotCredential, onMessage func(context.Context, Message) error) error {
	conn, _, err := t.Dialer.DialContext(ctx, t.Endpoint, nil)
	if err != nil {
		return fmt.Errorf("dial wecom bot websocket: %w", err)
	}
	t.mu.Lock()
	t.connections[credential.BotID] = conn
	t.mu.Unlock()
	defer func() { conn.Close(); t.mu.Lock(); delete(t.connections, credential.BotID); t.mu.Unlock() }()
	sub, err := MarshalSubscribe(uuid.NewString(), credential)
	if err != nil {
		return err
	}
	if err := t.write(conn, sub); err != nil {
		return err
	}
	go t.pingLoop(ctx, conn)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read wecom bot websocket: %w", err)
		}
		cmd, err := DecodeCommand(data)
		if err != nil {
			return err
		}
		if cmd.Cmd != "aibot_msg_callback" {
			continue
		}
		var msg Message
		if err := json.Unmarshal(cmd.Body, &msg); err != nil {
			return fmt.Errorf("decode wecom bot callback: %w", err)
		}
		if err := onMessage(ctx, msg); err != nil && !errors.Is(err, ErrDuplicateMessage) {
			return err
		}
	}
}

func (t *WebSocketTransport) Send(ctx context.Context, credential BotCredential, message OutgoingMessage) error {
	t.mu.Lock()
	conn := t.connections[credential.BotID]
	t.mu.Unlock()
	if conn == nil {
		return errors.New("wecom bot websocket is not connected")
	}
	data, err := MarshalTextSend(uuid.NewString(), message)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return t.write(conn, data)
}

func (t *WebSocketTransport) write(conn *websocket.Conn, data []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(t.WriteTimeout)); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func (t *WebSocketTransport) pingLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(t.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(t.WriteTimeout))
		case <-ctx.Done():
			return
		}
	}
}

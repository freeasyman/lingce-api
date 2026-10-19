// Package wecombot contains the provider-neutral core for the WeCom smart bot
// long-connection integration. The wire protocol is intentionally isolated
// behind Transport so it can be changed when the official frame contract is
// finalized without changing tenant, routing, or reply logic.
package wecombot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrBotAlreadyConnected = errors.New("wecom bot is already connected")
var ErrDuplicateMessage = errors.New("duplicate wecom bot message")

type Message struct {
	TenantID   string
	BotID      string
	MessageID  string
	UserID     string
	ChatID     string
	ChatType   string // single or group
	Text       string
	ReceivedAt time.Time
}

type OutgoingMessage struct {
	TenantID string
	BotID    string
	ToUserID string
	ChatID   string
	Text     string
	ReplyTo  string
}

type BotCredential struct{ TenantID, BotID, Secret string }

// Transport owns the provider-specific connection. Receive must invoke onMessage
// until ctx is cancelled; Send writes on that same bot connection.
type Transport interface {
	Receive(ctx context.Context, credential BotCredential, onMessage func(context.Context, Message) error) error
	Send(ctx context.Context, credential BotCredential, message OutgoingMessage) error
}

type ReplyHandler func(context.Context, Message) (string, error)

type Service struct {
	transport   Transport
	reply       ReplyHandler
	mu          sync.Mutex
	connected   map[string]context.CancelFunc
	credentials map[string]BotCredential
	seen        map[string]time.Time
}

func NewService(transport Transport, reply ReplyHandler) (*Service, error) {
	if transport == nil {
		return nil, errors.New("wecombot transport is required")
	}
	if reply == nil {
		return nil, errors.New("wecombot reply handler is required")
	}
	return &Service{transport: transport, reply: reply, connected: make(map[string]context.CancelFunc), credentials: make(map[string]BotCredential), seen: make(map[string]time.Time)}, nil
}

func (s *Service) Connect(ctx context.Context, credential BotCredential) error {
	if credential.TenantID == "" || credential.BotID == "" || credential.Secret == "" {
		return errors.New("tenant_id, bot_id and secret are required")
	}
	s.mu.Lock()
	if _, ok := s.connected[credential.BotID]; ok {
		s.mu.Unlock()
		return ErrBotAlreadyConnected
	}
	connCtx, cancel := context.WithCancel(ctx)
	s.connected[credential.BotID] = cancel
	s.credentials[credential.BotID] = credential
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.connected, credential.BotID)
			delete(s.credentials, credential.BotID)
			s.mu.Unlock()
		}()
		_ = s.transport.Receive(connCtx, credential, s.handleMessage)
	}()
	return nil
}

func (s *Service) Disconnect(botID string) {
	s.mu.Lock()
	if cancel := s.connected[botID]; cancel != nil {
		cancel()
	}
	s.mu.Unlock()
}

func (s *Service) Push(ctx context.Context, credential BotCredential, message OutgoingMessage) error {
	if credential.TenantID == "" || credential.BotID == "" || credential.Secret == "" {
		return errors.New("tenant_id, bot_id and secret are required")
	}
	if message.Text == "" {
		return errors.New("message text is required")
	}
	return s.transport.Send(ctx, credential, message)
}

func (s *Service) handleMessage(ctx context.Context, message Message) error {
	if message.MessageID != "" {
		s.mu.Lock()
		if _, ok := s.seen[message.BotID+":"+message.MessageID]; ok {
			s.mu.Unlock()
			return ErrDuplicateMessage
		}
		s.seen[message.BotID+":"+message.MessageID] = time.Now()
		// Keep the in-memory POC bounded. Production should persist a dedupe key.
		for key, at := range s.seen {
			if time.Since(at) > 10*time.Minute {
				delete(s.seen, key)
			}
		}
		s.mu.Unlock()
	}
	text, err := s.reply(ctx, message)
	if err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	s.mu.Lock()
	credential := s.credentials[message.BotID]
	s.mu.Unlock()
	return s.transport.Send(ctx, credential, OutgoingMessage{ToUserID: message.UserID, ChatID: message.ChatID, Text: text, ReplyTo: message.MessageID})
}

// EchoReply is a deterministic handler useful for smoke tests and initial POC.
func EchoReply(_ context.Context, message Message) (string, error) {
	return fmt.Sprintf("已收到：%s", message.Text), nil
}

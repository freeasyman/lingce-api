package wecombot

import (
	"context"
	"testing"
	"time"
)

type memoryTransport struct {
	received chan Message
	sent     chan OutgoingMessage
}

func (m *memoryTransport) Receive(ctx context.Context, credential BotCredential, onMessage func(context.Context, Message) error) error {
	select {
	case msg := <-m.received:
		msg.TenantID, msg.BotID = credential.TenantID, credential.BotID
		return onMessage(ctx, msg)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *memoryTransport) Send(_ context.Context, _ BotCredential, msg OutgoingMessage) error {
	m.sent <- msg
	return nil
}

func TestServiceReceivesAndReplies(t *testing.T) {
	transport := &memoryTransport{received: make(chan Message, 1), sent: make(chan OutgoingMessage, 2)}
	service, err := NewService(transport, EchoReply)
	if err != nil {
		t.Fatal(err)
	}
	credential := BotCredential{TenantID: "tenant-1", BotID: "bot-1", Secret: "secret"}
	if err := service.Connect(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	transport.received <- Message{MessageID: "m1", UserID: "u1", ChatID: "c1", Text: "你好"}
	select {
	case got := <-transport.sent:
		if got.Text != "已收到：你好" || got.ToUserID != "u1" || got.ChatID != "c1" || got.ReplyTo != "m1" {
			t.Fatalf("unexpected reply: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reply")
	}
}

func TestServiceRejectsSecondConnection(t *testing.T) {
	transport := &memoryTransport{received: make(chan Message), sent: make(chan OutgoingMessage)}
	service, err := NewService(transport, EchoReply)
	if err != nil {
		t.Fatal(err)
	}
	credential := BotCredential{TenantID: "tenant-1", BotID: "bot-1", Secret: "secret"}
	if err := service.Connect(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	if err := service.Connect(context.Background(), credential); err != ErrBotAlreadyConnected {
		t.Fatalf("got %v, want ErrBotAlreadyConnected", err)
	}
}

func TestServicePush(t *testing.T) {
	transport := &memoryTransport{received: make(chan Message), sent: make(chan OutgoingMessage, 1)}
	service, err := NewService(transport, EchoReply)
	if err != nil {
		t.Fatal(err)
	}
	credential := BotCredential{TenantID: "tenant-1", BotID: "bot-1", Secret: "secret"}
	if err := service.Push(context.Background(), credential, OutgoingMessage{ToUserID: "u1", Text: "待办提醒"}); err != nil {
		t.Fatal(err)
	}
	if got := <-transport.sent; got.Text != "待办提醒" || got.ToUserID != "u1" {
		t.Fatalf("unexpected push: %+v", got)
	}
}

func TestServiceDeduplicatesMessage(t *testing.T) {
	transport := &memoryTransport{received: make(chan Message, 2), sent: make(chan OutgoingMessage, 2)}
	service, err := NewService(transport, EchoReply)
	if err != nil {
		t.Fatal(err)
	}
	credential := BotCredential{TenantID: "tenant-1", BotID: "bot-1", Secret: "secret"}
	if err := service.Connect(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	transport.received <- Message{MessageID: "same", UserID: "u1", Text: "hello"}
	transport.received <- Message{MessageID: "same", UserID: "u1", Text: "hello"}
	select {
	case <-transport.sent:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first reply")
	}
	select {
	case <-transport.sent:
		t.Fatal("duplicate produced a second reply")
	case <-time.After(100 * time.Millisecond):
	}
}

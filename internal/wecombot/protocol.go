package wecombot

import (
	"encoding/json"
	"errors"
	"fmt"
)

// WireCommand is the JSON envelope used by the smart-bot long connection.
// Keep this type small: provider additions belong in Body, while routing stays
// in Service.
type WireCommand struct {
	Cmd     string          `json:"cmd"`
	Headers WireHeaders     `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
}

type WireHeaders struct {
	ReqID string `json:"req_id"`
}

type SubscribeBody struct {
	BotID  string `json:"bot_id"`
	Secret string `json:"secret"`
}

type SendTextBody struct {
	ChatID  string `json:"chatid,omitempty"`
	UserID  string `json:"userid,omitempty"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
}

func MarshalSubscribe(reqID string, credential BotCredential) ([]byte, error) {
	if credential.BotID == "" || credential.Secret == "" {
		return nil, errors.New("bot_id and secret are required")
	}
	return json.Marshal(WireCommand{Cmd: "aibot_subscribe", Headers: WireHeaders{ReqID: reqID}, Body: mustJSON(SubscribeBody{BotID: credential.BotID, Secret: credential.Secret})})
}

func MarshalTextSend(reqID string, message OutgoingMessage) ([]byte, error) {
	if message.ChatID == "" && message.ToUserID == "" {
		return nil, errors.New("chat_id or user_id is required")
	}
	if message.Text == "" {
		return nil, errors.New("message text is required")
	}
	body := SendTextBody{ChatID: message.ChatID, UserID: message.ToUserID, MsgType: "text"}
	body.Text.Content = message.Text
	return json.Marshal(WireCommand{Cmd: "aibot_send_msg", Headers: WireHeaders{ReqID: reqID}, Body: mustJSON(body)})
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func DecodeCommand(data []byte) (WireCommand, error) {
	var cmd WireCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return cmd, fmt.Errorf("decode wecom bot frame: %w", err)
	}
	if cmd.Cmd == "" {
		return cmd, errors.New("wecom bot frame cmd is required")
	}
	return cmd, nil
}

package wecombot

import (
	"encoding/json"
	"testing"
)

func TestMarshalSubscribe(t *testing.T) {
	b, err := MarshalSubscribe("r1", BotCredential{BotID: "b1", Secret: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["cmd"] != "aibot_subscribe" {
		t.Fatalf("unexpected command: %v", got["cmd"])
	}
}

func TestMarshalTextSendTargetsUser(t *testing.T) {
	b, err := MarshalTextSend("r1", OutgoingMessage{ToUserID: "u1", Text: "提醒"})
	if err != nil {
		t.Fatal(err)
	}
	var got WireCommand
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	var body SendTextBody
	if err := json.Unmarshal(got.Body, &body); err != nil {
		t.Fatal(err)
	}
	if got.Cmd != "aibot_send_msg" || body.UserID != "u1" || body.Text.Content != "提醒" {
		t.Fatalf("unexpected frame: %+v %+v", got, body)
	}
}

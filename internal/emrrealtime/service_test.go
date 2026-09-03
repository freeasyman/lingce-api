package emrrealtime

import (
	"encoding/binary"
	"testing"
)

func TestGatewayWebSocketURL(t *testing.T) {
	cases := map[string]string{
		"http://gateway.example":  "ws://gateway.example",
		"https://gateway.example": "wss://gateway.example",
		"ws://gateway.example":    "ws://gateway.example",
	}
	for input, want := range cases {
		if got := gatewayWebSocketURL(input); got != want {
			t.Errorf("gatewayWebSocketURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSourceIDIsStableAndPositive(t *testing.T) {
	first := sourceID("request-1")
	if first <= 0 {
		t.Fatalf("sourceID must be positive, got %d", first)
	}
	if got := sourceID("request-1"); got != first {
		t.Fatalf("sourceID must be stable, got %d want %d", got, first)
	}
}

func TestPCMIsWrappedAsWAVForSavedRecording(t *testing.T) {
	pcm := make([]byte, 320)
	wav := pcmToWAV(pcm, 16000, 1, 16)
	if !isRealtimePCM("audio/pcm", "realtime.pcm") {
		t.Fatal("expected PCM media type to be recognized")
	}
	if len(wav) != 364 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("unexpected WAV payload: len=%d header=%q", len(wav), wav[:12])
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != 320 {
		t.Fatalf("unexpected WAV data size: %d", got)
	}
}

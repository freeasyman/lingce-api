package badge

import "strings"

func isAudioCallback(payload CallbackPayload) bool {
	ev := strings.ToUpper(strings.TrimSpace(payload.EventType))
	if ev == "" {
		return false
	}
	if strings.Contains(ev, "AUDIO") {
		return true
	}
	return ev == "audio_event"
}

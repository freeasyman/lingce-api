package pipeline

import (
	"strings"
	"unicode"
)

// This is an explicit producer control token, not a heuristic interpretation of
// natural language. Every kept piece is an unmodified contiguous UTF-8 substring.
const truncationControl = "[转写过长，已截断]"

type sourcePiece struct {
	start, end          int
	disposition, reason string
}

func dialogueText(text string) bool {
	if strings.Contains(text, truncationControl) {
		return false
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

func splitSourceText(text string) ([]sourcePiece, bool) {
	pieces := []sourcePiece{}
	control := strings.Contains(text, truncationControl)
	appendDialogue := func(start, end int) {
		reason, disposition := "original_text", "kept"
		if control {
			reason = "control_separated_substring"
		}
		if !dialogueText(text[start:end]) {
			disposition = "dropped"
			reason = "noise"
			if strings.TrimSpace(text[start:end]) == "" {
				reason = "whitespace"
			}
		}
		if start != end || len(text) == 0 {
			pieces = append(pieces, sourcePiece{start, end, disposition, reason})
		}
	}
	start := 0
	for {
		index := strings.Index(text[start:], truncationControl)
		if index < 0 {
			appendDialogue(start, len(text))
			break
		}
		index += start
		appendDialogue(start, index)
		end := index + len(truncationControl)
		pieces = append(pieces, sourcePiece{index, end, "dropped", "truncation_control"})
		start = end
	}
	return pieces, control
}

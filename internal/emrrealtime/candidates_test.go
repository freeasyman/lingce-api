package emrrealtime

import (
	"context"
	"testing"
	"time"
)

func TestRealtimeTranscriptCollectorUsesOnlyFinalResultsAndDeduplicates(t *testing.T) {
	collector := newRealtimeTranscriptCollector()
	now := time.Now()
	collector.Add(realtimeASRResult{Sequence: 1, Text: "临时内容", Final: false}, now)
	collector.Add(realtimeASRResult{Sequence: 2, Text: "第二句", Final: true}, now)
	collector.Add(realtimeASRResult{Sequence: 1, Text: "第一句", Final: true}, now)
	collector.Add(realtimeASRResult{Sequence: 1, Text: "第一句重复", Final: true}, now)

	items, wait := collector.Acquire(true, now)
	if wait != 0 || len(items) != 2 {
		t.Fatalf("unexpected acquired items: len=%d wait=%s", len(items), wait)
	}
	if items[0].Sequence != 1 || items[1].Sequence != 2 || items[0].Text != "第一句" {
		t.Fatalf("unexpected ordered items: %+v", items)
	}
}

func TestRealtimeTranscriptCollectorWaitsForBatchThreshold(t *testing.T) {
	collector := newRealtimeTranscriptCollector()
	now := time.Now()
	collector.Add(realtimeASRResult{Sequence: 1, Text: "短句", Final: true}, now)
	if items, wait := collector.Acquire(false, now.Add(time.Second)); len(items) != 0 || wait <= 0 {
		t.Fatalf("expected delayed generation, items=%d wait=%s", len(items), wait)
	}
	if items, wait := collector.Acquire(false, now.Add(realtimeCandidateMaximumWait+time.Second)); len(items) != 1 || wait != 0 {
		t.Fatalf("expected timeout generation, items=%d wait=%s", len(items), wait)
	}
}

func TestParseCandidateJSONRejectsInvalidAndReadsCandidates(t *testing.T) {
	items, err := parseCandidateJSON("```json\n{\"candidates\":[{\"section_code\":\"chief_complaint\"}]}\n```")
	if err != nil || len(items) != 1 {
		t.Fatalf("parse valid candidate JSON: items=%d err=%v", len(items), err)
	}
	if _, err := parseCandidateJSON("not-json"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestRealtimeCandidateGeneratorFlushReturnsWhenIdle(t *testing.T) {
	generator := newRealtimeCandidateGenerator(nil, 1, 2, "record", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	generator.FlushContext(ctx)
}

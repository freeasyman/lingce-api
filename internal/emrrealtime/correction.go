package emrrealtime

import (
	"context"
	"sort"
	"strings"
	"sync"
)

type realtimeTranscriptCorrector struct {
	service     *Service
	tenantID    int64
	encounterID int64
	onSuccess   func(realtimeTranscriptCorrection)
	onError     func(realtimeASRResult, error)

	mu        sync.Mutex
	pending   map[int]realtimeASRResult
	processed map[int]bool
	running   bool
	idle      chan struct{}
	previous  string
}

func newRealtimeTranscriptCorrector(service *Service, tenantID, encounterID int64, onSuccess func(realtimeTranscriptCorrection), onError func(realtimeASRResult, error)) *realtimeTranscriptCorrector {
	return &realtimeTranscriptCorrector{
		service: service, tenantID: tenantID, encounterID: encounterID, onSuccess: onSuccess, onError: onError,
		pending: make(map[int]realtimeASRResult), processed: make(map[int]bool), idle: closedChannel(),
	}
}

func (c *realtimeTranscriptCorrector) Add(result realtimeASRResult) {
	if !result.Final || strings.TrimSpace(result.Text) == "" {
		return
	}
	c.mu.Lock()
	if !c.processed[result.Sequence] {
		if _, exists := c.pending[result.Sequence]; !exists {
			c.pending[result.Sequence] = result
		}
	}
	c.mu.Unlock()
	c.kick()
}

func (c *realtimeTranscriptCorrector) FlushContext(ctx context.Context) {
	for {
		c.kick()
		c.mu.Lock()
		running := c.running
		pending := len(c.pending) > 0
		idle := c.idle
		c.mu.Unlock()
		if !running && !pending {
			return
		}
		select {
		case <-idle:
		case <-ctx.Done():
			return
		}
	}
}

func (c *realtimeTranscriptCorrector) kick() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	item, ok := c.nextLocked()
	if !ok {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.idle = make(chan struct{})
	previous := c.previous
	c.mu.Unlock()

	go c.correct(item, previous)
}

func (c *realtimeTranscriptCorrector) nextLocked() (realtimeASRResult, bool) {
	if len(c.pending) == 0 {
		return realtimeASRResult{}, false
	}
	sequences := make([]int, 0, len(c.pending))
	for sequence := range c.pending {
		sequences = append(sequences, sequence)
	}
	sort.Ints(sequences)
	sequence := sequences[0]
	item := c.pending[sequence]
	delete(c.pending, sequence)
	return item, true
}

func (c *realtimeTranscriptCorrector) correct(item realtimeASRResult, previous string) {
	result, err := c.service.CorrectRealtimeTranscript(context.Background(), c.tenantID, c.encounterID, item, previous)

	c.mu.Lock()
	c.processed[item.Sequence] = true
	if err == nil {
		c.previous = result.CorrectedText
	}
	c.running = false
	idle := c.idle
	c.mu.Unlock()

	if err != nil {
		if c.onError != nil {
			c.onError(item, err)
		}
	} else if c.onSuccess != nil {
		c.onSuccess(*result)
	}
	close(idle)
	c.kick()
}

package emrrealtime

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrrecord"
)

const (
	realtimeCandidateMinimumCharacters = 300
	realtimeCandidateMaximumWait       = 30 * time.Second
	realtimeCandidateGenerationTimeout = 125 * time.Second
)

type realtimeTranscriptCollector struct {
	segments       map[int]realtimeTranscriptSegment
	processed      map[int]bool
	inFlight       map[int]bool
	firstPendingAt time.Time
}

func newRealtimeTranscriptCollector() *realtimeTranscriptCollector {
	return &realtimeTranscriptCollector{
		segments:  make(map[int]realtimeTranscriptSegment),
		processed: make(map[int]bool),
		inFlight:  make(map[int]bool),
	}
}

func (c *realtimeTranscriptCollector) Add(result realtimeASRResult, now time.Time) {
	if !result.Final || strings.TrimSpace(result.Text) == "" || c.processed[result.Sequence] || c.inFlight[result.Sequence] {
		return
	}
	if _, exists := c.segments[result.Sequence]; exists {
		return
	}
	c.segments[result.Sequence] = realtimeTranscriptSegment{
		Sequence: result.Sequence, Text: strings.TrimSpace(result.Text), StartTime: result.StartTime, EndTime: result.EndTime,
	}
	if c.firstPendingAt.IsZero() {
		c.firstPendingAt = now
	}
}

func (c *realtimeTranscriptCollector) Acquire(force bool, now time.Time) ([]realtimeTranscriptSegment, time.Duration) {
	items := c.pending()
	if len(items) == 0 {
		return nil, 0
	}
	if !force && characterCount(items) < realtimeCandidateMinimumCharacters {
		elapsed := now.Sub(c.firstPendingAt)
		if elapsed < realtimeCandidateMaximumWait {
			return nil, realtimeCandidateMaximumWait - elapsed
		}
	}
	for _, item := range items {
		c.inFlight[item.Sequence] = true
	}
	return items, 0
}

func (c *realtimeTranscriptCollector) Complete(items []realtimeTranscriptSegment, success bool, now time.Time) {
	for _, item := range items {
		delete(c.inFlight, item.Sequence)
		if success {
			c.processed[item.Sequence] = true
		}
	}
	if success {
		if len(c.pending()) == 0 {
			c.firstPendingAt = time.Time{}
		}
		return
	}
	c.firstPendingAt = now
}

func (c *realtimeTranscriptCollector) pending() []realtimeTranscriptSegment {
	items := make([]realtimeTranscriptSegment, 0, len(c.segments))
	for sequence, item := range c.segments {
		if !c.processed[sequence] && !c.inFlight[sequence] {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Sequence < items[j].Sequence })
	return items
}

func characterCount(items []realtimeTranscriptSegment) int {
	count := 0
	for _, item := range items {
		count += len([]rune(item.Text))
	}
	return count
}

type realtimeCandidateGenerator struct {
	service     *Service
	tenantID    int64
	encounterID int64
	recordID    string
	onSuccess   func([]*emrrecord.AICandidate)
	onError     func(error)

	mu        sync.Mutex
	collector *realtimeTranscriptCollector
	running   bool
	timer     *time.Timer
	idle      chan struct{}
	failed    bool
}

func newRealtimeCandidateGenerator(service *Service, tenantID, encounterID int64, recordID string, onSuccess func([]*emrrecord.AICandidate), onError func(error)) *realtimeCandidateGenerator {
	return &realtimeCandidateGenerator{
		service: service, tenantID: tenantID, encounterID: encounterID, recordID: recordID,
		onSuccess: onSuccess, onError: onError, collector: newRealtimeTranscriptCollector(), idle: closedChannel(),
	}
}

func (g *realtimeCandidateGenerator) Add(result realtimeASRResult) {
	g.mu.Lock()
	g.collector.Add(result, time.Now())
	g.failed = false
	g.mu.Unlock()
	g.kick(false)
}

func (g *realtimeCandidateGenerator) Flush() {
	g.FlushContext(context.Background())
}

func (g *realtimeCandidateGenerator) FlushContext(ctx context.Context) {
	for {
		g.kick(true)
		g.mu.Lock()
		idle := g.idle
		running := g.running
		pending := len(g.collector.pending()) > 0
		failed := g.failed
		g.mu.Unlock()
		if !running {
			if !pending || failed {
				return
			}
			continue
		}
		select {
		case <-idle:
		case <-ctx.Done():
			return
		}
	}
}

func (g *realtimeCandidateGenerator) kick(force bool) {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return
	}
	items, wait := g.collector.Acquire(force, time.Now())
	if len(items) == 0 {
		g.scheduleLocked(wait)
		g.mu.Unlock()
		return
	}
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	g.running = true
	g.idle = make(chan struct{})
	g.mu.Unlock()

	go g.generate(items)
}

func (g *realtimeCandidateGenerator) scheduleLocked(wait time.Duration) {
	if wait <= 0 || g.timer != nil {
		return
	}
	g.timer = time.AfterFunc(wait, func() {
		g.mu.Lock()
		g.timer = nil
		g.mu.Unlock()
		g.kick(false)
	})
}

func (g *realtimeCandidateGenerator) generate(items []realtimeTranscriptSegment) {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.Text)
	}
	ctx, cancel := context.WithTimeout(context.Background(), realtimeCandidateGenerationTimeout)
	defer cancel()
	candidates, err := g.service.GenerateRealtimeAICandidates(ctx, g.tenantID, g.encounterID, g.recordID, strings.Join(parts, "\n"), items)

	g.mu.Lock()
	g.collector.Complete(items, err == nil, time.Now())
	g.running = false
	g.failed = err != nil
	idle := g.idle
	g.mu.Unlock()

	if err != nil {
		if g.onError != nil {
			g.onError(err)
		}
		close(idle)
		return
	}
	if len(candidates) > 0 && g.onSuccess != nil {
		g.onSuccess(candidates)
	}
	close(idle)
	g.kick(false)
}

func closedChannel() chan struct{} {
	channel := make(chan struct{})
	close(channel)
	return channel
}

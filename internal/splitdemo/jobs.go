package splitdemo

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type JobManager struct {
	mu      sync.RWMutex
	counter uint64
	jobs    map[string]*SplitJob
}

func NewJobManager() *JobManager {
	return &JobManager{
		jobs: make(map[string]*SplitJob),
	}
}

func (m *JobManager) Create(req SplitRequest) *SplitJob {
	now := time.Now().Format(time.RFC3339)
	id := fmt.Sprintf("split_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&m.counter, 1))
	job := &SplitJob{
		ID:            id,
		RecordingID:   req.RecordingID,
		Model:         req.Model,
		PromptVersion: req.PromptVersion,
		Status:        SplitJobPending,
		Stage:         "pending",
		Message:       "任务已创建，等待执行",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()
	return cloneJob(job)
}

func (m *JobManager) Get(id string) (*SplitJob, bool) {
	m.mu.RLock()
	job, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return cloneJob(job), true
}

func (m *JobManager) Update(id string, fn func(job *SplitJob)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return
	}
	fn(job)
	job.UpdatedAt = time.Now().Format(time.RFC3339)
}

func cloneJob(job *SplitJob) *SplitJob {
	if job == nil {
		return nil
	}
	copyJob := *job
	if job.Result != nil {
		resultCopy := *job.Result
		if job.Result.Segments != nil {
			resultCopy.Segments = append([]SplitSegment(nil), job.Result.Segments...)
		}
		copyJob.Result = &resultCopy
	}
	if job.MatchReport != nil {
		matchCopy := *job.MatchReport
		if job.MatchReport.Materials != nil {
			matchCopy.Materials = append([]MaterialMatch(nil), job.MatchReport.Materials...)
		}
		if job.MatchReport.Encounters != nil {
			matchCopy.Encounters = append([]EncounterMatch(nil), job.MatchReport.Encounters...)
		}
		if job.MatchReport.Seams != nil {
			matchCopy.Seams = append([]SeamDetail(nil), job.MatchReport.Seams...)
		}
		copyJob.MatchReport = &matchCopy
	}
	return &copyJob
}

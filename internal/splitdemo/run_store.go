package splitdemo

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type RunStore struct {
	mu   sync.Mutex
	path string
}

func NewRunStore(path string) *RunStore {
	if path == "" {
		path = os.Getenv("SPLITDEMO_RUNS_FILE")
	}
	if path == "" {
		path = filepath.Join(os.TempDir(), "lingce-splitdemo-runs.jsonl")
	}
	return &RunStore{path: path}
}

func (s *RunStore) Save(ctx context.Context, record *SplitRunRecord) error {
	_ = ctx
	if record == nil || record.Result == nil {
		return fmt.Errorf("empty split run record")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	record.UpdatedAt = time.Now()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func (s *RunStore) LatestByRecording(ctx context.Context, recordingID int64) (*SplitRunRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)
	var latest *SplitRunRecord
	for scanner.Scan() {
		var record SplitRunRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if record.RecordingID != recordingID {
			continue
		}
		copyRecord := record
		latest = &copyRecord
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return latest, nil
}

func (s *RunStore) LatestByCase(ctx context.Context, caseID string) (*SplitRunRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)
	var latest *SplitRunRecord
	for scanner.Scan() {
		var record SplitRunRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if strings.TrimSpace(record.CaseID) != strings.TrimSpace(caseID) {
			continue
		}
		copyRecord := record
		latest = &copyRecord
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return latest, nil
}

func (s *RunStore) ListByCase(ctx context.Context, caseID string, limit int) ([]*SplitRunRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = 5
	}
	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)
	all := make([]*SplitRunRecord, 0, limit)
	for scanner.Scan() {
		var record SplitRunRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if strings.TrimSpace(record.CaseID) != strings.TrimSpace(caseID) {
			continue
		}
		copyRecord := record
		all = append(all, &copyRecord)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

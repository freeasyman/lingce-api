package splitdemo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type AnnotationStore struct {
	mu  sync.Mutex
	dir string
}

func NewAnnotationStore(dir string) *AnnotationStore {
	if dir == "" {
		dir = filepath.Join("data", "annotations")
	}
	return &AnnotationStore{dir: dir}
}

func (s *AnnotationStore) path(recordingID int64) string {
	return filepath.Join(s.dir, fmt.Sprintf("recording_%d.json", recordingID))
}

func (s *AnnotationStore) Save(ctx context.Context, record *AnnotationRecord) error {
	_ = ctx
	if record == nil {
		return fmt.Errorf("empty annotation record")
	}
	if record.RecordingID <= 0 {
		return fmt.Errorf("recording_id is required")
	}
	if record.AnnotatedAt == "" {
		record.AnnotatedAt = time.Now().Format(time.RFC3339)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(record.RecordingID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(record.RecordingID))
}

func (s *AnnotationStore) Load(ctx context.Context, recordingID int64) (*AnnotationRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path(recordingID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out AnnotationRecord
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

package splitdemo

import (
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

type SyntheticStore struct {
	mu  sync.Mutex
	dir string
}

func NewSyntheticStore(dir string) *SyntheticStore {
	if dir == "" {
		dir = filepath.Join("data", "synthetic")
	}
	return &SyntheticStore{dir: dir}
}

func (s *SyntheticStore) path(caseID string) string {
	return filepath.Join(s.dir, fmt.Sprintf("case_%s.json", sanitizeSyntheticCaseID(caseID)))
}

func (s *SyntheticStore) Save(ctx context.Context, record *SyntheticCase) error {
	_ = ctx
	if record == nil {
		return fmt.Errorf("empty synthetic case")
	}
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("case id is required")
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(record.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(record.ID))
}

func (s *SyntheticStore) Load(ctx context.Context, caseID string) (*SyntheticCase, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(caseID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out SyntheticCase
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *SyntheticStore) List(ctx context.Context) ([]*SyntheticCase, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	items := make([]*SyntheticCase, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var item SyntheticCase
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		copyItem := item
		items = append(items, &copyItem)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func (s *SyntheticStore) Delete(ctx context.Context, caseID string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(caseID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func sanitizeSyntheticCaseID(caseID string) string {
	caseID = strings.TrimSpace(strings.ToLower(caseID))
	if caseID == "" {
		return "unnamed"
	}
	var b strings.Builder
	for _, r := range caseID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "unnamed"
	}
	return out
}

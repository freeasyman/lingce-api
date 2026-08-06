package auth

import (
	"context"
	"sync"
	"time"
	"strings"
)

// CaptchaStore stores captcha codes in memory with expiration
type CaptchaStore struct {
	mu      sync.RWMutex
	codes   map[string]captchaEntry
	cleanup *time.Ticker
}

type captchaEntry struct {
	code      string
	expiresAt time.Time
}

// NewCaptchaStore creates a new captcha store
func NewCaptchaStore() *CaptchaStore {
	store := &CaptchaStore{
		codes:   make(map[string]captchaEntry),
		cleanup: time.NewTicker(5 * time.Minute),
	}

	// Start cleanup goroutine
	go store.cleanupExpired()

	return store
}

// Save stores a captcha code with expiration
func (s *CaptchaStore) Save(captchaID, code string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.codes[captchaID] = captchaEntry{
		code:      code,
		expiresAt: time.Now().Add(ttl),
	}
}

// Verify checks if the captcha code is valid and removes it
func (s *CaptchaStore) Verify(captchaID, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.codes[captchaID]
	if !exists {
		return false
	}

	// Remove the code (one-time use)
	delete(s.codes, captchaID)

	// Check expiration
	if time.Now().After(entry.expiresAt) {
		return false
	}

	// Case-insensitive comparison
	return strings.EqualFold(entry.code, code)
}

// cleanupExpired removes expired captcha codes
func (s *CaptchaStore) cleanupExpired() {
	for range s.cleanup.C {
		s.mu.Lock()
		now := time.Now()
		for id, entry := range s.codes {
			if now.After(entry.expiresAt) {
				delete(s.codes, id)
			}
		}
		s.mu.Unlock()
	}
}

// Close stops the cleanup ticker
func (s *CaptchaStore) Close() {
	s.cleanup.Stop()
}

// Dummy context usage to satisfy linter
var _ = context.Background()

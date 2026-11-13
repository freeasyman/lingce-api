package his

import (
	"testing"
	"time"
)

func TestImportFileCacheTenantIsolation(t *testing.T) {
	cache := newImportFileCache()
	token := cache.put(1, importTargetChargeItem, []string{"编码", "名称"}, []importRow{{Number: 2, Cells: []string{"C1", "项目"}}})
	if token == "" {
		t.Fatal("token should not be empty")
	}
	if _, ok := cache.get(token, 2); ok {
		t.Fatal("token must not be readable by another tenant")
	}
	entry, ok := cache.get(token, 1)
	if !ok {
		t.Fatal("token should be readable by owner tenant")
	}
	if entry.target != importTargetChargeItem || len(entry.rows) != 1 || entry.rows[0].Number != 2 {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := cache.get("not-a-token", 1); ok {
		t.Fatal("unknown token should not resolve")
	}
}

func TestImportFileCacheExpiry(t *testing.T) {
	cache := newImportFileCache()
	token := cache.put(1, importTargetMaterial, []string{"编码"}, nil)
	cache.mu.Lock()
	entry := cache.entries[token]
	entry.expires = time.Now().Add(-time.Minute)
	cache.entries[token] = entry
	cache.mu.Unlock()
	if _, ok := cache.get(token, 1); ok {
		t.Fatal("expired token should not resolve")
	}
}

func TestBuildHeadersHandlesEmptyAndDuplicate(t *testing.T) {
	headers := buildHeaders([]string{"名称", "", "名称"}, [][]string{{"a", "b", "c", "d"}})
	if len(headers) != 4 {
		t.Fatalf("headers width = %d, want 4", len(headers))
	}
	if headers[1] != "列2" || headers[2] != "名称(2)" || headers[3] != "列4" {
		t.Fatalf("headers = %v", headers)
	}
}

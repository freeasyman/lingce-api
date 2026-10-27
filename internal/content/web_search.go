package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// searchResult DeepSeek 原生搜索 API 的单条结果。
type searchResult struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Snippet   string `json:"snippet"`
	Summary   string `json:"summary"`
	SiteName  string `json:"site_name"`
	Date      string `json:"date"`
	Authority int    `json:"authority"`
}

// searchWeb 调 DeepSeek 原生搜索 API（/v1/tools/search），返回真实信源。
func (s *Service) searchWeb(ctx context.Context, query string, count int) ([]searchResult, error) {
	if s.searchAPIKey == "" {
		return nil, nil
	}
	if count <= 0 || count > 20 {
		count = 8
	}
	body, _ := json.Marshal(map[string]any{"query": query, "count": count, "need_content": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/v1/tools/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.searchAPIKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search api status %d", resp.StatusCode)
	}
	var out struct {
		Results []searchResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

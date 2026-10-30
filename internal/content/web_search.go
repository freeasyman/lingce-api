package content

import (
	"context"
	"fmt"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// searchResult 搜索单条结果（来自网关 /v1/inference/search）。
type searchResult struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Snippet   string `json:"snippet"`
	Summary   string `json:"summary"`
	SiteName  string `json:"site_name"`
	Date      string `json:"date"`
	Authority int    `json:"authority"`
}

// searchWeb 通过网关执行搜索（成本由网关统一审计计量）。
func (s *Service) searchWeb(ctx context.Context, tenantID int64, query string, count int) ([]searchResult, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	raw, err := s.llmClient.Search(ctx, llmgateway.SearchRequest{
		Query:         query,
		Count:         count,
		TenantID:      int(tenantID),
		CallerService: "lingce-api",
		CallerModule:  "ai_visibility_search",
		FunctionType:  "search",
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "content",
			BusinessObjectType: "ai_visibility_run",
			BillingSubject:     "ai_visibility_search",
			BillingScene:       "ai_visibility",
			CostLayer:          "product",
		},
	})
	if err != nil {
		return nil, err
	}
	out := make([]searchResult, 0, len(raw))
	for _, m := range raw {
		out = append(out, searchResult{
			Title:     strOf(m["title"]),
			URL:       strOf(m["url"]),
			Snippet:   strOf(m["snippet"]),
			Summary:   strOf(m["summary"]),
			SiteName:  strOf(m["site_name"]),
			Date:      strOf(m["date"]),
			Authority: intOf(m["authority"]),
		})
	}
	return out, nil
}

func strOf(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func intOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return 0
}

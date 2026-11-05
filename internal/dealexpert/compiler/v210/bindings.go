package v210

import (
	"fmt"
	"sort"
	"strings"
)

// Check every binding, including fields subsequently removed by business rules.
// Otherwise a null summary or an irrelevant condition could hide invalid refs.
func (c *compiler) checkAllReferences(v any, path string) {
	switch n := v.(type) {
	case object:
		keys := []string{}
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "source_refs" || key == "timing_source_refs" || key == "response_source_refs" || key == "customer_window_source_refs" {
				kind := "transcript"
				if path == "$.decision" || path == "$.conclusion" || path == "$.coach.refund_review" || strings.HasPrefix(path, "$.secondary_findings[") {
					kind = ""
				}
				c.refs(n, key, path, kind)
			} else {
				c.checkAllReferences(n[key], path+"."+key)
			}
		}
	case []any:
		for i, child := range n {
			c.checkAllReferences(child, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

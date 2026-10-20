package dealexpert

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed deal_card_v1.schema.json
var dealCardSchemaFS embed.FS

const dealCardSchemaID = "https://lingce.local/schema/deal-card-v1.json"

// loadDealCardSchema 优先使用提示词表里的 output_schema（允许租户覆盖），
// 没有则使用仓库内置的 deal-card-v1，保证分析器在提示词未写库前也能做结构校验。
func loadDealCardSchema(override string) (*jsonschema.Schema, error) {
	raw := strings.TrimSpace(override)
	if raw == "" {
		data, err := dealCardSchemaFS.ReadFile("deal_card_v1.schema.json")
		if err != nil {
			return nil, fmt.Errorf("read embedded deal card schema: %w", err)
		}
		raw = string(data)
	}
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("decode deal card schema: %w", err)
	}
	id := dealCardSchemaID
	if root, ok := doc.(map[string]any); ok {
		if value, ok := root["$id"].(string); ok && strings.TrimSpace(value) != "" {
			id = strings.TrimSpace(value)
		}
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(id, doc); err != nil {
		return nil, fmt.Errorf("add deal card schema: %w", err)
	}
	schema, err := compiler.Compile(id)
	if err != nil {
		return nil, fmt.Errorf("compile deal card schema: %w", err)
	}
	return schema, nil
}

type cardValidationContext struct {
	Transcript        string
	CatalogProductIDs map[int64]bool
	CatalogAvailable  bool
	OwnerEmployeeID   int64
}

// validateDealCard 先做 JSON Schema 严格校验，再做业务二次校验。
// 任何一步失败都不允许写库。
func validateDealCard(content string, schema *jsonschema.Schema, ctx cardValidationContext) (map[string]any, error) {
	cleaned := stripCodeFence(content)
	if strings.TrimSpace(cleaned) == "" {
		return nil, fmt.Errorf("model returned empty content")
	}
	var card map[string]any
	if err := json.Unmarshal([]byte(cleaned), &card); err != nil {
		return nil, fmt.Errorf("model output is not valid JSON: %w", err)
	}
	if err := schema.Validate(card); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}
	if err := validateBusinessRules(card, ctx); err != nil {
		return nil, err
	}
	return card, nil
}

func validateBusinessRules(card map[string]any, ctx cardValidationContext) error {
	items, err := evidenceItems(card)
	if err != nil {
		return err
	}
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		id, _ := item["id"].(string)
		if id != "" {
			ids[id] = true
		}
	}

	// 1) 引用的证据必须存在。
	for _, ref := range collectEvidenceRefs(card) {
		if !ids[ref] {
			return fmt.Errorf("business validation: unknown evidence ref %s", ref)
		}
	}

	// 2) 证据原话必须来自转写（系统事实除外）。
	normalizedTranscript := normalizeForQuoteMatch(ctx.Transcript)
	for _, item := range items {
		speaker, _ := item["speaker"].(string)
		if speaker == "系统事实" {
			continue
		}
		quote, _ := item["quote"].(string)
		if strings.TrimSpace(quote) == "" {
			continue
		}
		if !strings.Contains(normalizedTranscript, normalizeForQuoteMatch(quote)) {
			return fmt.Errorf("business validation: evidence quote not found in transcript: %q", truncateRunes(quote, 30))
		}
	}

	// 3) 至多一个主顾虑。
	decision, _ := card["decision"].(map[string]any)
	primaryCount := 0
	if concerns, ok := decision["concerns"].([]any); ok {
		for _, row := range concerns {
			concern, _ := row.(map[string]any)
			if primary, ok := concern["is_primary"].(bool); ok && primary {
				primaryCount++
			}
		}
	}
	if primaryCount > 1 {
		return fmt.Errorf("business validation: more than one primary concern (%d)", primaryCount)
	}

	// 4) 产品库不可用时禁止漏推/升级/替代；产品 ID 必须来自目录。
	findings, _ := card["product_findings"].([]any)
	for _, row := range findings {
		finding, _ := row.(map[string]any)
		findingType, _ := finding["finding_type"].(string)
		if !ctx.CatalogAvailable {
			switch findingType {
			case "可能漏推", "升级机会", "替代方案":
				return fmt.Errorf("business validation: %s is not allowed without product catalog", findingType)
			}
		}
		if id, ok := finding["product_id"].(float64); ok && id > 0 {
			if !ctx.CatalogProductIDs[int64(id)] {
				return fmt.Errorf("business validation: product_id %d not found in catalog", int64(id))
			}
		}
		if findingType == "联合机会" {
			if requires, ok := finding["requires_clinical_confirmation"].(bool); !ok || !requires {
				return fmt.Errorf("business validation: 联合机会 must require clinical confirmation")
			}
		}
	}

	// 5) 低置信不得生成动作。
	confidence, _ := card["confidence"].(string)
	if confidence == "low" && card["primary_action"] != nil {
		return fmt.Errorf("business validation: low confidence must not produce primary_action")
	}

	// 6) 负责人强制覆盖为原接待咨询师。
	if action, ok := card["primary_action"].(map[string]any); ok && ctx.OwnerEmployeeID > 0 {
		action["owner_employee_id"] = ctx.OwnerEmployeeID
	}
	return nil
}

func evidenceItems(card map[string]any) ([]map[string]any, error) {
	rows, ok := card["evidence_refs"].([]any)
	if !ok {
		return nil, fmt.Errorf("business validation: evidence_refs missing")
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item, ok := row.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("business validation: evidence item is not an object")
		}
		items = append(items, item)
	}
	return items, nil
}

// collectEvidenceRefs 收集所有 evidence_refs / source_evidence_refs 中引用的 id。
func collectEvidenceRefs(value any) []string {
	refs := make([]string, 0, 8)
	var walk func(node any, key string)
	walk = func(node any, key string) {
		switch typed := node.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		case []any:
			for _, child := range typed {
				if key == "evidence_refs" || key == "source_evidence_refs" {
					if text, ok := child.(string); ok {
						refs = append(refs, text)
					}
					continue
				}
				walk(child, key)
			}
		}
	}
	walk(value, "")
	return refs
}

// normalizeForQuoteMatch 去掉空白与标点后再比对，降低转写标点差异带来的误判。
func normalizeForQuoteMatch(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func stripCodeFence(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	trimmed = strings.TrimPrefix(trimmed, "```")
	if idx := strings.Index(trimmed, "\n"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	return strings.TrimSpace(trimmed)
}

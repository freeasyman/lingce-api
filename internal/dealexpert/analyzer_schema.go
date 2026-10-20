package dealexpert

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed deal_card_v1_1.schema.json
var dealCardSchemaFS embed.FS

const (
	dealCardSchemaID    = "https://lingce.local/schema/deal-card-v1.1.json"
	dealCardSchemaFile  = "deal_card_v1_1.schema.json"
	dealCardVersion     = "deal-card-v1.1"
	dealQualityStandard = "compass-sales-v1.1"
)

var evidenceIDPattern = regexp.MustCompile(`^ev_[0-9]+$`)

type evidenceSource struct {
	ID      string
	Text    string
	Speaker string
	StartMS *int
}

type businessFactRef struct {
	Type string
}

type catalogRef struct {
	Name    string
	Version string
}

type cardValidationContext struct {
	Sources       map[string]evidenceSource
	Facts         map[string]businessFactRef
	Catalog       map[int64]catalogRef
	AllowMissed   bool
	AllowExtended bool
}

// loadDealCardSchema 优先使用提示词表里的 output_schema，否则用内置的 deal-card-v1.1。
// 同时返回原始 Schema 文本，供调用模型时作为可见约束。
func loadDealCardSchema(override string) (*jsonschema.Schema, string, error) {
	raw := strings.TrimSpace(override)
	if raw == "" {
		data, err := dealCardSchemaFS.ReadFile(dealCardSchemaFile)
		if err != nil {
			return nil, "", fmt.Errorf("read embedded deal card schema: %w", err)
		}
		raw = string(data)
	}
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, "", fmt.Errorf("decode deal card schema: %w", err)
	}
	id := dealCardSchemaID
	if root, ok := doc.(map[string]any); ok {
		if value, ok := root["$id"].(string); ok && strings.TrimSpace(value) != "" {
			id = strings.TrimSpace(value)
		}
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(id, doc); err != nil {
		return nil, "", fmt.Errorf("add deal card schema: %w", err)
	}
	schema, err := compiler.Compile(id)
	if err != nil {
		return nil, "", fmt.Errorf("compile deal card schema: %w", err)
	}
	return schema, raw, nil
}

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
	if card["schema_version"] != dealCardVersion {
		return fmt.Errorf("business validation: schema_version must be %s", dealCardVersion)
	}
	quality, _ := card["quality"].(map[string]any)
	if quality["standard_version"] != dealQualityStandard {
		return fmt.Errorf("business validation: quality.standard_version must be %s", dealQualityStandard)
	}

	items, err := evidenceItems(card)
	if err != nil {
		return err
	}
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		id, _ := item["id"].(string)
		if id == "" {
			return fmt.Errorf("business validation: evidence id is empty")
		}
		if ids[id] {
			return fmt.Errorf("business validation: duplicate evidence id %s", id)
		}
		ids[id] = true
	}

	// 所有 ev_N 引用必须可解析（不限定字段名，避免漏检）。
	for _, ref := range collectEvidenceIDStrings(card) {
		if !ids[ref] {
			return fmt.Errorf("business validation: unknown evidence ref %s", ref)
		}
	}

	// 证据必须与声明的来源逐字一致。
	for _, item := range items {
		eid, _ := item["id"].(string)
		sourceRef, _ := item["source_ref"].(string)
		source, ok := ctx.Sources[sourceRef]
		if !ok {
			return fmt.Errorf("business validation: evidence %s source_ref %q not found", eid, sourceRef)
		}
		quote, _ := item["quote"].(string)
		if !strings.Contains(normalizeForQuoteMatch(source.Text), normalizeForQuoteMatch(quote)) {
			return fmt.Errorf("business validation: evidence %s quote not found in source %s", eid, sourceRef)
		}
		if speaker, _ := item["speaker"].(string); speaker != "" && source.Speaker != "" && speaker != source.Speaker {
			return fmt.Errorf("business validation: evidence %s speaker %q != source %q", eid, speaker, source.Speaker)
		}
		if !sameStartMS(item["start_ms"], source.StartMS) {
			return fmt.Errorf("business validation: evidence %s start_ms does not match source %s", eid, sourceRef)
		}
	}

	decision, _ := card["decision"].(map[string]any)
	if err := validateStateBasis(decision, ctx.Facts); err != nil {
		return err
	}

	// 至多一个主顾虑。
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

	// 停止联系 / 医疗暂停时不得生成面向客户的动作或销售机会。
	contactPolicy, _ := card["contact_policy"].(string)
	action, hasAction := card["primary_action"].(map[string]any)
	actionType := ""
	if hasAction {
		actionType, _ = action["type"].(string)
	}
	clinicalConflict, _ := card["clinical_conflict"].(map[string]any)
	conflictActive, _ := clinicalConflict["active"].(bool)

	if contactPolicy == "stop" && actionType == "sales_follow_up" {
		return fmt.Errorf("business validation: contact_policy=stop must not produce sales_follow_up")
	}
	if conflictActive {
		if actionType == "sales_follow_up" {
			return fmt.Errorf("business validation: clinical conflict must not produce sales_follow_up")
		}
		if findings, ok := card["product_findings"].([]any); ok && len(findings) > 0 {
			return fmt.Errorf("business validation: clinical conflict must not produce product findings")
		}
	}

	if err := validateProductFindings(card, ctx); err != nil {
		return err
	}

	// 低置信不得生成动作。
	if confidence, _ := card["confidence"].(string); confidence == "low" && hasAction {
		return fmt.Errorf("business validation: low confidence must not produce primary_action")
	}
	return nil
}

func validateStateBasis(decision map[string]any, facts map[string]businessFactRef) error {
	basis, _ := decision["state_basis"].(string)
	if basis != "verified_fact" {
		return nil
	}
	state, _ := decision["decision_state"].(string)
	want := ""
	switch state {
	case "已付款":
		want = "付款"
	case "已交定金":
		want = "定金"
	case "已预约":
		want = "预约"
	default:
		return fmt.Errorf("business validation: verified_fact is not valid for decision_state %q", state)
	}
	for _, fact := range facts {
		if fact.Type == want {
			return nil
		}
	}
	return fmt.Errorf("business validation: state_basis=verified_fact but no %s fact exists", want)
}

func validateProductFindings(card map[string]any, ctx cardValidationContext) error {
	findings, _ := card["product_findings"].([]any)
	seen := make(map[string]bool, len(findings))
	for _, row := range findings {
		finding, _ := row.(map[string]any)
		findingType, _ := finding["finding_type"].(string)
		switch findingType {
		case "可能漏推":
			if !ctx.AllowMissed {
				return fmt.Errorf("business validation: 可能漏推 is disabled (allow_missed_opportunity=false)")
			}
		case "升级机会", "替代方案":
			if !ctx.AllowExtended {
				return fmt.Errorf("business validation: %s is disabled (allow_extended_opportunities=false)", findingType)
			}
		}
		productID, hasID := finding["product_id"].(float64)
		name, _ := finding["product_name"].(string)
		key := fmt.Sprintf("%s|%v", findingType, name)
		if hasID {
			ref, ok := ctx.Catalog[int64(productID)]
			if !ok {
				return fmt.Errorf("business validation: product_id %d not found in catalog", int64(productID))
			}
			if name != "" && ref.Name != "" && name != ref.Name {
				return fmt.Errorf("business validation: product_id %d name %q != catalog %q", int64(productID), name, ref.Name)
			}
			version, _ := finding["product_version"].(string)
			if version != "" && ref.Version != "" && version != ref.Version {
				return fmt.Errorf("business validation: product_id %d version %q != catalog %q", int64(productID), version, ref.Version)
			}
			key = fmt.Sprintf("%s|%d", findingType, int64(productID))
		}
		if seen[key] {
			return fmt.Errorf("business validation: duplicate product finding %s", key)
		}
		seen[key] = true
		if findingType == "联合机会" {
			if requires, ok := finding["requires_clinical_confirmation"].(bool); !ok || !requires {
				return fmt.Errorf("business validation: 联合机会 must require clinical confirmation")
			}
		}
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

// collectEvidenceIDStrings 收集 JSON 中所有形如 ev_N 的字符串。
func collectEvidenceIDStrings(value any) []string {
	refs := make([]string, 0, 8)
	var walk func(node any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			if evidenceIDPattern.MatchString(typed) {
				refs = append(refs, typed)
			}
		}
	}
	walk(value)
	return refs
}

func sameStartMS(raw any, want *int) bool {
	if raw == nil {
		return want == nil
	}
	value, ok := raw.(float64)
	if !ok {
		return false
	}
	if want == nil {
		return false
	}
	return int(value) == *want
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

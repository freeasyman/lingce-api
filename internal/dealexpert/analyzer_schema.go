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
	ConsultantName string
	Sources        map[string]evidenceSource
	SegmentOrder   []string
	Facts          map[string]businessFactRef
	Catalog        map[int64]catalogRef
	AllowMissed    bool
	AllowExtended  bool
	// Repairs 记录 API 为模型纠正过的引用（source_ref / speaker / start_ms）。
	Repairs []string
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
	// 模型常给出“待核实/停止联系”却仍附带动作；按触达策略收敛，保留卡片其余内容。
	if shouldDropAction(card) {
		card["primary_action"] = nil
	}
	// 开关未开放或医疗冲突时，模型仍可能输出销售型产品发现；按策略过滤而不是废掉整张卡。
	sanitizeProductFindings(card, ctx)
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

	if brief, ok := card["brief"].(map[string]any); ok {
		for _, key := range []string{"situation", "blocker", "next_step"} {
			text, _ := brief[key].(string)
			for _, forbidden := range []string{"保证成交", "百分百", "一定能成", "绝对能成"} {
				if strings.Contains(text, forbidden) {
					return fmt.Errorf("business validation: brief.%s contains forbidden promise %q", key, forbidden)
				}
			}
		}
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

	// 证据必须能在转写/事实中找到；引用错位时由 API 校正，找不到才拒绝。
	for _, item := range items {
		if err := ctx.resolveEvidence(item); err != nil {
			return err
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

// resolveEvidence 校验并尽量修复模型给错的 source_ref / speaker / start_ms。
// 规则：quote 必须真实存在；优先用声明的来源，其次唯一命中，最后才拒绝。
func (ctx *cardValidationContext) resolveEvidence(item map[string]any) error {
	eid, _ := item["id"].(string)
	quote, _ := item["quote"].(string)
	normalizedQuote := normalizeForQuoteMatch(quote)
	if normalizedQuote == "" {
		return fmt.Errorf("business validation: evidence %s quote is empty", eid)
	}

	declared, _ := item["source_ref"].(string)
	if source, ok := ctx.Sources[declared]; ok && strings.Contains(normalizeForQuoteMatch(source.Text), normalizedQuote) {
		return ctx.applySource(item, declared, source, false)
	}

	// 唯一命中：quote 出现在另一个来源里（含连续两段的拼接）。
	if ref, source, ok := ctx.findQuoteSource(normalizedQuote); ok {
		return ctx.applySource(item, ref, source, true)
	}

	// 多段拼接：模型常把几段话用「……」连成一条引用，按片段分别定位。
	if ref, source, ok := ctx.findCompositeQuote(quote); ok {
		return ctx.applySource(item, ref, source, true)
	}

	// 近似命中：清洗改写过标点/口语，允许高相似度匹配。
	if ref, source, score, ok := ctx.findSimilarSource(quote); ok {
		return ctx.applySource(item, ref, source, true)
	} else {
		return fmt.Errorf("business validation: evidence %s quote not found (best=%.2f on %s, quote=%q)", eid, score, ref, truncateRunes(quote, 40))
	}
}

func (ctx *cardValidationContext) applySource(item map[string]any, ref string, source evidenceSource, repaired bool) error {
	eid, _ := item["id"].(string)
	// 来源以 API 侧为准：模型给错 source_ref / speaker / start_ms 都校正，不因此拒绝整张卡。
	changed := repaired
	if item["source_ref"] != ref {
		item["source_ref"] = ref
		changed = true
	}
	if source.Speaker != "" && item["speaker"] != source.Speaker {
		item["speaker"] = source.Speaker
		changed = true
	}
	if source.StartMS != nil {
		if !sameStartMS(item["start_ms"], source.StartMS) {
			item["start_ms"] = float64(*source.StartMS)
			changed = true
		}
	} else if item["start_ms"] != nil {
		item["start_ms"] = nil
		changed = true
	}
	if changed {
		ctx.Repairs = append(ctx.Repairs, eid+"->"+ref)
	}
	return nil
}

func (ctx *cardValidationContext) findQuoteSource(normalizedQuote string) (string, evidenceSource, bool) {
	matches := make([]struct {
		ref    string
		source evidenceSource
	}, 0, 2)
	for ref, source := range ctx.Sources {
		if strings.Contains(normalizeForQuoteMatch(source.Text), normalizedQuote) {
			matches = append(matches, struct {
				ref    string
				source evidenceSource
			}{ref, source})
		}
	}
	if len(matches) == 1 {
		return matches[0].ref, matches[0].source, true
	}
	// 连续分段拼接（最多三段），用于模型把一问一答摘成一条的情况。
	for i := 0; i < len(ctx.SegmentOrder); i++ {
		combined := ""
		for length := 1; length <= 3 && i+length <= len(ctx.SegmentOrder); length++ {
			combined += normalizeForQuoteMatch(ctx.Sources[ctx.SegmentOrder[i+length-1]].Text)
			if strings.Contains(combined, normalizedQuote) {
				return ctx.SegmentOrder[i], ctx.Sources[ctx.SegmentOrder[i]], true
			}
		}
	}
	return "", evidenceSource{}, false
}

func (ctx *cardValidationContext) findSimilarSource(quote string) (string, evidenceSource, float64, bool) {
	bestRef := ""
	bestScore := 0.0
	best := evidenceSource{}
	for ref, source := range ctx.Sources {
		score := bigramSimilarity(quote, source.Text)
		if score > bestScore {
			bestScore, bestRef, best = score, ref, source
		}
	}
	if bestScore >= 0.9 {
		return bestRef, best, bestScore, true
	}
	return bestRef, best, bestScore, false
}

// findCompositeQuote 处理「A……B……」这类多段拼接引用：
// 按省略号/句末标点拆段，逐段查找，覆盖率 >= 0.8 视为可定位，取最长命中段作为来源。
func (ctx *cardValidationContext) findCompositeQuote(quote string) (string, evidenceSource, bool) {
	parts := splitQuoteParts(quote)
	if len(parts) < 2 {
		return "", evidenceSource{}, false
	}
	totalChars := 0
	coveredChars := 0
	bestRef := ""
	bestLen := 0
	best := evidenceSource{}
	for _, part := range parts {
		normalized := normalizeForQuoteMatch(part)
		if len([]rune(normalized)) < 4 {
			continue
		}
		totalChars += len([]rune(normalized))
		if ref, source, ok := ctx.findQuoteSource(normalized); ok {
			coveredChars += len([]rune(normalized))
			if len([]rune(normalized)) > bestLen {
				bestLen, bestRef, best = len([]rune(normalized)), ref, source
			}
		}
	}
	if totalChars == 0 || bestRef == "" {
		return "", evidenceSource{}, false
	}
	if float64(coveredChars)/float64(totalChars) < 0.8 {
		return "", evidenceSource{}, false
	}
	return bestRef, best, true
}

func splitQuoteParts(quote string) []string {
	replacer := strings.NewReplacer("……", "|", "…", "|", "...", "|", "。", "|", "！", "|", "？", "|", "；", "|")
	raw := replacer.Replace(quote)
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(raw, "|") {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	return parts
}

// shouldDropAction：stop/review 或低置信时，动作必须为空。
func shouldDropAction(card map[string]any) bool {
	if card["primary_action"] == nil {
		return false
	}
	policy, _ := card["contact_policy"].(string)
	confidence, _ := card["confidence"].(string)
	return policy == "stop" || policy == "review" || confidence == "low"
}

// sanitizeProductFindings 去掉策略不允许的产品发现与重复项。
func sanitizeProductFindings(card map[string]any, ctx cardValidationContext) {
	rows, ok := card["product_findings"].([]any)
	if !ok {
		return
	}
	conflict, _ := card["clinical_conflict"].(map[string]any)
	conflictActive, _ := conflict["active"].(bool)
	kept := make([]any, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		finding, ok := row.(map[string]any)
		if !ok {
			continue
		}
		if conflictActive {
			continue
		}
		findingType, _ := finding["finding_type"].(string)
		if findingType == "可能漏推" && !ctx.AllowMissed {
			continue
		}
		if (findingType == "升级机会" || findingType == "替代方案") && !ctx.AllowExtended {
			continue
		}
		name, _ := finding["product_name"].(string)
		key := findingType + "|" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, finding)
	}
	card["product_findings"] = kept
}

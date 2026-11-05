package dealexpert

import (
	"fmt"
	"strings"
)

// 本文件是 v2.1（rc11）后台分析卡到现有 deal-card-v1.1 展示契约的服务端兼容适配层。
// 目标：只替换后台分析内容，前端展示结构、字段名、枚举和读取路径保持不变。
// 适配是纯结构搬运：v2.1 编译器已经降级/清空的字段在这里保持降级，不补造客户结论。

const (
	// v210CardSchemaVersion 是 v2.1 后台分析卡的 schema 标识。
	v210CardSchemaVersion = "deal-card-v2.1"

	// v1.1 展示契约无法承载 v2.1 的服务端模板字段，用固定文案补位。
	// 文案只描述动作类型，不添加客户事实、承诺、疗效或诊断。
	v210DefaultActionSuggestion = "按已核验的信息确认下一步安排，不承诺疗效、价格或结果。"
	v210DefaultPracticeReuse    = "同类客户沟通场景可复用。"
)

var v210ActionSuggestions = map[string]string{
	"confirm_next_step":    "先向客户确认方便的时间与下一步安排，不承诺疗效、价格或结果。",
	"professional_handoff": "把录音中的未解决事项连同原文交给专业人员核实，再回复客户。",
	"review_sources":       "先核对录音来源、客户主体与未确认事项，再决定是否联系。",
}

var v210PracticeReuse = map[string]string{
	"请澄清客户当前问题和所讨论的方案。":   "客户问题或方案不清楚时可复用。",
	"请客户复述理解，并核对仍需解释的问题。": "客户刚听完解释、需要确认理解时可复用。",
	"说明接待流程，并确认客户希望如何安排。": "客户询问流程或时间安排时可复用。",
	"先征得客户对后续联系和安排的许可。":   "需要再次联系或安排下一步时可复用。",
	"将未解决的问题及原文交给专业人员核实。": "涉及专业判断或未解决问题时可复用。",
}

// v1.1 枚举集合。v2.1 的子集可以直接复用，超出集合的值保守降级。
var (
	v11DecisionStates = map[string]bool{
		"了解比较中": true, "有意向但未决定": true, "暂缓": true, "明确不考虑": true,
		"已预约": true, "已交定金": true, "已付款": true, "无法判断": true,
	}
	v11StateBases = map[string]bool{
		"verified_fact": true, "reported_only": true, "transcript_judgment": true, "unknown": true,
	}
	v11NeedStates   = map[string]bool{"明确": true, "部分明确": true, "不清": true, "无法判断": true}
	v11TrustTargets = map[string]bool{"医生": true, "机构": true, "方案": true, "承诺": true, "其他": true}
	v11TrustStates  = map[string]bool{"已建立": true, "部分建立": true, "存疑": true, "无法判断": true}
	v11ConcernTypes = map[string]bool{
		"效果与适配": true, "安全与恢复": true, "价格与价值": true, "医生与机构信任": true,
		"家属或共同决策人": true, "时间与流程": true, "竞品比较": true, "疼痛隐私情绪": true, "其他": true,
	}
	v11FindingTypes = map[string]bool{
		"已正确推荐": true, "推荐不清": true, "可能漏推": true, "升级机会": true, "联合机会": true, "替代方案": true,
	}
	v11ActionTypes = map[string]bool{
		"sales_follow_up": true, "internal_coordination": true, "internal_review": true,
	}
	v11DueBases = map[string]bool{
		"today": true, "within_48h": true, "within_7d": true,
		"after_family_discussion": true, "after_clinical_confirmation": true, "unknown": true,
	}
	v11ContactPolicies = map[string]bool{"allow": true, "stop": true, "pause_clinical": true, "review": true}
	v11Speakers        = map[string]bool{"客户": true, "咨询师": true, "家属": true, "医生": true, "系统事实": true, "未知": true}
	v11OutcomeTypes    = map[string]bool{"预约": true, "定金": true, "付款": true, "退款": true}
	v11Confidences     = map[string]bool{"high": true, "medium": true, "low": true}
	v11QualityNames    = []string{"开场建立权威", "需求探索", "问题放大", "专业呈现", "方案定制", "异议化解", "促成与收尾"}
)

// adaptV210Card 把 deal-card-v2.1 编译结果映射成 deal-card-v1.1。
// 个人字段（decision/trust/need/concerns）原样沿用 v2.1 编译器的发布结果：
// 主体未绑定时它们已经是 无法判断/空数组，适配层不会把它们补成客户结论。
func adaptV210Card(card map[string]any) (map[string]any, error) {
	if card == nil {
		return nil, fmt.Errorf("adapt v210 card: empty card")
	}
	if version, _ := card["schema_version"].(string); version != v210CardSchemaVersion {
		return nil, fmt.Errorf("adapt v210 card: unexpected schema_version %q", version)
	}
	conclusion := adaptV210Conclusion(card["conclusion"])
	rawDecision := asObject(card["decision"])
	decision := adaptV210Decision(card["decision"])
	// v1.1 的 outcome_signals 只表达“录音里提到、尚未被业务系统确认”的事项，
	// 因此只接收 event_reports 里的 planned 计划；口述已完成/否认进入次要观察。
	addV210OutcomeSignals(decision, card["event_reports"])
	contactPolicy := asString(card["contact_policy"])
	if !v11ContactPolicies[contactPolicy] {
		contactPolicy = "review"
	}
	clinical := adaptV210ClinicalConflict(card["clinical_conflict"])
	products := adaptV210ProductFindings(card["product_findings"])
	action := adaptV210Action(card["primary_action"])
	confidence := adaptV210Confidence(card["confidence"])
	// v1.1 的 top-level 不变量：医疗冲突时不发布产品机会、只允许内部协调动作；
	// review/stop 或低置信时不发布销售动作。适配层按展示契约收敛，避免卡片自相矛盾。
	if clinical["active"] == true {
		if contactPolicy != "stop" && contactPolicy != "pause_clinical" {
			contactPolicy = "pause_clinical"
		}
		products = []any{}
		if action != nil && asString(asObject(action)["type"]) != "internal_coordination" {
			action = nil
		}
	}
	if contactPolicy == "stop" || contactPolicy == "review" || confidence == "low" {
		action = nil
	}
	secondary := adaptV210SecondaryFindings(card, decision)
	out := map[string]any{
		"schema_version":     dealCardVersion,
		"conclusion":         conclusion,
		"decision":           decision,
		"clinical_conflict":  clinical,
		"contact_policy":     contactPolicy,
		"product_findings":   products,
		"primary_action":     action,
		"secondary_findings": secondary,
		"evidence_refs":      adaptV210Evidence(card["evidence_refs"]),
		"quality":            adaptV210Quality(card["quality"]),
		"coach":              adaptV210Coach(card["coach"]),
		"confidence":         confidence,
		"unknowns":           adaptV210Unknowns(card["unknowns"]),
	}
	// v2.1 的 brief.blocker 恒为 null；非暂缓的 condition 在 v1.1 里没有结构化位置，
	// 作为“卡在”文案保留，避免摘要比结构化字段更空。
	rawCondition := strings.TrimSpace(asString(rawDecision["condition"]))
	if brief := adaptV210Brief(card["brief"], conclusion, decision, rawCondition); brief != nil {
		out["brief"] = brief
	}
	return out, nil
}

func adaptV210Conclusion(raw any) map[string]any {
	c := asObject(raw)
	impact := asString(c["business_impact"])
	switch impact {
	case "仍可推进", "已流失", "已成交可复盘", "无法判断":
	default:
		impact = "无法判断"
	}
	current := strings.TrimSpace(asString(c["current_state"]))
	if current == "" {
		current = "本次录音没有形成可发布的经营结论。"
	}
	verification := strings.TrimSpace(asString(c["verification"]))
	if verification == "" {
		verification = "请人工核对来源与业务事实后再决定后续处理。"
	}
	return map[string]any{
		"current_state":   current,
		"business_impact": impact,
		"main_basis":      v11Refs(asSlice(c["main_basis"]), 0),
		"verification":    verification,
	}
}

func adaptV210Decision(raw any) map[string]any {
	d := asObject(raw)
	state := asString(d["decision_state"])
	if !v11DecisionStates[state] {
		state = "无法判断"
	}
	stateBasis := asString(d["state_basis"])
	if !v11StateBases[stateBasis] {
		stateBasis = "unknown"
	}
	needState := asString(d["need_state"])
	if !v11NeedStates[needState] {
		needState = "无法判断"
	}
	out := map[string]any{
		"decision_state":      state,
		"state_basis":         stateBasis,
		"evidence_refs":       v11Refs(asSlice(d["evidence_refs"]), 0),
		"condition":           nil,
		"condition_due_quote": nil,
		"trust":               adaptV210Trust(d["trust"]),
		"need_state":          needState,
		"need_summary":        nilWithText(d["need_summary"]),
		"need_evidence_refs":  v11Refs(asSlice(d["need_evidence_refs"]), 0),
		"concerns":            adaptV210Concerns(d["concerns"]),
		"outcome_signals":     []any{},
	}
	// v1.1 只在“暂缓”时允许 condition；v2.1 允许其他状态带 condition。
	// 非暂缓的 condition 移入 brief.blocker / 次要观察，避免声明式丢弃。
	if state == "暂缓" {
		condition := strings.TrimSpace(asString(d["condition"]))
		if condition == "" {
			// v2.1 编译器本应把无条件的暂缓降级；这里保守再降一次，避免展示空条件。
			out["decision_state"] = "无法判断"
			out["state_basis"] = "unknown"
			out["evidence_refs"] = []any{}
		} else {
			out["condition"] = condition
			out["condition_due_quote"] = nilWithText(d["condition_due_quote"])
		}
	}
	return out
}

func adaptV210Trust(raw any) []any {
	out := []any{}
	for _, item := range asSlice(raw) {
		row := asObject(item)
		target := asString(row["trust_target"])
		state := asString(row["state"])
		if !v11TrustTargets[target] || !v11TrustStates[state] {
			continue
		}
		label := strings.TrimSpace(asString(row["target_label"]))
		if len([]rune(label)) > 12 {
			label = string([]rune(label)[:12])
		}
		item := map[string]any{
			"trust_target":  target,
			"state":         state,
			"summary":       nilWithText(row["summary"]),
			"evidence_refs": v11Refs(asSlice(row["evidence_refs"]), 0),
		}
		if target == "其他" {
			if label == "" {
				// v1.1 要求“其他”必须带 12 字以内标签；无标签时不发布这一条。
				continue
			}
			item["target_label"] = label
		} else if label != "" {
			item["target_label"] = label
		} else {
			item["target_label"] = nil
		}
		out = append(out, item)
	}
	return out
}

func adaptV210Concerns(raw any) []any {
	out := []any{}
	primary := false
	for _, item := range asSlice(raw) {
		row := asObject(item)
		concernType := asString(row["type"])
		summary := strings.TrimSpace(asString(row["summary"]))
		refs := v11Refs(asSlice(row["evidence_refs"]), 0)
		if !v11ConcernTypes[concernType] || summary == "" || len(refs) == 0 {
			continue
		}
		label := strings.TrimSpace(asString(row["type_label"]))
		if len([]rune(label)) > 12 {
			label = string([]rune(label)[:12])
		}
		isPrimary := row["is_primary"] == true
		if isPrimary {
			if primary {
				// 与 v2.1 一致：最多一个主顾虑，其余降为非主。
				isPrimary = false
			} else {
				primary = true
			}
		}
		concern := map[string]any{
			"type":          concernType,
			"summary":       summary,
			"is_primary":    isPrimary,
			"evidence_refs": refs,
		}
		if concernType == "其他" {
			if label == "" {
				continue
			}
			concern["type_label"] = label
		} else if label != "" {
			concern["type_label"] = label
		} else {
			concern["type_label"] = nil
		}
		out = append(out, concern)
	}
	return out
}

// addV210OutcomeSignals 把“计划中”的口头事件映射成 v1.1 的 outcome_signals。
// 口述已完成/否认/其他类型不写入，避免把未来计划展示成已预约、已付款。
func addV210OutcomeSignals(decision map[string]any, raw any) {
	signals := []any{}
	for _, item := range asSlice(raw) {
		row := asObject(item)
		eventType := asString(row["event_type"])
		state := asString(row["state"])
		subject := asString(row["subject"])
		if !v11OutcomeTypes[eventType] || state != "planned" {
			continue
		}
		if subject == "unknown" {
			continue
		}
		refs := v11Refs(asSlice(row["evidence_refs"]), 1)
		if len(refs) == 0 {
			continue
		}
		signals = append(signals, map[string]any{
			"type":          eventType,
			"summary":       "录音提到计划，尚未获业务系统确认。",
			"evidence_refs": refs,
		})
	}
	decision["outcome_signals"] = signals
}

func adaptV210ClinicalConflict(raw any) map[string]any {
	c := asObject(raw)
	return map[string]any{
		"active":        c["active"] == true,
		"reason":        nilWithText(c["reason"]),
		"evidence_refs": v11Refs(asSlice(c["evidence_refs"]), 0),
	}
}

func adaptV210Evidence(raw any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, item := range asSlice(raw) {
		row := asObject(item)
		id := strings.TrimSpace(asString(row["id"]))
		quote := asString(row["quote"])
		speaker := asString(row["speaker"])
		if id == "" || quote == "" || seen[id] || !v11Speakers[speaker] {
			continue
		}
		seen[id] = true
		out = append(out, map[string]any{
			"id":         id,
			"quote":      quote,
			"speaker":    speaker,
			"source_ref": asString(row["source_ref"]),
			"start_ms":   asIntOrNil(row["start_ms"]),
			"used_for":   v11EvidenceUsage(asSlice(row["used_for"])),
		})
	}
	return out
}

func v11EvidenceUsage(raw []any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, item := range raw {
		value := asString(item)
		if value == "" || seen[value] {
			continue
		}
		if !v11EvidenceUsageValues[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

var v11EvidenceUsageValues = map[string]bool{
	"结论": true, "信任": true, "需求": true, "顾虑": true,
	"产品": true, "动作": true, "质检": true, "复盘": true, "医疗协作": true,
}

func adaptV210ProductFindings(raw any) []any {
	out := []any{}
	for _, item := range asSlice(raw) {
		row := asObject(item)
		name := strings.TrimSpace(asString(row["product_name"]))
		finding := asString(row["finding_type"])
		refs := v11Refs(asSlice(row["evidence_refs"]), 1)
		if name == "" || !v11FindingTypes[finding] || len(refs) == 0 {
			continue
		}
		confidence := asString(row["confidence"])
		if !v11Confidences[confidence] {
			confidence = "low"
		}
		out = append(out, map[string]any{
			"product_id":                     asIntOrNil(row["product_id"]),
			"product_name":                   name,
			"finding_type":                   finding,
			"customer_evidence":              strings.TrimSpace(asString(row["customer_evidence"])),
			"matching_reason":                strings.TrimSpace(asString(row["matching_reason"])),
			"recommended_by_consultant":      row["recommended_by_consultant"] == true,
			"requires_clinical_confirmation": row["requires_clinical_confirmation"] == true,
			"suggested_action":               nilWithText(row["suggested_action"]),
			"product_version":                nilWithText(row["product_version"]),
			"confidence":                     confidence,
			"evidence_refs":                  refs,
		})
	}
	return out
}

func adaptV210Action(raw any) any {
	a := asObject(raw)
	if a == nil {
		return nil
	}
	actionType := asString(a["type"])
	goal := strings.TrimSpace(asString(a["goal"]))
	body := strings.TrimSpace(asString(a["action"]))
	refs := v11Refs(asSlice(a["source_evidence_refs"]), 1)
	if !v11ActionTypes[actionType] || goal == "" || body == "" || len(refs) == 0 {
		return nil
	}
	due := asString(a["due_basis"])
	if !v11DueBases[due] {
		due = "unknown"
	}
	suggestion := v210ActionSuggestions[asString(a["template"])]
	if suggestion == "" {
		suggestion = v210DefaultActionSuggestion
	}
	return map[string]any{
		"type":                 actionType,
		"goal":                 goal,
		"action":               body,
		"suggested_content":    suggestion,
		"owner_employee_id":    nil,
		"due_basis":            due,
		"due_quote":            nil,
		"source_evidence_refs": refs,
	}
}

func adaptV210Quality(raw any) map[string]any {
	q := asObject(raw)
	standard := strings.TrimSpace(asString(q["standard_version"]))
	if standard == "" {
		standard = dealQualityStandard
	}
	dimensions := asObject(q["dimensions"])
	out := map[string]any{}
	for _, name := range v11QualityNames {
		item := asObject(dimensions[name])
		assessment := asString(item["assessment"])
		switch assessment {
		case "scored", "not_applicable", "insufficient_evidence":
		default:
			assessment = "insufficient_evidence"
		}
		score := asIntOrNil(item["score"])
		refs := v11Refs(asSlice(item["evidence_refs"]), 0)
		// v1.1：scored 必须有 1–5 分且至少一条证据；否则保留“证据不足”而不是空分。
		if assessment == "scored" && (score == nil || len(refs) == 0) {
			assessment = "insufficient_evidence"
		}
		if assessment != "scored" {
			score = nil
		}
		comment := strings.TrimSpace(asString(item["comment"]))
		if comment == "" {
			if assessment == "not_applicable" {
				comment = "本次录音不涉及该维度。"
			} else {
				comment = "有效证据不足，暂不评估该维度。"
			}
		}
		impact := nilWithText(item["impact_on_deal"])
		highlight := nilWithText(item["highlight_narrative"])
		if score != nil && scoreInt(score) <= 3 {
			// v1.1：3 分及以下不输出“可复用”叙述。
			highlight = nil
		}
		if assessment != "scored" {
			impact = nil
			highlight = nil
		}
		out[name] = map[string]any{
			"assessment":          assessment,
			"score":               score,
			"comment":             comment,
			"evidence_refs":       refs,
			"impact_on_deal":      impact,
			"highlight_narrative": highlight,
		}
	}
	return map[string]any{"standard_version": standard, "dimensions": out}
}

func adaptV210Coach(raw any) map[string]any {
	c := asObject(raw)
	practices := []any{}
	for _, item := range asSlice(c["observations"]) {
		row := asObject(item)
		behavior := strings.TrimSpace(asString(row["behavior"]))
		refs := v11Refs(asSlice(row["evidence_refs"]), 1)
		if behavior == "" || len(refs) == 0 {
			continue
		}
		reuse := v210PracticeReuse[behavior]
		if reuse == "" {
			reuse = v210DefaultPracticeReuse
		}
		practices = append(practices, map[string]any{
			"behavior":          behavior,
			"customer_response": nilWithText(row["customer_response"]),
			"reusable_when":     reuse,
			"evidence_refs":     refs,
		})
	}
	var improvement any
	if imp := asObject(c["improvement"]); imp != nil {
		suggestion := strings.TrimSpace(asString(imp["suggestion"]))
		refs := v11Refs(asSlice(imp["evidence_refs"]), 1)
		if suggestion != "" && len(refs) > 0 {
			improvement = map[string]any{"suggestion": suggestion, "evidence_refs": refs}
		}
	}
	// v2.1 的 coach 不含退款复盘；这里显式留空而不是补造退款原因。
	return map[string]any{"successful_practices": practices, "improvement": improvement, "refund_review": nil}
}

func adaptV210Confidence(raw any) string {
	value := asString(raw)
	if v11Confidences[value] {
		return value
	}
	return "low"
}

func adaptV210Unknowns(raw any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, item := range asSlice(raw) {
		value := strings.TrimSpace(asString(item))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// adaptV210Brief 生成 v1.1 的经营结论三段。v2.1 的 brief.blocker 恒为 null，
// 这里用已发布的“主顾虑/等待条件”补位，不新增事实。
func adaptV210Brief(raw any, conclusion, decision map[string]any, rawCondition string) any {
	b := asObject(raw)
	situation := strings.TrimSpace(asString(b["situation"]))
	if situation == "" {
		situation = asString(conclusion["current_state"])
	}
	situation = clipRunes(situation, 100)
	nextStep := strings.TrimSpace(asString(b["next_step"]))
	nextStep = clipRunes(nextStep, 100)
	if len([]rune(situation)) < 10 || len([]rune(nextStep)) < 10 {
		return nil
	}
	blocker := ""
	if b != nil {
		blocker = strings.TrimSpace(asString(b["blocker"]))
	}
	if blocker == "" {
		blocker = v210BlockerFromDecision(decision)
	}
	if blocker == "" {
		blocker = rawCondition
	}
	refs := v11Refs(asSlice(b["evidence_refs"]), 0)
	if len(refs) == 0 {
		refs = v11Refs(asSlice(conclusion["main_basis"]), 0)
	}
	if len(refs) == 0 {
		refs = v11Refs(asSlice(decision["evidence_refs"]), 0)
	}
	refs = v11Refs(refs, 4)
	if len(refs) == 0 {
		// 没有可引用证据时宁可不展示 brief，前端会回落到结论与动作。
		return nil
	}
	out := map[string]any{
		"situation":     situation,
		"blocker":       v11BlockerText(blocker),
		"next_step":     nextStep,
		"evidence_refs": refs,
	}
	return out
}

func v210BlockerFromDecision(decision map[string]any) string {
	var primary, first string
	for _, item := range asSlice(decision["concerns"]) {
		row := asObject(item)
		summary := strings.TrimSpace(asString(row["summary"]))
		if summary == "" {
			continue
		}
		if first == "" {
			first = summary
		}
		if row["is_primary"] == true {
			primary = summary
			break
		}
	}
	if primary != "" {
		return primary
	}
	if first != "" {
		return first
	}
	return strings.TrimSpace(asString(decision["condition"]))
}

func v11BlockerText(value string) any {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) < 5 {
		return nil
	}
	if len(runes) > 70 {
		value = string(runes[:69]) + "…"
	}
	return value
}

// adaptV210SecondaryFindings 汇总 v1.1 没有独立字段的 v2.1 信息：
// 其他方案、录音级观察、口述事件、非业务标签。按原文摘要去重。
func adaptV210SecondaryFindings(card map[string]any, decision map[string]any) []any {
	out := []any{}
	seen := map[string]bool{}
	add := func(summary string, refs []any) {
		summary = strings.TrimSpace(summary)
		if summary == "" || seen[summary] {
			return
		}
		seen[summary] = true
		out = append(out, map[string]any{"summary": summary, "evidence_refs": refs})
	}
	for _, item := range asSlice(card["secondary_findings"]) {
		row := asObject(item)
		add(asString(row["summary"]), v11Refs(asSlice(row["evidence_refs"]), 0))
	}
	// 其他方案：v1.1 只有一个 decision，其余方案以“方案名 + 状态/条件”保留。
	if schemes := asSlice(card["scheme_decisions"]); len(schemes) > 1 {
		for _, item := range schemes[1:] {
			row := asObject(item)
			target := strings.TrimSpace(asString(row["target"]))
			state := asString(row["decision_state"])
			parts := []string{"其他方案"}
			if target != "" {
				parts = append(parts, "（"+target+"）")
			}
			if state != "" {
				parts = append(parts, "："+state)
			}
			if condition := strings.TrimSpace(asString(row["condition"])); condition != "" {
				parts = append(parts, "；条件："+condition)
			}
			add(strings.Join(parts, ""), v11Refs(asSlice(row["evidence_refs"]), 0))
		}
	}
	decisionNeed := strings.TrimSpace(asString(decision["need_summary"]))
	decisionConcerns := len(asSlice(decision["concerns"])) > 0
	decisionTrust := len(asSlice(decision["trust"])) > 0
	for _, item := range asSlice(card["recording_observations"]) {
		row := asObject(item)
		kind := asString(row["kind"])
		summary := strings.TrimSpace(asString(row["summary"]))
		if summary == "" {
			continue
		}
		switch kind {
		case "need":
			if decisionNeed != "" {
				continue
			}
			add("录音级需求观察（待核实）："+summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		case "concern":
			if decisionConcerns {
				continue
			}
			add("录音级顾虑观察（待核实）："+summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		case "trust":
			if decisionTrust {
				continue
			}
			add("录音级信任观察（待核实）："+summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		case "condition":
			add("客户提到的前置条件/现实限制："+summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		case "clinical_statement":
			add("待专业核实的临床陈述："+summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		case "staff_claim":
			add("员工陈述（不是客户意向）："+summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		case "non_business":
			add("非业务录音："+summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		case "intent":
			add("录音级意向观察（待核实）："+summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		default:
			add(summary, v11Refs(asSlice(row["evidence_refs"]), 0))
		}
	}
	// 口述事件里，非 planned 的状态不能进入 outcome_signals，保留为次要观察。
	for _, item := range asSlice(card["event_reports"]) {
		row := asObject(item)
		eventType := asString(row["event_type"])
		state := asString(row["state"])
		if !v11OutcomeTypes[eventType] || state == "planned" {
			continue
		}
		label := map[string]string{
			"claimed_completed": "口述称已完成（录音提及，待业务系统核实）",
			"denied":            "客户否认已发生",
			"unknown":           "状态未知",
		}[state]
		if label == "" {
			label = "状态待核实"
		}
		add("录音提到"+eventType+"："+label+"。", v11Refs(asSlice(row["evidence_refs"]), 0))
	}
	return out
}

// --- 通用小工具（JSON 解码后的 any 安全取值） ---

func asObject(value any) map[string]any {
	if object, ok := value.(map[string]any); ok && object != nil {
		return object
	}
	return nil
}

func asSlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

func asString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func nilWithText(value any) any {
	text := strings.TrimSpace(asString(value))
	if text == "" {
		return nil
	}
	return text
}

// v11Refs 过滤成 ev_N 引用并去重；limit>0 时截断到 limit 条。
func v11Refs(items []any, limit int) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, item := range items {
		ref := strings.TrimSpace(asString(item))
		if !evidenceIDPattern.MatchString(ref) || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func asIntOrNil(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case float64:
		if typed != float64(int64(typed)) {
			return nil
		}
		return int64(typed)
	case float32:
		if typed != float32(int(typed)) {
			return nil
		}
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	}
	return nil
}

func clipRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func scoreInt(value any) int {
	switch typed := value.(type) {
	case int64:
		return int(typed)
	case int:
		return typed
	}
	return 0
}

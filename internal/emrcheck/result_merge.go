package emrcheck

// mergeQualityResults removes duplicate results only when the rule
// configuration explicitly identifies the same field fact. Without fact_code,
// rule results remain separate because two rules in one field may describe
// different defects.
func mergeQualityResults(results []*CheckResult) []*CheckResult {
	if len(results) < 2 {
		return results
	}

	merged := make([]*CheckResult, 0, len(results))
	positions := make(map[string]int, len(results))
	for _, result := range results {
		if result == nil || result.FactCode == "" {
			merged = append(merged, result)
			continue
		}
		key := result.FieldCode + "\x00" + result.FactCode
		position, exists := positions[key]
		if !exists {
			positions[key] = len(merged)
			merged = append(merged, result)
			continue
		}
		merged[position] = preferQualityResult(merged[position], result)
	}
	return merged
}

func preferQualityResult(current, candidate *CheckResult) *CheckResult {
	if current == nil {
		return candidate
	}
	if candidate == nil {
		return current
	}
	if resultPriority(candidate) > resultPriority(current) {
		return candidate
	}
	return current
}

func resultPriority(result *CheckResult) int {
	priority := 0
	if result.ActualExecutionMode == "程序判断" {
		priority += 4
	}
	if result.HandlingResult == "阻断" {
		priority += 3
	}
	if result.Conclusion == "不符合" {
		priority += 2
	}
	if result.CheckStatus != "已完成" {
		priority++
	}
	return priority
}

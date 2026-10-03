package emrcheck

// filterLLMRulesByProgramResults removes only LLM rules whose declared
// prerequisite was conclusively answered by a program rule in this run.
func filterLLMRulesByProgramResults(rules []qualityRuleInput, programResults []*CheckResult) []qualityRuleInput {
	if len(rules) == 0 || len(programResults) == 0 {
		return rules
	}

	programFacts := make(map[string]string, len(programResults))
	for _, result := range programResults {
		if result == nil || result.CheckStatus != "已完成" {
			continue
		}
		programFacts[result.FieldCode+"\x00"+result.RuleCode] = result.Conclusion
	}

	filtered := make([]qualityRuleInput, 0, len(rules))
	for _, rule := range rules {
		skip := false
		for _, dependency := range rule.SkipIf {
			if conclusion, ok := programFacts[dependency.FieldCode+"\x00"+dependency.RuleCode]; ok && conclusion == dependency.Conclusion {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

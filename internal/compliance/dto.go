package compliance

type CreateRuleRequest struct {
	Code            string       `json:"code"`
	Name            string       `json:"name"`
	Category        string       `json:"category"`
	Scope           string       `json:"scope"`
	Enabled         *bool        `json:"enabled"`
	Severity        string       `json:"severity"`
	TriggerType     string       `json:"triggerType"`
	Conditions      JSONMap      `json:"conditions"`
	Description     string       `json:"description"`
	LegalBasis      string       `json:"legalBasis"`
	SuggestedScript string       `json:"suggestedScript"`
	Examples        RuleExamples `json:"examples"`
}

type UpdateRuleRequest struct {
	Code            *string       `json:"code"`
	Name            *string       `json:"name"`
	Category        *string       `json:"category"`
	Scope           *string       `json:"scope"`
	Enabled         *bool         `json:"enabled"`
	Severity        *string       `json:"severity"`
	TriggerType     *string       `json:"triggerType"`
	Conditions      *JSONMap      `json:"conditions"`
	Description     *string       `json:"description"`
	LegalBasis      *string       `json:"legalBasis"`
	SuggestedScript *string       `json:"suggestedScript"`
	Examples        *RuleExamples `json:"examples"`
}

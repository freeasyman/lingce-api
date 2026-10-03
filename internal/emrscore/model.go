package emrscore

type SectionCode string

type Finding struct {
	Code              string      `json:"code"`
	Section           SectionCode `json:"section"`
	Title             string      `json:"title"`
	Message           string      `json:"message"`
	Evidence          string      `json:"evidence,omitempty"`
	Suggestion        string      `json:"suggestion"`
	DeductPoints      int         `json:"deduct_points"`
	Source            string      `json:"source"`
	NeedsManualReview bool        `json:"needs_manual_review"`
}

type SectionScore struct {
	Section      SectionCode `json:"section"`
	Standard     int         `json:"standard"`
	ProgramScore int         `json:"program_score"`
	DeductPoints int         `json:"deduct_points"`
	Findings     []Finding   `json:"findings"`
	Pending      bool        `json:"pending"`
}

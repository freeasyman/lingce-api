package emrquality

type ListRequest struct {
	TenantID int64
	Group    string
	RuleType string
	Status   string
}

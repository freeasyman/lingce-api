package tenant

type TrialCustomerAssignOwnerRequest struct {
	SalesOwnerAdminID *int64 `json:"sales_owner_admin_id"`
}

type TrialCustomerUpsertAssignmentRequest struct {
	SalesOwnerAdminID *int64  `json:"sales_owner_admin_id"`
	Source           *string `json:"source,omitempty"`
	Notes            *string `json:"notes,omitempty"`
}

type TrialCustomerFollowUpCreateRequest struct {
	FollowUpType   string  `json:"follow_up_type"`
	Summary        string  `json:"summary"`
	Result         string  `json:"result"`
	NextFollowUpAt *string `json:"next_follow_up_at,omitempty"`
}

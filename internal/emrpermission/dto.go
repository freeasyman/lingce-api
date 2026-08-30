package emrpermission

type AssignmentResponse struct {
	EmployeeID  int64    `json:"employee_id"`
	TenantID    int64    `json:"tenant_id"`
	Enabled     bool     `json:"enabled"`
	EmrRoleCode string   `json:"emr_role_code"`
	Scope       string   `json:"scope"`
	Abilities   []string `json:"abilities"`
	UpdatedAt   string   `json:"updated_at"`
	UpdatedBy   *int64   `json:"updated_by,omitempty"`
}

type UpsertAssignmentRequest struct {
	Enabled     bool     `json:"enabled"`
	EmrRoleCode string   `json:"emr_role_code"`
	Scope       string   `json:"scope"`
	Abilities   []string `json:"abilities"`
}

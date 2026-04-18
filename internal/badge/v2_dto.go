package badge

type V2DeviceListRequest struct {
	Status           *string
	HealthStatus     *string
	ManufacturerCode *string
	DeviceNo         *string
	Realtime         bool
	Page             int
	PageSize         int
}

type V2BatchImportRequest struct {
	ManufacturerCode string `json:"manufacturer_code"`
	ManufacturerName string `json:"manufacturer_name"`
	ImportBatchNo    string `json:"import_batch_no,omitempty"`
	Devices          []struct {
		DeviceNo      string `json:"device_no"`
		HardwareModel string `json:"hardware_model,omitempty"`
	} `json:"devices"`
}

type V2BatchAcceptRequest struct {
	DeviceIDs         []int64 `json:"device_ids"`
	AcceptanceBatchNo string  `json:"acceptance_batch_no,omitempty"`
	SkipHealthCheck   bool    `json:"skip_health_check,omitempty"`
}

type V2BatchAssignRequest struct {
	DeviceIDs     []int64 `json:"device_ids"`
	TenantID      int64   `json:"tenant_id"`
	TenantName    string  `json:"tenant_name"`
	EmployeeID    int64   `json:"employee_id"`
	EmployeeName  string  `json:"employee_name"`
	EmployeePhone string  `json:"employee_phone,omitempty"`
}

type V2BatchReclaimRequest struct {
	DeviceIDs []int64 `json:"device_ids"`
	Reason    string  `json:"reason,omitempty"`
}

type V2TransferRequest struct {
	ToTenantID     int64  `json:"to_tenant_id"`
	ToTenantName   string `json:"to_tenant_name"`
	ToEmployeeID   int64  `json:"to_employee_id"`
	ToEmployeeName string `json:"to_employee_name"`
	Reason         string `json:"reason,omitempty"`
}

type V2UpdateDeviceRequest struct {
	HardwareModel *string    `json:"hardware_model,omitempty"`
	Metadata      JSONObject `json:"metadata,omitempty"`
}

type V2HealthCheckResult struct {
	DeviceID          int64      `json:"device_id"`
	DeviceNo          string     `json:"device_no"`
	Status            string     `json:"status,omitempty"`
	Passed            bool       `json:"passed"`
	HealthStatus      string     `json:"health_status"`
	HealthCheckResult JSONObject `json:"health_check_result"`
}

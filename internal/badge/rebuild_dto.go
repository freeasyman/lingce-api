package badge

type RebuildBadgeDeviceListRequest struct {
	TenantID    *int64
	EmployeeID  *int64
	BadgeStatus *string
	HealthLevel *string
	DeviceNo    *string
	Page        int
	PageSize    int
}

type RebuildBadgeDeviceActionRequest struct {
	Reason string `json:"reason,omitempty"`
}

type RebuildBadgeImportRequest struct {
	ManufacturerCode string `json:"manufacturer_code"`
	ManufacturerName string `json:"manufacturer_name,omitempty"`
	ImportBatchNo    string `json:"import_batch_no,omitempty"`
	Devices          []struct {
		DeviceNo      string `json:"device_no"`
		HardwareModel string `json:"hardware_model,omitempty"`
	} `json:"devices"`
}

type RebuildBadgeAssignRequest struct {
	TenantID   int64 `json:"tenant_id"`
	EmployeeID int64 `json:"employee_id"`
}

type RebuildBadgeImportResponse struct {
	SuccessCount int      `json:"success_count"`
	FailedCount  int      `json:"failed_count"`
	Duplicates   []string `json:"duplicates"`
}

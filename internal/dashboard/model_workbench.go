package dashboard

type OpsWorkbenchOverview struct {
	InstitutionCount  int64 `json:"institution_count"`
	DeviceCount       int64 `json:"device_count"`
	RecordingCount    int64 `json:"recording_count"`
	RecordingDuration int64 `json:"recording_duration_sec"`
	TaskTotal         int64 `json:"task_total"`
	TaskDone          int64 `json:"task_done"`
	TaskUndone        int64 `json:"task_undone"`
	DealCount         int64 `json:"deal_count"`
	DealAmount        int64 `json:"deal_amount"`
}

type OpsWorkbenchTrendPoint struct {
	Date  string `json:"date"`
	Value int64  `json:"value"`
}

type OpsWorkbenchTrend struct {
	Metric string                   `json:"metric"`
	Points []OpsWorkbenchTrendPoint `json:"points"`
}

type OpsWorkbenchTableRow struct {
	TenantID             int64  `json:"tenant_id"`
	TenantName           string `json:"tenant_name"`
	DeviceCount          int64  `json:"device_count"`
	RecordingCount       int64  `json:"recording_count"`
	RecordingDurationSec int64  `json:"recording_duration_sec"`
	TaskDone             int64  `json:"task_done"`
	TaskUndone           int64  `json:"task_undone"`
	DealCount            int64  `json:"deal_count"`
	DealAmount           int64  `json:"deal_amount"`
}

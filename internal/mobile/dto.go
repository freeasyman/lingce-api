package mobile

import "github.com/freeasyman/lingce-api/internal/recording"

type EmployeeSummary struct {
	ID             int64   `json:"id"`
	TenantID       int64   `json:"tenant_id"`
	TenantName     string  `json:"tenant_name"`
	FullName       string  `json:"full_name"`
	Phone          string  `json:"phone"`
	DepartmentID   *int64  `json:"department_id,omitempty"`
	DepartmentName *string `json:"department_name,omitempty"`
	RoleCode       string  `json:"role_code"`
	RoleName       string  `json:"role_name"`
}

type HomeSummary struct {
	TodoTaskCount    int `json:"todo_task_count"`
	DoneTaskCount    int `json:"done_task_count"`
	RecordingCount   int `json:"recording_count"`
	RecentTaskCount  int `json:"recent_task_count"`
	RecentRecordings int `json:"recent_recordings"`
}

type HomeResponse struct {
	Me               *EmployeeSummary               `json:"me"`
	Summary          HomeSummary                    `json:"summary"`
	TodoTasks        []*recording.TaskResponse      `json:"todo_tasks"`
	RecentRecordings []*recording.RecordingResponse `json:"recent_recordings"`
	ServerTime       string                         `json:"server_time"`
}

type RecordingListParams struct {
	Page           int
	PageSize       int
	Q              string
	AnalysisStatus string
	DateFrom       string
	DateTo         string
	TimeRange      string
	BusinessScope  string
	HasTask        *bool
	HasContentSeed *bool
	CriticalGap    *bool
	Sort           string
}

type TaskListParams struct {
	Page      int
	PageSize  int
	Status    string
	Q         string
	Priority  string
	TaskType  string
	DueBucket string
	DateFrom  string
	DateTo    string
	Sort      string
}

package dashboard

// AdminDashboardMetrics represents admin dashboard metrics
type AdminDashboardMetrics struct {
	Revenue struct {
		Value float64 `json:"value"`
		Trend float64 `json:"trend"`
	} `json:"revenue"`
	DealRate struct {
		Value float64 `json:"value"`
		Trend float64 `json:"trend"`
	} `json:"dealRate"`
	Patients struct {
		Value int     `json:"value"`
		Trend float64 `json:"trend"`
	} `json:"patients"`
	TaskCompletion struct {
		Value float64 `json:"value"`
		Trend float64 `json:"trend"`
	} `json:"taskCompletion"`
	TargetAchievement struct {
		Value float64 `json:"value"`
		Trend float64 `json:"trend"`
	} `json:"targetAchievement"`
	ServiceQuality struct {
		Value float64 `json:"value"`
		Trend float64 `json:"trend"`
	} `json:"serviceQuality"`
}

// AdminDashboardAlert represents an alert item
type AdminDashboardAlert struct {
	ID          int    `json:"id"`
	Priority    string `json:"priority"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// AdminDashboardTopPerformer represents a top performer
type AdminDashboardTopPerformer struct {
	Name    string  `json:"name"`
	Revenue float64 `json:"revenue"`
	Rank    int     `json:"rank"`
}

// AdminDashboardData represents admin dashboard response
type AdminDashboardData struct {
	Metrics       AdminDashboardMetrics        `json:"metrics"`
	Alerts        []AdminDashboardAlert        `json:"alerts"`
	TopPerformers []AdminDashboardTopPerformer `json:"topPerformers"`
}

// ConsultantDashboardMetrics represents consultant dashboard metrics
type ConsultantDashboardMetrics struct {
	TodayDeals struct {
		Count  int     `json:"count"`
		Amount float64 `json:"amount"`
	} `json:"todayDeals"`
	Following int `json:"following"`
	Pending   int `json:"pending"`
	DealRate  struct {
		Value float64 `json:"value"`
		Trend float64 `json:"trend"`
		Rank  int     `json:"rank"`
	} `json:"dealRate"`
}

// ConsultantDashboardCustomer represents a high priority customer
type ConsultantDashboardCustomer struct {
	ID          int    `json:"id"`
	Priority    string `json:"priority"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ConsultantDashboardData represents consultant dashboard response
type ConsultantDashboardData struct {
	Metrics               ConsultantDashboardMetrics    `json:"metrics"`
	HighPriorityCustomers []ConsultantDashboardCustomer `json:"highPriorityCustomers"`
	AbilityScore          struct {
		Overall float64 `json:"overall"`
		Rank    int     `json:"rank"`
		Total   int     `json:"total"`
	} `json:"abilityScore"`
}

// DoctorDashboardMetrics represents doctor dashboard metrics
type DoctorDashboardMetrics struct {
	TodayRecordings struct {
		Count       int `json:"count"`
		AvgDuration int `json:"avgDuration"`
	} `json:"todayRecordings"`
	Quality struct {
		Score float64 `json:"score"`
		Rate  float64 `json:"rate"`
	} `json:"quality"`
	WeeklyService struct {
		Count       int `json:"count"`
		AvgDuration int `json:"avgDuration"`
	} `json:"weeklyService"`
	QualityTrend struct {
		Value float64 `json:"value"`
		Trend float64 `json:"trend"`
	} `json:"qualityTrend"`
}

// DoctorDashboardRecording represents a recording to review
type DoctorDashboardRecording struct {
	ID          int     `json:"id"`
	Priority    string  `json:"priority"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

// DoctorDashboardData represents doctor dashboard response
type DoctorDashboardData struct {
	Metrics          DoctorDashboardMetrics     `json:"metrics"`
	ReviewRecordings []DoctorDashboardRecording `json:"reviewRecordings"`
	AbilityScores    struct {
		Professionalism float64 `json:"professionalism"`
		Empathy         float64 `json:"empathy"`
		Efficiency      float64 `json:"efficiency"`
		Compliance      float64 `json:"compliance"`
		Rank            int     `json:"rank"`
		Total           int     `json:"total"`
	} `json:"abilityScores"`
}

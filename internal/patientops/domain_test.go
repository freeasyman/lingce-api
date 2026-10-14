package patientops

import "testing"

func TestTaskTypesAndResultsAreStable(t *testing.T) {
	if len(TaskTypes) != 10 {
		t.Fatalf("task type count = %d, want 10", len(TaskTypes))
	}
	if len(TaskResults) != 10 {
		t.Fatalf("task result count = %d, want 10", len(TaskResults))
	}
	for _, value := range []string{TaskStatusPendingConfirmation, TaskStatusPending, TaskStatusProcessing, TaskStatusWaitingPatient, TaskStatusWaitingCollab, TaskStatusCompleted, TaskStatusRescheduled, TaskStatusCancelled, TaskStatusPaused} {
		if value == "" {
			t.Fatal("task status must not be empty")
		}
	}
}

func TestDomainObjectsKeepSourceAndCustomerTraceability(t *testing.T) {
	customerID := int64(42)
	origin := SourceOrigin{SourceType: SourceScreenshot, SourceID: 7, Evidence: "患者询问价格"}
	record := CommunicationRecord{ID: 1, CustomerID: &customerID, Type: RecordScreenshot, Origin: &origin, Status: "confirmed"}
	insight := Insight{ID: 2, CustomerID: &customerID, Type: InsightConcern, Origin: origin, Status: "pending"}
	task := Task{ID: 3, Type: TaskTypes[2], CustomerID: customerID, Origin: origin, Reason: "需要继续联系", OperationalGoal: "推进咨询", Action: "发送方案说明", Priority: "normal", Status: TaskStatusPendingConfirmation, DedupeKey: "screenshot:7:customer:42:consult"}
	if record.Origin.SourceID != insight.Origin.SourceID || insight.Origin.SourceID != task.Origin.SourceID {
		t.Fatal("record, insight and task must retain the same source")
	}
	if *record.CustomerID != *insight.CustomerID || *record.CustomerID != task.CustomerID {
		t.Fatal("record, insight and task must retain the same customer")
	}
	if insight.Status != "pending" {
		t.Fatal("AI insight must remain pending until employee confirmation")
	}
}

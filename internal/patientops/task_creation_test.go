package patientops

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

type taskWriteProbe struct {
	calls int
	err   error
}

func (p *taskWriteProbe) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	p.calls++
	return pgconn.NewCommandTag("INSERT 0 1"), p.err
}

func TestFormalTaskRejectsIncompleteWorkBeforeWrite(t *testing.T) {
	due := time.Now()
	valid := patientTaskInput{TenantID: 1, CustomerID: 2, EmployeeID: 3, TaskType: "咨询推进", Goal: "确认患者意愿", Action: "发送方案差异", Content: "发送已确认的方案差异", DueAt: &due}
	for _, name := range []string{"owner", "time", "action", "type"} {
		t.Run(name, func(t *testing.T) {
			input := valid
			switch name {
			case "owner":
				input.EmployeeID = 0
			case "time":
				input.DueAt = nil
			case "action":
				input.Action = " "
			case "type":
				input.TaskType = "截图"
			}
			probe := &taskWriteProbe{}
			if err := insertPatientTask(context.Background(), probe, input); err == nil {
				t.Fatal("incomplete work must not create formal task")
			}
			if probe.calls != 0 {
				t.Fatal("invalid task reached database")
			}
		})
	}
}

func TestFormalTaskPropagatesPersistenceFailure(t *testing.T) {
	due := time.Now()
	failure := errors.New("database unavailable")
	probe := &taskWriteProbe{err: failure}
	err := insertPatientTask(context.Background(), probe, patientTaskInput{TenantID: 1, CustomerID: 2, EmployeeID: 3, TaskType: "员工承诺跟进", Goal: "兑现承诺", Action: "发送资料", Content: "发送资料", DueAt: &due})
	if !errors.Is(err, failure) {
		t.Fatalf("lost write failure: %v", err)
	}
}

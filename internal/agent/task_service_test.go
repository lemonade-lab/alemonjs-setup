package agent

import (
	"context"
	"testing"
	"time"
)

func TestTaskServiceWait(t *testing.T) {
	m := NewTaskManager(NewTaskStoreAt(t.TempDir()))
	task, err := m.Create(AgentTask{ID: "wait", Status: TaskQueued}, func(context.Context, AgentTask, func(Event)) (string, error) { return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	service := &TaskService{Manager: m}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := service.Wait(ctx, task.ID)
	if err != nil || result.Status != TaskCompleted {
		t.Fatalf("wait result: %+v %v", result, err)
	}
}

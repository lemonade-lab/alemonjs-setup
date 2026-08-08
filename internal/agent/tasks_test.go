package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStoreCheckpointAndEventReplay(t *testing.T) {
	store := NewTaskStoreAt(t.TempDir())
	task := AgentTask{ID: "t1", Status: TaskQueued, Created: time.Now(), Updated: time.Now()}
	if err := store.SaveTask(task); err != nil {
		t.Fatal(err)
	}
	cp := AgentCheckpoint{TaskID: "t1", Version: 1, Messages: []Message{{Role: "user", Content: "继续"}}, Status: TaskRunning}
	if err := store.SaveCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadCheckpoint("t1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Messages[0].Content != "继续" {
		t.Fatalf("checkpoint 内容错误：%+v", loaded)
	}
	if err := store.AppendEvent("t1", TaskEvent{ID: 1, TaskID: "t1", Event: Event{Type: "turn", Turn: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent("t1", TaskEvent{ID: 2, TaskID: "t1", Event: Event{Type: "done"}}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadEvents("t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || string(events[0]) == "" {
		t.Fatalf("事件重放错误：%s", events)
	}
	if _, err := os.Stat(filepath.Join(store.TasksDir(), "t1", "checkpoint.json")); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTaskPlan(t *testing.T) {
	plan := TaskPlan{Goal: "g", Completion: "c", CurrentStep: 0, Steps: []PlanStep{{ID: "a", Status: "pending"}}}
	if err := ValidateTaskPlan(plan); err != nil {
		t.Fatal(err)
	}
	plan.Steps[0].Status = "unknown"
	if err := ValidateTaskPlan(plan); err == nil {
		t.Fatal("expected invalid status")
	}
}

func TestStepExecutorAdvancesOnlyCompletedSteps(t *testing.T) {
	store := NewTaskStoreAt(t.TempDir())
	m := NewTaskManager(store)
	plan := TaskPlan{Goal: "g", Completion: "c", CurrentStep: 0, Approved: true, Steps: []PlanStep{{ID: "a", Status: "pending"}, {ID: "b", Status: "pending"}}}
	_, err := m.Create(AgentTask{ID: "step-exec", Status: TaskQueued, Plan: plan}, func(context.Context, AgentTask, func(Event)) (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	e := StepExecutor{Manager: m}
	if _, err := e.Advance("step-exec"); err == nil {
		t.Fatal("未完成步骤不应推进")
	}
	if _, err := e.StartCurrent("step-exec"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Complete("step-exec", "ok"); err == nil {
		t.Fatal("未进入验证状态不应完成")
	}
	if _, err := e.MarkVerifying("step-exec", "checking"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Complete("step-exec", "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Advance("step-exec"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Current("step-exec")
	if got.ID != "b" {
		t.Fatalf("current step = %q", got.ID)
	}
}

func TestPlanPendingRequiresApproval(t *testing.T) {
	manager := NewTaskManager(NewTaskStoreAt(t.TempDir()))
	plan := TaskPlan{Goal: "g", Completion: "c", CurrentStep: 0, Steps: []PlanStep{{ID: "a", Status: "pending"}}}
	task, err := manager.Create(AgentTask{ID: "pending", Status: TaskPlanPending, Plan: plan}, func(context.Context, AgentTask, func(Event)) (string, error) { return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(task.ID); err == nil {
		t.Fatal("expected approval requirement")
	}
	if _, err := manager.ApprovePlan(task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUpdatePlanKeepsExecutionReady(t *testing.T) {
	manager := NewTaskManager(NewTaskStoreAt(t.TempDir()))
	plan := TaskPlan{Goal: "g", Completion: "c", CurrentStep: 0, Steps: []PlanStep{{ID: "a", Status: "pending"}}}
	task, err := manager.Create(AgentTask{ID: "edit-plan", Status: TaskPlanPending, Plan: plan}, func(context.Context, AgentTask, func(Event)) (string, error) { return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.UpdatePlan(task.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Plan.Approved {
		t.Fatal("编辑计划不应撤销任务的执行就绪状态")
	}
	if updated.Status != TaskQueued {
		t.Fatalf("编辑待批准计划后状态 = %q，期望 queued", updated.Status)
	}
}

func TestSnapshotRollbackDetectsExternalChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewSnapshotStoreAt(filepath.Join(t.TempDir(), "snapshots"))
	snap, err := store.Capture("t1", root, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("agent"), 0600); err != nil {
		t.Fatal(err)
	}
	snap.AfterHash = HashBytes([]byte("agent"))
	if err := store.Save("t1", []FileSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("human"), 0600); err != nil {
		t.Fatal(err)
	}
	if conflicts, err := store.Rollback("t1", root, false); err == nil || len(conflicts) != 1 {
		t.Fatalf("应检测外部修改，conflicts=%v err=%v", conflicts, err)
	}
	if _, err := store.Rollback("t1", root, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "before" {
		t.Fatalf("强制回滚内容错误：%q", data)
	}
}

func TestTaskManagerCancel(t *testing.T) {
	manager := NewTaskManager(NewTaskStoreAt(t.TempDir()))
	task, err := manager.Create(AgentTask{ID: "t1"}, func(ctx context.Context, task AgentTask, emit func(Event)) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, _ := manager.Get(task.ID)
		if current.Status == TaskCancelled {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("任务未进入 cancelled 状态")
}

func TestTaskManagerSubscribeWakesAfterPersistedEventAndStatus(t *testing.T) {
	manager := NewTaskManager(NewTaskStoreAt(t.TempDir()))
	task, err := manager.Create(AgentTask{ID: "subscribe", Status: TaskQueued}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wake, unsubscribe, err := manager.Subscribe(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	manager.EmitExternal(task.ID, Event{Type: "turn", Text: "first"})
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("persisted event did not wake subscriber")
	}
	events, err := manager.Events(task.ID, 0)
	if err != nil || len(events) != 1 || events[0].Text != "first" {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if err := manager.SetStatus(task.ID, TaskCancelled, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("status change did not wake subscriber")
	}
}

func TestTaskManagerRejectsCompletedRestart(t *testing.T) {
	manager := NewTaskManager(NewTaskStoreAt(t.TempDir()))
	task, err := manager.Create(AgentTask{ID: "done", Status: TaskCompleted}, func(context.Context, AgentTask, func(Event)) (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(task.ID); err == nil {
		t.Fatal("completed 任务不应重新启动")
	}
}

func TestTaskManagerIdempotencyKeyReturnsExistingTask(t *testing.T) {
	manager := NewTaskManager(NewTaskStoreAt(t.TempDir()))
	first, err := manager.Create(AgentTask{ID: "first", IdempotencyKey: "incident:i1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create(AgentTask{ID: "second", IdempotencyKey: "incident:i1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("幂等请求创建了新任务：first=%s second=%s", first.ID, second.ID)
	}
}

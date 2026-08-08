package agent

import (
	"fmt"
	"time"
)

// StepExecutor centralizes the small, durable state machine used by runners.
// It deliberately delegates persistence to TaskManager so every transition is
// visible to observers and survives a process restart.
type StepExecutor struct{ Manager *TaskManager }

func (e StepExecutor) Current(taskID string) (PlanStep, error) {
	if e.Manager == nil {
		return PlanStep{}, fmt.Errorf("任务管理器未初始化")
	}
	t, err := e.Manager.Get(taskID)
	if err != nil {
		return PlanStep{}, err
	}
	if t.Plan.CurrentStep < 0 || t.Plan.CurrentStep >= len(t.Plan.Steps) {
		return PlanStep{}, fmt.Errorf("当前步骤无效")
	}
	return t.Plan.Steps[t.Plan.CurrentStep], nil
}

func (e StepExecutor) StartCurrent(taskID string) (AgentTask, error) {
	s, err := e.Current(taskID)
	if err != nil {
		return AgentTask{}, err
	}
	return e.Manager.MarkStep(taskID, s.ID, "running", "")
}

func (e StepExecutor) MarkVerifying(taskID, result string) (AgentTask, error) {
	s, err := e.Current(taskID)
	if err != nil {
		return AgentTask{}, err
	}
	return e.Manager.MarkStep(taskID, s.ID, "verifying", result)
}

func (e StepExecutor) Complete(taskID, result string) (AgentTask, error) {
	s, err := e.Current(taskID)
	if err != nil {
		return AgentTask{}, err
	}
	return e.Manager.MarkStep(taskID, s.ID, "completed", result)
}

func (e StepExecutor) Fail(taskID, result string) (AgentTask, error) {
	s, err := e.Current(taskID)
	if err != nil {
		return AgentTask{}, err
	}
	return e.Manager.MarkStep(taskID, s.ID, "failed", result)
}

func (e StepExecutor) Advance(taskID string) (AgentTask, error) {
	t, err := e.Manager.Get(taskID)
	if err != nil {
		return AgentTask{}, err
	}
	if t.Plan.CurrentStep >= len(t.Plan.Steps)-1 {
		return t, nil
	}
	if t.Plan.Steps[t.Plan.CurrentStep].Status != "completed" && t.Plan.Steps[t.Plan.CurrentStep].Status != "skipped" {
		return AgentTask{}, fmt.Errorf("当前步骤尚未完成")
	}
	next := t.Plan.CurrentStep + 1
	// UpdatePlan is intentionally user-facing and disallows running tasks;
	// advancing is an internal execution transition, so mutate through the
	// manager's lock and persist directly.
	mng := e.Manager
	mng.mu.Lock()
	defer mng.mu.Unlock()
	m, ok := mng.tasks[taskID]
	if !ok {
		return AgentTask{}, fmt.Errorf("任务不存在")
	}
	m.Task.Plan.CurrentStep = next
	m.Task.Updated = time.Now()
	if err := mng.store.SaveTask(m.Task); err != nil {
		return AgentTask{}, err
	}
	return m.Task, nil
}

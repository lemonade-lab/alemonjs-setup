package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type TaskEvent struct {
	ID     int64  `json:"id"`
	TaskID string `json:"taskId"`
	Event
}

type TaskRunner func(context.Context, AgentTask, func(Event)) (string, error)

type ManagedTask struct {
	Task   AgentTask
	Cancel context.CancelFunc
	Runner TaskRunner
}

type TaskManager struct {
	store     *TaskStore
	mu        sync.Mutex
	tasks     map[string]*ManagedTask
	nextEvent map[string]int64
}

func NewTaskManager(store *TaskStore) *TaskManager {
	return &TaskManager{store: store, tasks: map[string]*ManagedTask{}, nextEvent: map[string]int64{}}
}

func (m *TaskManager) Create(task AgentTask, runner TaskRunner) (AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task.ID == "" {
		task.ID = fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	if task.Status == "" {
		task.Status = TaskQueued
	}
	if task.Created.IsZero() {
		task.Created = time.Now()
	}
	task.Updated = time.Now()
	if len(task.Plan.Steps) > 0 {
		if err := ValidateTaskPlan(task.Plan); err != nil {
			return task, err
		}
	}
	if _, exists := m.tasks[task.ID]; exists {
		return task, errors.New("任务已存在")
	}
	m.tasks[task.ID] = &ManagedTask{Task: task, Runner: runner}
	if err := m.store.SaveTask(task); err != nil {
		delete(m.tasks, task.ID)
		return task, err
	}
	return task, nil
}

func (m *TaskManager) Start(id string) error {
	m.mu.Lock()
	managed, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("任务不存在")
	}
	if managed.Task.Status == TaskRunning {
		m.mu.Unlock()
		return errors.New("任务正在运行")
	}
	if managed.Task.Status != TaskQueued && managed.Task.Status != TaskPaused {
		m.mu.Unlock()
		return fmt.Errorf("任务状态 %q 不允许启动", managed.Task.Status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	managed.Cancel = cancel
	managed.Task.Status = TaskRunning
	managed.Task.Updated = time.Now()
	task := managed.Task
	_ = m.store.SaveTask(task)
	m.mu.Unlock()
	go m.run(ctx, id, task, managed.Runner)
	return nil
}

func (m *TaskManager) Resume(id string, runner TaskRunner) error {
	m.mu.Lock()
	managed, ok := m.tasks[id]
	if !ok {
		loaded, err := m.store.LoadTask(id)
		if err != nil {
			m.mu.Unlock()
			return errors.New("任务不存在")
		}
		managed = &ManagedTask{Task: loaded}
		m.tasks[id] = managed
	}
	if managed.Task.Status == TaskRunning {
		m.mu.Unlock()
		return errors.New("任务正在运行")
	}
	managed.Runner = runner
	managed.Task.Status = TaskQueued
	managed.Task.LastError = ""
	managed.Task.Updated = time.Now()
	_ = m.store.SaveTask(managed.Task)
	m.mu.Unlock()
	return m.Start(id)
}

func (m *TaskManager) SetStatus(id string, status TaskStatus, lastError string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	managed, ok := m.tasks[id]
	if !ok {
		loaded, err := m.store.LoadTask(id)
		if err != nil {
			return errors.New("任务不存在")
		}
		managed = &ManagedTask{Task: loaded}
		m.tasks[id] = managed
	}
	managed.Task.Status = status
	managed.Task.LastError = lastError
	managed.Task.Updated = time.Now()
	return m.store.SaveTask(managed.Task)
}

func (m *TaskManager) UpdatePlan(id string, plan TaskPlan) (AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	managed, ok := m.tasks[id]
	if !ok {
		loaded, err := m.store.LoadTask(id)
		if err != nil {
			return AgentTask{}, errors.New("任务不存在")
		}
		managed = &ManagedTask{Task: loaded}
		m.tasks[id] = managed
	}
	if err := ValidateTaskPlan(plan); err != nil {
		return AgentTask{}, err
	}
	managed.Task.Plan = plan
	managed.Task.Updated = time.Now()
	if err := m.store.SaveTask(managed.Task); err != nil {
		return AgentTask{}, err
	}
	return managed.Task, nil
}

func (m *TaskManager) MarkStep(id, stepID, status, result string) (AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	managed, ok := m.tasks[id]
	if !ok {
		loaded, err := m.store.LoadTask(id)
		if err != nil {
			return AgentTask{}, errors.New("任务不存在")
		}
		managed = &ManagedTask{Task: loaded}
		m.tasks[id] = managed
	}
	for index := range managed.Task.Plan.Steps {
		step := &managed.Task.Plan.Steps[index]
		if step.ID != stepID {
			continue
		}
		if status == "running" && step.Status == "completed" {
			return AgentTask{}, errors.New("已完成步骤不能重新运行")
		}
		if status == "pending" && step.Status != "failed" && step.Status != "verifying" {
			return AgentTask{}, errors.New("只有失败或验证中的步骤可以重试")
		}
		step.Status, step.Result = status, result
		if status == "running" {
			step.Attempts++
			managed.Task.Plan.CurrentStep = index
		}
		managed.Task.Updated = time.Now()
		if err := m.store.SaveTask(managed.Task); err != nil {
			return AgentTask{}, err
		}
		return managed.Task, nil
	}
	return AgentTask{}, errors.New("步骤不存在")
}

func (m *TaskManager) run(ctx context.Context, id string, task AgentTask, runner TaskRunner) {
	answer, err := runner(ctx, task, func(event Event) { m.emit(id, event) })
	m.mu.Lock()
	managed, ok := m.tasks[id]
	if ok {
		if errors.Is(ctx.Err(), context.Canceled) {
			managed.Task.Status = TaskCancelled
		} else if err != nil {
			managed.Task.Status = TaskFailed
			managed.Task.LastError = err.Error()
		} else {
			managed.Task.Status = TaskCompleted
		}
		managed.Task.Updated = time.Now()
		_ = m.store.SaveTask(managed.Task)
	}
	m.mu.Unlock()
	_ = answer // the runner emits the final Agent event; status is persisted here.
}

func (m *TaskManager) emit(id string, event Event) {
	m.mu.Lock()
	if m.nextEvent[id] == 0 {
		m.nextEvent[id] = m.store.LastEventID(id)
	}
	m.nextEvent[id]++
	envelope := TaskEvent{ID: m.nextEvent[id], TaskID: id, Event: event}
	if managed, ok := m.tasks[id]; ok && event.Turn > 0 {
		managed.Task.Turn = event.Turn
		managed.Task.Updated = time.Now()
		_ = m.store.SaveTask(managed.Task)
	}
	m.mu.Unlock()
	_ = m.store.AppendEvent(id, envelope)
}

func (m *TaskManager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	managed, ok := m.tasks[id]
	if !ok {
		return errors.New("任务不存在")
	}
	if managed.Cancel == nil {
		return errors.New("任务尚未运行")
	}
	managed.Cancel()
	return nil
}

func (m *TaskManager) Get(id string) (AgentTask, error) {
	m.mu.Lock()
	managed, ok := m.tasks[id]
	m.mu.Unlock()
	if ok {
		return managed.Task, nil
	}
	return m.store.LoadTask(id)
}

func (m *TaskManager) List() []AgentTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	out := make([]AgentTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		out = append(out, task.Task)
		seen[task.Task.ID] = true
	}
	if persisted, err := m.store.ListTasks(); err == nil {
		for _, task := range persisted {
			if !seen[task.ID] {
				out = append(out, task)
			}
		}
	}
	return out
}

func (m *TaskManager) Events(id string, after int64) ([]TaskEvent, error) {
	raw, err := m.store.ReadEvents(id, after)
	if err != nil {
		return nil, err
	}
	var events []TaskEvent
	for _, line := range raw {
		var event TaskEvent
		if json.Unmarshal(line, &event) == nil {
			events = append(events, event)
		}
	}
	return events, nil
}

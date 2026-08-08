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
type TaskObserver func(previous AgentTask, current AgentTask)

type ManagedTask struct {
	Task   AgentTask
	Cancel context.CancelFunc
	Runner TaskRunner
}

type TaskManager struct {
	store       *TaskStore
	mu          sync.Mutex
	tasks       map[string]*ManagedTask
	nextEvent   map[string]int64
	subscribers map[string]map[chan struct{}]struct{}
	observer    TaskObserver
}

func (m *TaskManager) SetObserver(observer TaskObserver) {
	m.mu.Lock()
	m.observer = observer
	m.mu.Unlock()
}

func NewTaskManager(store *TaskStore) *TaskManager {
	return &TaskManager{store: store, tasks: map[string]*ManagedTask{}, nextEvent: map[string]int64{}, subscribers: map[string]map[chan struct{}]struct{}{}}
}

// Subscribe wakes a consumer after a task event or state change is persisted.
// Consumers must still read Events/Get after each wake: the durable store is
// the source of truth across reconnects and missed in-memory notifications.
func (m *TaskManager) Subscribe(id string) (<-chan struct{}, func(), error) {
	if _, err := m.Get(id); err != nil {
		return nil, nil, err
	}
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	if m.subscribers == nil {
		m.subscribers = map[string]map[chan struct{}]struct{}{}
	}
	if m.subscribers[id] == nil {
		m.subscribers[id] = map[chan struct{}]struct{}{}
	}
	m.subscribers[id][ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subscribers[id], ch)
		if len(m.subscribers[id]) == 0 {
			delete(m.subscribers, id)
		}
		m.mu.Unlock()
	}, nil
}

func (m *TaskManager) notifyLocked(id string) {
	for ch := range m.subscribers[id] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
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
	if task.IdempotencyKey != "" {
		for _, existing := range m.tasks {
			if existing.Task.IdempotencyKey == task.IdempotencyKey {
				return existing.Task, nil
			}
		}
		if persisted, err := m.store.ListTasks(); err == nil {
			for _, existing := range persisted {
				if existing.IdempotencyKey == task.IdempotencyKey {
					m.tasks[existing.ID] = &ManagedTask{Task: existing}
					return existing, nil
				}
			}
		}
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
	if len(managed.Task.Plan.Steps) > 0 && !managed.Task.Plan.Approved {
		managed.Task.Status = TaskPlanPending
		_ = m.store.SaveTask(managed.Task)
		m.mu.Unlock()
		return errors.New("任务计划尚未批准")
	}
	if managed.Task.Status != TaskQueued && managed.Task.Status != TaskPaused {
		m.mu.Unlock()
		return fmt.Errorf("任务状态 %q 不允许启动", managed.Task.Status)
	}
	if managed.Runner == nil {
		m.mu.Unlock()
		return errors.New("任务执行器未恢复，请重新提交或恢复任务")
	}
	ctx, cancel := context.WithCancel(context.Background())
	managed.Cancel = cancel
	managed.Task.Status = TaskRunning
	managed.Task.Updated = time.Now()
	task := managed.Task
	saved := m.store.SaveTask(task)
	if saved == nil {
		m.notifyLocked(id)
	}
	m.mu.Unlock()
	if saved != nil {
		return saved
	}
	go m.run(ctx, id, task, managed.Runner)
	return nil
}

func (m *TaskManager) ApprovePlan(id string) (AgentTask, error) {
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
	if err := ValidateTaskPlan(managed.Task.Plan); err != nil {
		return AgentTask{}, err
	}
	if managed.Task.Status == TaskCompleted || managed.Task.Status == TaskRolledBack {
		return AgentTask{}, errors.New("任务已结束")
	}
	managed.Task.Plan.Approved = true
	if managed.Task.Status == TaskPlanPending || managed.Task.Status == TaskPaused {
		managed.Task.Status = TaskQueued
	}
	managed.Task.Updated = time.Now()
	if err := m.store.SaveTask(managed.Task); err != nil {
		return AgentTask{}, err
	}
	m.notifyLocked(id)
	return managed.Task, nil
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
	saved := m.store.SaveTask(managed.Task)
	if saved == nil {
		m.notifyLocked(id)
	}
	m.mu.Unlock()
	if saved != nil {
		return saved
	}
	return m.Start(id)
}

func (m *TaskManager) SetStatus(id string, status TaskStatus, lastError string) error {
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
	if !validTaskTransition(managed.Task.Status, status) {
		m.mu.Unlock()
		return fmt.Errorf("任务状态不能从 %q 变为 %q", managed.Task.Status, status)
	}
	previous := managed.Task
	managed.Task.Status = status
	managed.Task.LastError = lastError
	managed.Task.Updated = time.Now()
	err := m.store.SaveTask(managed.Task)
	if err == nil {
		m.notifyLocked(id)
	}
	current, observer := managed.Task, m.observer
	m.mu.Unlock()
	if err == nil && observer != nil {
		observer(previous, current)
	}
	return err
}

func validTaskTransition(from, to TaskStatus) bool {
	if from == to {
		return true
	}
	if to == TaskCancelled || to == TaskPaused {
		return from == TaskRunning || from == TaskQueued || from == TaskPlanPending
	}
	switch from {
	case "":
		return to == TaskQueued || to == TaskPlanPending
	case TaskPlanPending:
		return to == TaskQueued
	case TaskQueued:
		return to == TaskRunning || to == TaskPlanPending
	case TaskRunning:
		return to == TaskFailed || to == TaskCompleted
	case TaskFailed:
		return to == TaskQueued || to == TaskPlanPending
	case TaskPaused:
		return to == TaskQueued || to == TaskPlanPending
	}
	return false
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
	if managed.Task.Status == TaskRunning || managed.Task.Status == TaskCompleted || managed.Task.Status == TaskRolledBack {
		return AgentTask{}, errors.New("当前任务状态不允许编辑计划")
	}
	for i := range plan.Steps {
		if plan.Steps[i].Status == "" {
			plan.Steps[i].Status = "pending"
		}
		if plan.Steps[i].Status == "running" {
			if i != plan.CurrentStep {
				return AgentTask{}, errors.New("运行中步骤必须是当前步骤")
			}
		}
	}
	if err := ValidateTaskPlan(plan); err != nil {
		return AgentTask{}, err
	}
	// A changed plan affects execution intent, not tool authorization. Risky
	// tools are still gated by the task's approver, so editing a paused/pending
	// plan must not introduce a second, unrelated approval checkpoint.
	plan.Approved = true
	managed.Task.Plan = plan
	if managed.Task.Status == TaskPlanPending {
		managed.Task.Status = TaskQueued
	}
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
		if status == "completed" && step.Status != "verifying" && step.Status != "completed" {
			return AgentTask{}, errors.New("步骤必须先验证")
		}
		if status == "verifying" && step.Status != "running" && step.Status != "verifying" {
			return AgentTask{}, errors.New("只有运行中的步骤可以进入验证")
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
	var previous, current AgentTask
	var observer TaskObserver
	if ok {
		previous = managed.Task
		if errors.Is(ctx.Err(), context.Canceled) && managed.Task.Status != TaskPaused {
			managed.Task.Status = TaskCancelled
		} else if err != nil {
			if IsRecoverable(err) {
				managed.Task.Status = TaskPaused
			} else {
				managed.Task.Status = TaskFailed
			}
			managed.Task.LastError = err.Error()
		} else {
			complete := true
			for _, step := range managed.Task.Plan.Steps {
				if step.Status != "completed" && step.Status != "skipped" {
					complete = false
					break
				}
			}
			if len(managed.Task.Plan.Steps) > 0 && !complete {
				managed.Task.Status = TaskFailed
				managed.Task.LastError = "计划仍有未完成步骤"
			} else {
				managed.Task.Status = TaskCompleted
			}
		}
		managed.Task.Updated = time.Now()
		if m.store.SaveTask(managed.Task) == nil {
			m.notifyLocked(id)
		}
		current = managed.Task
		observer = m.observer
	}
	m.mu.Unlock()
	if observer != nil && ok {
		observer(previous, current)
	}
	_ = answer // the runner emits the final Agent event; status is persisted here.
}

func (m *TaskManager) emit(id string, event Event) {
	m.mu.Lock()
	if m.nextEvent[id] == 0 {
		m.nextEvent[id] = m.store.LastEventID(id)
	}
	if managed, ok := m.tasks[id]; ok {
		event.TaskID = id
		if managed.Task.Plan.CurrentStep >= 0 && managed.Task.Plan.CurrentStep < len(managed.Task.Plan.Steps) {
			event.StepID = managed.Task.Plan.Steps[managed.Task.Plan.CurrentStep].ID
		}
	}
	m.nextEvent[id]++
	envelope := TaskEvent{ID: m.nextEvent[id], TaskID: id, Event: event}
	if managed, ok := m.tasks[id]; ok && event.Turn > 0 {
		managed.Task.Turn = event.Turn
		managed.Task.Updated = time.Now()
		_ = m.store.SaveTask(managed.Task)
	}
	m.mu.Unlock()
	if m.store.AppendEvent(id, envelope) == nil {
		m.mu.Lock()
		m.notifyLocked(id)
		m.mu.Unlock()
	}
}

func (m *TaskManager) EmitExternal(id string, event Event) { m.emit(id, event) }

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
	if ok {
		task := managed.Task
		m.mu.Unlock()
		return task, nil
	}
	m.mu.Unlock()
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

// PauseRunning preserves checkpoints while stopping active runners during a
// graceful server shutdown.
func (m *TaskManager) PauseRunning() error {
	m.mu.Lock()
	ids := make([]string, 0)
	for id, managed := range m.tasks {
		if managed.Task.Status == TaskRunning {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.mu.Lock()
		managed := m.tasks[id]
		if managed != nil && managed.Task.Status == TaskRunning {
			if managed.Cancel != nil {
				managed.Cancel()
			}
			managed.Task.Status = TaskPaused
			managed.Task.Updated = time.Now()
			if err := m.store.SaveTask(managed.Task); err != nil {
				m.mu.Unlock()
				return err
			}
		}
		m.mu.Unlock()
	}
	return nil
}

// ReconcileStartup converts persisted running tasks into paused tasks. The
// process cannot safely resume a model/tool call without renewed permission.
func (m *TaskManager) ReconcileStartup() error {
	tasks, err := m.store.ListTasks()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Status != TaskRunning {
			continue
		}
		task.Status, task.LastError, task.Updated = TaskPaused, "服务重启后等待显式恢复", time.Now()
		if err := m.store.SaveTask(task); err != nil {
			return err
		}
		m.mu.Lock()
		if managed, ok := m.tasks[task.ID]; ok {
			managed.Task = task
			managed.Cancel = nil
		}
		m.mu.Unlock()
	}
	return nil
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

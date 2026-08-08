package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TaskStatus is the durable lifecycle state of a background Agent task.
type TaskStatus string

const (
	TaskQueued      TaskStatus = "queued"
	TaskPlanPending TaskStatus = "plan_pending"
	TaskRunning     TaskStatus = "running"
	TaskPaused      TaskStatus = "paused"
	TaskFailed      TaskStatus = "failed"
	TaskCompleted   TaskStatus = "completed"
	TaskCancelled   TaskStatus = "cancelled"
	TaskRolledBack  TaskStatus = "rolled_back"
)

type AgentTask struct {
	ID             string      `json:"id"`
	IdempotencyKey string      `json:"idempotencyKey,omitempty"`
	SessionID      string      `json:"sessionId"`
	GoalID         string      `json:"goalId,omitempty"`
	Root           string      `json:"root"`
	Provider       string      `json:"provider"`
	Model          string      `json:"model"`
	Access         string      `json:"access"`
	Status         TaskStatus  `json:"status"`
	Turn           int         `json:"turn"`
	LastError      string      `json:"lastError,omitempty"`
	Created        time.Time   `json:"created"`
	Updated        time.Time   `json:"updated"`
	Plan           TaskPlan    `json:"plan"`
	Isolation      string      `json:"isolation,omitempty"`
	WorktreeRoot   string      `json:"worktreeRoot,omitempty"`
	Report         *TaskReport `json:"report,omitempty"`
}

type TaskPlan struct {
	Goal        string     `json:"goal"`
	Completion  string     `json:"completion"`
	Steps       []PlanStep `json:"steps"`
	CurrentStep int        `json:"currentStep"`
	Approved    bool       `json:"approved"`
}

type PlanStep struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	Result      string `json:"result,omitempty"`
}

var validPlanStatuses = map[string]bool{"pending": true, "running": true, "verifying": true, "completed": true, "failed": true, "skipped": true}

func ValidateTaskPlan(plan TaskPlan) error {
	if strings.TrimSpace(plan.Goal) == "" || strings.TrimSpace(plan.Completion) == "" {
		return errors.New("计划必须包含目标和完成条件")
	}
	if len(plan.Steps) == 0 {
		return errors.New("计划至少需要一个步骤")
	}
	if plan.CurrentStep < 0 || plan.CurrentStep >= len(plan.Steps) {
		return errors.New("当前步骤无效")
	}
	seen := map[string]bool{}
	for _, step := range plan.Steps {
		if step.ID == "" || seen[step.ID] {
			return errors.New("步骤 ID 必须唯一且非空")
		}
		if !validPlanStatuses[step.Status] {
			return fmt.Errorf("步骤状态无效：%s", step.Status)
		}
		seen[step.ID] = true
	}
	return nil
}

type AgentCheckpoint struct {
	Version      int          `json:"version"`
	TaskID       string       `json:"taskId"`
	SessionID    string       `json:"sessionId"`
	Root         string       `json:"root"`
	Provider     string       `json:"provider"`
	Model        string       `json:"model"`
	Messages     []Message    `json:"messages"`
	SystemPrompt string       `json:"systemPrompt"`
	Turn         int          `json:"turn"`
	Status       TaskStatus   `json:"status"`
	LastError    string       `json:"lastError,omitempty"`
	Updated      time.Time    `json:"updated"`
	Plan         TaskPlan     `json:"plan"`
	LastAction   string       `json:"lastAction,omitempty"`
	IndexVersion string       `json:"indexVersion,omitempty"`
	WorktreeRoot string       `json:"worktreeRoot,omitempty"`
	Report       *TaskReport  `json:"report,omitempty"`
	Context      ContextState `json:"context,omitempty"`
}

type ContextState struct {
	Goal          string   `json:"goal,omitempty"`
	Constraints   []string `json:"constraints,omitempty"`
	Decisions     []string `json:"decisions,omitempty"`
	ModifiedFiles []string `json:"modifiedFiles,omitempty"`
	Validation    []string `json:"validation,omitempty"`
	Failures      []string `json:"failures,omitempty"`
	Summary       string   `json:"summary,omitempty"`
}

type TaskReport struct {
	Goal           string       `json:"goal"`
	Plan           TaskPlan     `json:"plan"`
	ModifiedFiles  []string     `json:"modifiedFiles,omitempty"`
	Validation     []string     `json:"validation,omitempty"`
	Diff           string       `json:"diff,omitempty"`
	Reviewer       ReviewResult `json:"reviewer"`
	Summary        string       `json:"summary"`
	RollbackTaskID string       `json:"rollbackTaskId,omitempty"`
	GeneratedAt    time.Time    `json:"generatedAt"`
}

// TaskStore is a small atomic on-disk store. It deliberately stores no API
// credentials; callers provide those again when resolving a provider.
type TaskStore struct {
	dir string
	mu  sync.Mutex
}

func NewTaskStore() (*TaskStore, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &TaskStore{dir: filepath.Join(config, "alemonjs", "alx-agent", "tasks")}, nil
}

func NewTaskStoreAt(dir string) *TaskStore     { return &TaskStore{dir: dir} }
func (s *TaskStore) TasksDir() string          { return s.dir }
func (s *TaskStore) taskDir(id string) string  { return filepath.Join(s.dir, id) }
func (s *TaskStore) taskPath(id string) string { return filepath.Join(s.taskDir(id), "task.json") }
func (s *TaskStore) checkpointPath(id string) string {
	return filepath.Join(s.taskDir(id), "checkpoint.json")
}
func (s *TaskStore) reportPath(id string) string { return filepath.Join(s.taskDir(id), "report.json") }
func (s *TaskStore) eventsPath(id string) string { return filepath.Join(s.taskDir(id), "events.jsonl") }

func (s *TaskStore) SaveTask(task AgentTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.atomicJSON(s.taskPath(task.ID), task)
}

func (s *TaskStore) LoadTask(id string) (AgentTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var task AgentTask
	if err := readJSON(s.taskPath(id), &task); err != nil {
		return task, err
	}
	return task, nil
}

func (s *TaskStore) ListTasks() ([]AgentTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []AgentTask
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var task AgentTask
		if err := readJSONFile(filepath.Join(s.dir, entry.Name(), "task.json"), &task); err == nil {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (s *TaskStore) SaveCheckpoint(cp AgentCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cp.Version == 0 {
		cp.Version = 1
	}
	return s.atomicJSON(s.checkpointPath(cp.TaskID), cp)
}

func (s *TaskStore) LoadCheckpoint(id string) (AgentCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cp AgentCheckpoint
	if err := readJSON(s.checkpointPath(id), &cp); err != nil {
		return cp, err
	}
	if cp.Version > 1 {
		return cp, fmt.Errorf("不支持的 checkpoint 版本：%d", cp.Version)
	}
	if cp.Version == 0 {
		cp.Version = 1
		if cp.Plan.CurrentStep < 0 {
			cp.Plan.CurrentStep = 0
		}
	}
	return cp, nil
}

func (s *TaskStore) SaveReport(report TaskReport, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.atomicJSON(s.reportPath(taskID), report)
}

func (s *TaskStore) LoadReport(taskID string) (TaskReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var report TaskReport
	err := readJSONFile(s.reportPath(taskID), &report)
	return report, err
}

func (s *TaskStore) AppendEvent(id string, event any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.taskDir(id), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.eventsPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(raw, '\n'))
	return err
}

func (s *TaskStore) ReadEvents(id string, after int64) ([]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.eventsPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var out []json.RawMessage
	for i, line := range lines {
		if int64(i+1) > after {
			if strings.TrimSpace(line) != "" {
				out = append(out, json.RawMessage(line))
			}
		}
	}
	return out, nil
}

func (s *TaskStore) LastEventID(id string) int64 {
	raw, err := os.ReadFile(s.eventsPath(id))
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var last int64
	for _, line := range lines {
		var event TaskEvent
		if json.Unmarshal([]byte(line), &event) == nil && event.ID > last {
			last = event.ID
		}
	}
	return last
}

func (s *TaskStore) atomicJSON(path string, value any) error {
	return atomicJSONFile(path, value)
}

func atomicJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, target any) error {
	return readJSONFile(path, target)
}

func readJSONFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("读取 %s 失败：%w", filepath.Base(path), err)
	}
	return nil
}

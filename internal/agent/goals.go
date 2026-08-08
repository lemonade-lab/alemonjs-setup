package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GoalStatus string

const (
	GoalActive    GoalStatus = "active"
	GoalPaused    GoalStatus = "paused"
	GoalCompleted GoalStatus = "completed"
)

type Goal struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Prompt          string     `json:"prompt"`
	Root            string     `json:"root"`
	Provider        string     `json:"provider"`
	Model           string     `json:"model"`
	Access          string     `json:"access"`
	Isolation       string     `json:"isolation,omitempty"`
	ScheduleMinutes int        `json:"scheduleMinutes,omitempty"`
	NextRun         time.Time  `json:"nextRun,omitempty"`
	Status          GoalStatus `json:"status"`
	LastTaskID      string     `json:"lastTaskId,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	Updated         time.Time  `json:"updated"`
}

type GoalRun struct {
	ID         string     `json:"id"`
	GoalID     string     `json:"goalId"`
	TaskID     string     `json:"taskId"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type GoalStore struct {
	dir string
	mu  sync.Mutex
}

func NewGoalStoreAt(dir string) *GoalStore { return &GoalStore{dir: dir} }
func (s *GoalStore) path(id string) string { return filepath.Join(s.dir, id+".json") }
func (s *GoalStore) Save(goal Goal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	return atomicJSONFile(s.path(goal.ID), goal)
}
func (s *GoalStore) Get(id string) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var goal Goal
	err := readJSONFile(s.path(id), &goal)
	return goal, err
}
func (s *GoalStore) List() ([]Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Goal{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var goal Goal
		if readJSONFile(filepath.Join(s.dir, entry.Name()), &goal) == nil {
			out = append(out, goal)
		}
	}
	return out, nil
}
func (s *GoalStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.path(id))
}

func (s *GoalStore) runPath(goalID, runID string) string {
	return filepath.Join(s.dir, goalID+"-run-"+runID+".json")
}
func (s *GoalStore) SaveRun(run GoalRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	return atomicJSONFile(s.runPath(run.GoalID, run.ID), run)
}
func (s *GoalStore) ListRuns(goalID string) ([]GoalRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := []GoalRun{}
	prefix := goalID + "-run-"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		var run GoalRun
		if readJSONFile(filepath.Join(s.dir, entry.Name()), &run) == nil {
			out = append(out, run)
		}
	}
	return out, nil
}

func (s *GoalStore) UpdateRunByTask(taskID, status, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.Contains(entry.Name(), "-run-") {
			continue
		}
		var run GoalRun
		path := filepath.Join(s.dir, entry.Name())
		if readJSONFile(path, &run) != nil || run.TaskID != taskID || (run.Status != "running" && run.Status != "queued") {
			continue
		}
		run.Status, run.Error = status, message
		now := time.Now()
		run.FinishedAt = &now
		if err := atomicJSONFile(path, run); err != nil {
			return err
		}
	}
	return nil
}

func (s *GoalStore) UpdateRun(runID, status, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("运行记录不存在")
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.Contains(entry.Name(), "-run-"+runID) {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		var run GoalRun
		if readJSONFile(path, &run) != nil {
			continue
		}
		run.Status, run.Error = status, message
		if status == "completed" || status == "failed" || status == "cancelled" {
			now := time.Now()
			run.FinishedAt = &now
		}
		return atomicJSONFile(path, run)
	}
	return errors.New("运行记录不存在")
}

func (s *GoalStore) HasRunningRun(goalID string) bool {
	runs, _ := s.ListRuns(goalID)
	for _, run := range runs {
		if run.Status == "running" {
			return true
		}
	}
	return false
}

func (s *GoalStore) ReconcileRuns(tasks []AgentTask) error {
	known := map[string]TaskStatus{}
	for _, task := range tasks {
		known[task.ID] = task.Status
	}
	goals, err := s.List()
	if err != nil {
		return err
	}
	for _, goal := range goals {
		runs, _ := s.ListRuns(goal.ID)
		for _, run := range runs {
			status, ok := known[run.TaskID]
			if run.Status != "running" && run.Status != "queued" {
				continue
			}
			if !ok {
				_ = s.UpdateRun(run.ID, "failed", "服务重启后未找到关联任务")
				continue
			}
			if status == TaskCompleted || status == TaskFailed || status == TaskCancelled {
				_ = s.UpdateRunByTask(run.TaskID, string(status), "")
			}
		}
	}
	return nil
}

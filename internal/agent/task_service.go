package agent

import (
	"context"
	"errors"
	"time"
)

type TaskResult struct {
	TaskID    string
	SessionID string
	Answer    string
	Status    TaskStatus
	Error     string
}
type TaskService struct{ Manager *TaskManager }

func (s *TaskService) Create(task AgentTask, runner TaskRunner) (AgentTask, error) {
	if err := s.Validate(); err != nil {
		return AgentTask{}, err
	}
	return s.Manager.Create(task, runner)
}
func (s *TaskService) Start(id string) error                     { return s.Manager.Start(id) }
func (s *TaskService) Resume(id string, runner TaskRunner) error { return s.Manager.Resume(id, runner) }
func (s *TaskService) Cancel(id string) error                    { return s.Manager.Cancel(id) }
func (s *TaskService) Wait(ctx context.Context, id string) (TaskResult, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := s.Manager.Get(id)
		if err != nil {
			return TaskResult{TaskID: id}, err
		}
		result := TaskResult{TaskID: id, SessionID: task.SessionID, Status: task.Status, Error: task.LastError}
		if events, eventErr := s.Manager.Events(id, 0); eventErr == nil {
			for _, event := range events {
				if event.Type == "done" {
					result.Answer = event.Text
				}
				if event.Type == "error" && result.Error == "" {
					result.Error = event.Text
				}
			}
		}
		if task.Status == TaskCompleted || task.Status == TaskFailed || task.Status == TaskCancelled || task.Status == TaskRolledBack {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}
	}
}
func (s *TaskService) Validate() error {
	if s == nil || s.Manager == nil {
		return errors.New("任务服务未初始化")
	}
	return nil
}

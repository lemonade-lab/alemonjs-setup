package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"alemonx/internal/agent"
)

func (s *server) agentGoalsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		goals, err := s.goalStore.List()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, goals)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "该操作暂不支持。")
		return
	}
	var goal agent.Goal
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&goal); err != nil || strings.TrimSpace(goal.Prompt) == "" || strings.TrimSpace(goal.Root) == "" {
		writeError(w, 400, "目标需要 prompt 和 root。")
		return
	}
	if goal.ID == "" {
		goal.ID = "g" + time.Now().Format("20060102150405.000000000")
	}
	if goal.Status == "" {
		goal.Status = agent.GoalActive
	}
	if goal.ScheduleMinutes > 0 && goal.NextRun.IsZero() {
		goal.NextRun = time.Now().Add(time.Duration(goal.ScheduleMinutes) * time.Minute)
	}
	goal.Updated = time.Now()
	if err := s.goalStore.Save(goal); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, goal)
}

func (s *server) agentGoalHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent/goals/"), "/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if id == "" || strings.ContainsAny(id, `/\\`) {
		writeError(w, 400, "目标 ID 无效。")
		return
	}
	goal, err := s.goalStore.Get(id)
	if err != nil {
		writeError(w, 404, "目标不存在。")
		return
	}
	if r.Method == http.MethodPatch {
		var update agent.Goal
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&update); err != nil {
			writeError(w, 400, "目标格式无效。")
			return
		}
		if strings.TrimSpace(update.Prompt) != "" {
			goal.Prompt = update.Prompt
		}
		if strings.TrimSpace(update.Title) != "" {
			goal.Title = update.Title
		}
		if update.Root != "" {
			goal.Root = update.Root
		}
		if update.Provider != "" {
			goal.Provider = update.Provider
		}
		if update.Model != "" {
			goal.Model = update.Model
		}
		if update.Isolation != "" {
			goal.Isolation = update.Isolation
		}
		if update.ScheduleMinutes < 0 {
			writeError(w, 400, "调度周期无效。")
			return
		}
		goal.ScheduleMinutes = update.ScheduleMinutes
		goal.NextRun = time.Time{}
		if goal.ScheduleMinutes > 0 && goal.Status == agent.GoalActive {
			goal.NextRun = time.Now().Add(time.Duration(goal.ScheduleMinutes) * time.Minute)
		}
		goal.Updated = time.Now()
		if err := s.goalStore.Save(goal); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, goal)
		return
	}
	if r.Method == http.MethodGet {
		if action == "runs" {
			runs, runErr := s.goalStore.ListRuns(id)
			if runErr != nil {
				writeError(w, 500, runErr.Error())
				return
			}
			writeJSON(w, 200, runs)
			return
		}
		writeJSON(w, 200, goal)
		return
	}
	if r.Method == http.MethodDelete {
		_ = s.goalStore.Delete(id)
		writeJSON(w, 200, map[string]string{"status": "deleted"})
		return
	}
	if r.Method == http.MethodPost && action == "pause" {
		goal.Status = agent.GoalPaused
		goal.Updated = time.Now()
		_ = s.goalStore.Save(goal)
		writeJSON(w, 200, goal)
		return
	}
	if r.Method == http.MethodPost && action == "resume" {
		goal.Status = agent.GoalActive
		goal.Updated = time.Now()
		_ = s.goalStore.Save(goal)
		writeJSON(w, 200, goal)
		return
	}
	if r.Method == http.MethodPost && action == "run" {
		if !s.acquireGoalRun(goal.ID) {
			writeError(w, http.StatusConflict, "目标已有运行中的任务。")
			return
		}
		defer s.releaseGoalRun(goal.ID)
		if s.goalStore.HasRunningRun(goal.ID) {
			writeError(w, http.StatusConflict, "目标已有运行中的任务。")
			return
		}
		input := agentTaskInput{GoalID: goal.ID, Provider: goal.Provider, Model: goal.Model, Root: goal.Root, Access: "ask", Isolation: goal.Isolation, Messages: []map[string]string{{"role": "user", "content": goal.Prompt}}}
		run := agent.GoalRun{ID: "r" + time.Now().Format("20060102150405.000000000"), GoalID: goal.ID, Status: "queued", StartedAt: time.Now()}
		if err := s.goalStore.SaveRun(run); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		created, createErr := s.createAgentTask(input, false)
		if createErr != nil {
			_ = s.goalStore.UpdateRun(run.ID, "failed", createErr.Error())
			writeError(w, http.StatusBadRequest, "目标运行失败："+createErr.Error())
			return
		}
		run.TaskID, run.Status = created.ID, "running"
		_ = s.goalStore.SaveRun(run)
		goal.LastTaskID, goal.Updated = created.ID, time.Now()
		_ = s.goalStore.Save(goal)
		writeJSON(w, http.StatusAccepted, map[string]any{"goal": goal, "taskId": created.ID})
		return
	}
	writeError(w, 404, "目标操作不存在。")
	return
}

func (s *server) startGoalScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.goalSchedulerStop:
				return
			case <-ticker.C:
			}
			goals, _ := s.goalStore.List()
			for _, goal := range goals {
				if goal.Status != agent.GoalActive || goal.ScheduleMinutes <= 0 || goal.NextRun.After(time.Now()) {
					continue
				}
				if s.goalStore.HasRunningRun(goal.ID) {
					continue
				}
				if !s.acquireGoalRun(goal.ID) {
					continue
				}
				input := agentTaskInput{GoalID: goal.ID, Provider: goal.Provider, Model: goal.Model, Root: goal.Root, Access: "ask", Isolation: goal.Isolation, Messages: []map[string]string{{"role": "user", "content": goal.Prompt}}}
				run := agent.GoalRun{ID: "r" + time.Now().Format("20060102150405.000000000"), GoalID: goal.ID, Status: "queued", StartedAt: time.Now()}
				_ = s.goalStore.SaveRun(run)
				if created, createErr := s.createAgentTask(input, false); createErr == nil {
					goal.LastTaskID = created.ID
					run.TaskID, run.Status = created.ID, "running"
					_ = s.goalStore.SaveRun(run)
					goal.LastError = ""
				} else {
					goal.LastError = createErr.Error()
					_ = s.goalStore.UpdateRun(run.ID, "failed", createErr.Error())
				}
				goal.NextRun = time.Now().Add(time.Duration(goal.ScheduleMinutes) * time.Minute)
				goal.Updated = time.Now()
				_ = s.goalStore.Save(goal)
				s.releaseGoalRun(goal.ID)
			}
		}
	}()
}

func (s *server) acquireGoalRun(id string) bool {
	s.goalSchedulerMu.Lock()
	defer s.goalSchedulerMu.Unlock()
	if s.goalRunning == nil {
		s.goalRunning = map[string]bool{}
	}
	if s.goalRunning[id] {
		return false
	}
	s.goalRunning[id] = true
	return true
}

func (s *server) releaseGoalRun(id string) {
	s.goalSchedulerMu.Lock()
	delete(s.goalRunning, id)
	s.goalSchedulerMu.Unlock()
}

func (s *server) stopGoalScheduler() {
	if s.goalSchedulerStop != nil {
		select {
		case <-s.goalSchedulerStop:
		default:
			close(s.goalSchedulerStop)
		}
	}
}

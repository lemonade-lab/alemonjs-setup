package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"alemonx/internal/agent"
	"alemonx/internal/ai"
	"alemonx/internal/robot"
)

type agentTaskInput struct {
	Provider  string              `json:"provider"`
	Model     string              `json:"model"`
	Root      string              `json:"root"`
	SessionID string              `json:"sessionId"`
	Access    string              `json:"access"`
	Messages  []map[string]string `json:"messages"`
}

func (s *server) agentTasksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.agentTasks.List())
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input agentTaskInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求无法识别。")
		return
	}
	if len(input.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "请填写要发送的消息。")
		return
	}
	if input.Access == "" {
		input.Access = "ask"
	}
	if input.Access != "ask" && input.Access != "auto" && input.Access != "full" {
		writeError(w, http.StatusBadRequest, "权限模式无效。")
		return
	}
	if _, err := (robot.Manager{}).Validate(input.Root); err != nil {
		writeError(w, http.StatusBadRequest, "请先选择一个有效的机器人目录。")
		return
	}
	cfg, err := s.ai.Resolve(input.Provider, input.Model)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sessionID := input.SessionID
	if sessionID == "" {
		title := titleFromMessage(input.Messages[len(input.Messages)-1]["content"])
		session, createErr := s.agentSessions.Create(input.Root, input.Provider, input.Model, title)
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, createErr.Error())
			return
		}
		sessionID = session.ID
	}
	task := agent.AgentTask{SessionID: sessionID, Root: input.Root, Provider: input.Provider, Model: input.Model, Access: input.Access}
	goal := input.Messages[len(input.Messages)-1]["content"]
	task.Plan = defaultTaskPlan(goal)
	initial := make([]agent.Message, 0, len(input.Messages))
	for _, message := range input.Messages {
		initial = append(initial, agent.Message{Role: message["role"], Content: message["content"]})
	}
	checkpoint := agent.AgentCheckpoint{TaskID: task.ID, SessionID: sessionID, Root: input.Root, Provider: input.Provider, Model: input.Model, Messages: initial, Status: agent.TaskQueued, Plan: task.Plan, Updated: time.Now()}
	// TaskManager assigns the ID; the checkpoint is written by the runner before
	// the first model call once that ID is known.
	runner := s.makeAgentTaskRunner(cfg, checkpoint, input.Access)
	created, err := s.agentTasks.Create(task, runner)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	checkpoint.TaskID = created.ID
	_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
	if err := s.agentTasks.Start(created.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created.Status = agent.TaskRunning
	writeJSON(w, http.StatusAccepted, created)
}

func defaultTaskPlan(goal string) agent.TaskPlan {
	return agent.TaskPlan{Goal: goal, Completion: "目标完成且验证命令通过", Steps: []agent.PlanStep{{ID: "understand", Title: "理解项目", Description: "使用项目地图和必要文件确认实现入口", Status: "pending"}, {ID: "implement", Title: "实现变更", Description: "按用户目标修改最少的相关文件", Status: "pending"}, {ID: "verify", Title: "验证结果", Description: "运行相关验证并修复失败", Status: "pending"}}, CurrentStep: 0}
}

func (s *server) makeAgentTaskRunner(cfg ai.Resolved, checkpoint agent.AgentCheckpoint, access string) agent.TaskRunner {
	return func(ctx context.Context, task agent.AgentTask, emit func(agent.Event)) (string, error) {
		checkpoint.TaskID = task.ID
		checkpoint.Status = agent.TaskRunning
		checkpoint.Plan = task.Plan
		if checkpoint.Plan.CurrentStep < len(checkpoint.Plan.Steps) {
			checkpoint.Plan.Steps[checkpoint.Plan.CurrentStep].Status = "running"
		}
		snapshotStore := agent.NewSnapshotStoreAt(filepath.Join(s.agentTaskStore.TasksDir(), task.ID, "snapshots"))
		files := &robotFileService{manager: robot.Manager{}, snapshot: snapshotStore, taskID: task.ID}
		files.lockOwner = task.ID
		defer func() {
			if files.unlock != nil {
				files.unlock()
			}
		}()
		messages := agent.PruneOrphanTools(checkpoint.Messages)
		systemPrompt := agent.BuildSystemPrompt(task.Root, files, agentBasePrompt())
		checkpoint.SystemPrompt = systemPrompt
		checkpoint.Updated = time.Now()
		_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
		loop := agent.NewLoop(cfg, agent.ProjectTools(task.Root, files, agent.NewCommandRunner()), systemPrompt, 40).WithContextBudget(120 * 1024).WithAutoVerify()
		loop.WithObserver(emit)
		loop.WithCheckpoint(func(turn int, transcript []agent.Message) {
			checkpoint.Turn = turn
			checkpoint.Messages = transcript
			checkpoint.Updated = time.Now()
			_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
		})
		if access == "ask" {
			loop.WithApprover(askApprover(s.agentConfirms, emit, task.ID))
		} else {
			loop.WithApprover(taskApprover(task.Root))
		}
		result, err := loop.Run(ctx, messages)
		if err != nil {
			if checkpoint.Plan.CurrentStep < len(checkpoint.Plan.Steps) {
				checkpoint.Plan.Steps[checkpoint.Plan.CurrentStep].Status = "failed"
				checkpoint.Plan.Steps[checkpoint.Plan.CurrentStep].Result = err.Error()
			}
			checkpoint.Status = agent.TaskFailed
			_, _ = s.agentTasks.UpdatePlan(task.ID, checkpoint.Plan)
			checkpoint.LastError = err.Error()
			checkpoint.Updated = time.Now()
			_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
			return "", err
		}
		for i := range checkpoint.Plan.Steps {
			checkpoint.Plan.Steps[i].Status = "completed"
		}
		checkpoint.Status = agent.TaskCompleted
		_, _ = s.agentTasks.UpdatePlan(task.ID, checkpoint.Plan)
		checkpoint.Messages = result.Messages
		checkpoint.Updated = time.Now()
		_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
		return result.Answer, nil
	}
}

func (s *server) agentTaskHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/agent/tasks/")
	parts := strings.Split(strings.Trim(id, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "任务 ID 无效。")
		return
	}
	taskID := parts[0]
	if taskID == "." || taskID == ".." || strings.ContainsAny(taskID, `/\\`) {
		writeError(w, http.StatusBadRequest, "任务 ID 无效。")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		task, err := s.agentTasks.Get(taskID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, task)
		return
	}
	if len(parts) == 2 && parts[1] == "plan" {
		task, err := s.agentTasks.Get(taskID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, task.Plan)
			return
		}
		if r.Method == http.MethodPatch {
			var plan agent.TaskPlan
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&plan); err != nil {
				writeError(w, http.StatusBadRequest, "计划格式无效。")
				return
			}
			updated, err := s.agentTasks.UpdatePlan(taskID, plan)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, updated.Plan)
			return
		}
	}
	if len(parts) == 3 && parts[1] == "plan" && parts[2] == "approve" && r.Method == http.MethodPost {
		task, err := s.agentTasks.Get(taskID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		plan := task.Plan
		plan.Approved = true
		updated, err := s.agentTasks.UpdatePlan(taskID, plan)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated.Plan)
		return
	}
	if len(parts) == 4 && parts[1] == "step" && parts[3] == "retry" && r.Method == http.MethodPost {
		task, err := s.agentTasks.Get(taskID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		plan := task.Plan
		found := false
		for i := range plan.Steps {
			if plan.Steps[i].ID == parts[2] {
				plan.Steps[i].Status = "pending"
				plan.Steps[i].Attempts++
				plan.CurrentStep = i
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "步骤不存在。")
			return
		}
		updated, err := s.agentTasks.UpdatePlan(taskID, plan)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated.Plan)
		return
	}
	if len(parts) == 2 && parts[1] == "events" && r.Method == http.MethodGet {
		s.agentTaskEvents(w, r, taskID)
		return
	}
	if len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost {
		if err := s.agentTasks.Cancel(taskID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
		return
	}
	if len(parts) == 2 && parts[1] == "resume" && r.Method == http.MethodPost {
		task, err := s.agentTasks.Get(taskID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		checkpoint, err := s.agentTaskStore.LoadCheckpoint(taskID)
		if err != nil {
			writeError(w, http.StatusConflict, "没有可恢复的 checkpoint")
			return
		}
		cfg, err := s.ai.Resolve(task.Provider, task.Model)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		runner := s.makeAgentTaskRunner(cfg, checkpoint, "ask")
		if err := s.agentTasks.Resume(taskID, runner); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		task, _ = s.agentTasks.Get(taskID)
		writeJSON(w, http.StatusAccepted, task)
		return
	}
	if len(parts) == 2 && parts[1] == "rollback" && r.Method == http.MethodPost {
		task, err := s.agentTasks.Get(taskID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		store := agent.NewSnapshotStoreAt(filepath.Join(s.agentTaskStore.TasksDir(), taskID, "snapshots"))
		conflicts, rollbackErr := store.Rollback(taskID, task.Root, false)
		if rollbackErr != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": rollbackErr.Error(), "conflicts": conflicts})
			return
		}
		if err := s.agentTasks.SetStatus(taskID, agent.TaskRolledBack, ""); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		task, _ = s.agentTasks.Get(taskID)
		writeJSON(w, http.StatusOK, task)
		return
	}
	writeError(w, http.StatusNotFound, "任务操作不存在。")
	return
}

func (s *server) agentTaskEvents(w http.ResponseWriter, r *http.Request, id string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前服务不支持事件流。")
		return
	}
	after, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 0)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	last := after
	for {
		events, err := s.agentTasks.Events(id, last)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		for _, event := range events {
			raw, _ := json.Marshal(event)
			_, _ = w.Write([]byte("id: " + strconv.FormatInt(event.ID, 10) + "\ndata: " + string(raw) + "\n\n"))
			last = event.ID
		}
		flusher.Flush()
		task, err := s.agentTasks.Get(id)
		if err != nil {
			return
		}
		if task.Status == agent.TaskCompleted || task.Status == agent.TaskFailed || task.Status == agent.TaskCancelled || task.Status == agent.TaskRolledBack {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

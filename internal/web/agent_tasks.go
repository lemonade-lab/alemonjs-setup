package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
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
	GoalID    string              `json:"goalId,omitempty"`
	Access    string              `json:"access"`
	Messages  []map[string]string `json:"messages"`
	Isolation string              `json:"isolation,omitempty"`
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
	legacy := r.Header.Get("X-Legacy-Agent") == "1"
	created, err := s.createAgentTask(input, legacy)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "有效的机器人目录") || strings.Contains(err.Error(), "隔离模式") || strings.Contains(err.Error(), "权限模式") || strings.Contains(err.Error(), "请填写") {
			status = http.StatusBadRequest
		}
		if strings.Contains(err.Error(), "provider") || strings.Contains(err.Error(), "model") {
			status = http.StatusBadGateway
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": created.ID, "sessionId": created.SessionID, "status": created.Status, "task": created})
}

func (s *server) createAgentTask(input agentTaskInput, legacy bool) (agent.AgentTask, error) {
	if len(input.Messages) == 0 {
		return agent.AgentTask{}, errors.New("请填写要发送的消息。")
	}
	if input.Access == "" {
		input.Access = "ask"
	}
	if input.Access != "ask" && input.Access != "auto" && input.Access != "full" {
		return agent.AgentTask{}, errors.New("权限模式无效。")
	}
	if _, err := (robot.Manager{}).Validate(input.Root); err != nil {
		return agent.AgentTask{}, errors.New("请先选择一个有效的机器人目录。")
	}
	cfg, err := s.ai.Resolve(input.Provider, input.Model)
	if err != nil {
		return agent.AgentTask{}, err
	}
	sessionID := input.SessionID
	if sessionID == "" {
		title := titleFromMessage(input.Messages[len(input.Messages)-1]["content"])
		session, createErr := s.agentSessions.Create(input.Root, input.Provider, input.Model, title)
		if createErr != nil {
			return agent.AgentTask{}, createErr
		}
		sessionID = session.ID
	}
	task := agent.AgentTask{SessionID: sessionID, GoalID: input.GoalID, Root: input.Root, Provider: input.Provider, Model: input.Model, Access: input.Access}
	if input.Isolation == "" {
		input.Isolation = "workspace"
	}
	if input.Isolation != "workspace" && input.Isolation != "worktree" {
		return agent.AgentTask{}, errors.New("隔离模式无效。")
	}
	task.Isolation = input.Isolation
	goal := input.Messages[len(input.Messages)-1]["content"]
	task.Plan = defaultTaskPlan(goal)
	if legacy {
		task.Plan.Approved = true
		task.Status = agent.TaskQueued
	} else {
		task.Status = agent.TaskPlanPending
	}
	initial := make([]agent.Message, 0, len(input.Messages))
	for _, message := range input.Messages {
		initial = append(initial, agent.Message{Role: message["role"], Content: message["content"]})
	}
	checkpoint := agent.AgentCheckpoint{TaskID: task.ID, SessionID: sessionID, Root: input.Root, Provider: input.Provider, Model: input.Model, Messages: initial, Status: task.Status, Plan: task.Plan, Updated: time.Now()}
	// TaskManager assigns the ID; the checkpoint is written by the runner before
	// the first model call once that ID is known.
	runner := s.makeAgentTaskRunner(cfg, checkpoint, input.Access)
	created, err := s.taskService.Create(task, runner)
	if err != nil {
		return agent.AgentTask{}, err
	}
	checkpoint.TaskID = created.ID
	_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
	if task.Status != agent.TaskPlanPending {
		if err := s.taskService.Start(created.ID); err != nil {
			return agent.AgentTask{}, err
		}
	}
	if task.Status != agent.TaskPlanPending {
		created.Status = agent.TaskRunning
	}
	return created, nil
}

func defaultTaskPlan(goal string) agent.TaskPlan {
	return agent.TaskPlan{Goal: goal, Completion: "目标完成且验证命令通过", Steps: []agent.PlanStep{{ID: "understand", Title: "理解项目", Description: "使用项目地图和必要文件确认实现入口", Status: "pending"}, {ID: "implement", Title: "实现变更", Description: "按用户目标修改最少的相关文件", Status: "pending"}, {ID: "verify", Title: "验证结果", Description: "运行相关验证并修复失败", Status: "pending"}}, CurrentStep: 0}
}

func (s *server) makeAgentTaskRunner(cfg ai.Resolved, checkpoint agent.AgentCheckpoint, access string) agent.TaskRunner {
	return func(ctx context.Context, task agent.AgentTask, emit func(agent.Event)) (string, error) {
		checkpoint.TaskID = task.ID
		checkpoint.Status = agent.TaskRunning
		checkpoint.Plan = task.Plan
		checkpoint.Context.Goal = task.Plan.Goal
		workRoot := task.Root
		var worktree *agent.Worktree
		if task.Isolation == "worktree" {
			created, workErr := agent.CreateWorktree(task.Root, task.ID)
			if workErr != nil {
				emit(agent.Event{Type: "text", Text: "worktree 不可用，已回退到 workspace：" + workErr.Error()})
			} else {
				worktree, workRoot = &created, created.Root
				checkpoint.WorktreeRoot = workRoot
				defer worktree.Remove()
			}
		}
		snapshotStore := agent.NewSnapshotStoreAt(filepath.Join(s.agentTaskStore.TasksDir(), task.ID, "snapshots"))
		files := &robotFileService{manager: robot.Manager{}, snapshot: snapshotStore, taskID: task.ID}
		files.lockOwner = task.ID
		files.planApproved = task.Plan.Approved
		defer func() {
			if files.unlock != nil {
				files.unlock()
			}
		}()
		messages := agent.PruneOrphanTools(checkpoint.Messages)
		var result *agent.Result
		for checkpoint.Plan.CurrentStep < len(checkpoint.Plan.Steps) {
			currentTask, getErr := s.agentTasks.Get(task.ID)
			if getErr != nil {
				return "", getErr
			}
			checkpoint.Plan = currentTask.Plan
			idx := checkpoint.Plan.CurrentStep
			step := checkpoint.Plan.Steps[idx]
			if step.Status == "completed" || step.Status == "skipped" {
				if idx == len(checkpoint.Plan.Steps)-1 {
					break
				}
				if _, advErr := (agent.StepExecutor{Manager: s.agentTasks}).Advance(task.ID); advErr != nil {
					return "", advErr
				}
				continue
			}
			if _, startErr := (agent.StepExecutor{Manager: s.agentTasks}).StartCurrent(task.ID); startErr != nil {
				return "", startErr
			}
			stepID := step.ID
			files.stepID = stepID
			checkpoint.LastAction = "执行步骤：" + step.Title
			checkpoint.SystemPrompt = agent.BuildSystemPrompt(workRoot, files, agentBasePrompt()+"\n当前计划步骤："+step.Title+"\n步骤说明："+step.Description)
			checkpoint.Updated = time.Now()
			_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
			verifySeen, writeSeen := false, false
			loop := agent.NewLoop(cfg, agent.ProjectTools(workRoot, files, agent.NewCommandRunner()), checkpoint.SystemPrompt, 40).WithContextBudget(120 * 1024).WithAutoVerify()
			loop.WithVerificationObserver(func(v agent.VerificationResult) {
				verifySeen = true
				executor := agent.StepExecutor{Manager: s.agentTasks}
				if v.Passed {
					_, _ = executor.Complete(task.ID, v.Output)
				} else {
					_, _ = executor.MarkVerifying(task.ID, v.Output)
					_, _ = executor.Fail(task.ID, v.Error)
				}
			})
			loop.WithObserver(func(event agent.Event) {
				if event.Type == "tool" && (event.Tool == "agent_edit_file" || event.Tool == "agent_run_command") {
					writeSeen = true
					_, _ = s.agentTasks.MarkStep(task.ID, stepID, "verifying", "等待验证")
				}
				emit(event)
			})
			loop.WithCheckpoint(func(turn int, transcript []agent.Message) {
				checkpoint.Turn, checkpoint.Messages, checkpoint.Updated = turn, transcript, time.Now()
				_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
			})
			if access == "ask" {
				loop.WithApprover(askApprover(s.agentConfirms, emit, task.ID))
			} else {
				loop.WithApprover(taskApprover(workRoot))
			}
			stepResult, runErr := loop.Run(ctx, messages)
			if runErr != nil {
				_, _ = (agent.StepExecutor{Manager: s.agentTasks}).Fail(task.ID, runErr.Error())
				checkpoint.Status, checkpoint.LastError = agent.TaskFailed, runErr.Error()
				_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
				return "", runErr
			}
			result, messages = stepResult, stepResult.Messages
			updated, _ := s.agentTasks.Get(task.ID)
			stepState := updated.Plan.Steps[updated.Plan.CurrentStep]
			if stepState.Status == "running" {
				if stepID == "understand" && !writeSeen {
					_, _ = (agent.StepExecutor{Manager: s.agentTasks}).Complete(task.ID, "项目结构理解完成")
				} else if !verifySeen {
					err := errors.New("步骤未完成验证")
					_, _ = (agent.StepExecutor{Manager: s.agentTasks}).Fail(task.ID, err.Error())
					return "", err
				}
			}
			updated, _ = s.agentTasks.Get(task.ID)
			if updated.Plan.Steps[updated.Plan.CurrentStep].Status != "completed" && updated.Plan.Steps[updated.Plan.CurrentStep].Status != "skipped" {
				return "", errors.New("步骤验证失败")
			}
			checkpoint.Plan = updated.Plan
			checkpoint.Messages = messages
			if updated.Plan.CurrentStep < len(updated.Plan.Steps)-1 {
				if _, advErr := (agent.StepExecutor{Manager: s.agentTasks}).Advance(task.ID); advErr != nil {
					return "", advErr
				}
			}
		}
		if result == nil {
			return "", errors.New("计划没有产生结果")
		}
		modified := make([]string, 0, len(files.snapshots))
		for _, snap := range files.snapshots {
			modified = append(modified, snap.Path)
		}
		review := agent.ReviewTaskWithModel(cfg, checkpoint.Plan, result.Answer, strings.Join(modified, "\n"))
		reviewRaw, _ := json.Marshal(review)
		emit(agent.Event{Type: "review", Text: string(reviewRaw)})
		report := agent.TaskReport{Goal: checkpoint.Plan.Goal, Plan: checkpoint.Plan, ModifiedFiles: modified, Validation: []string{"Agent loop completed"}, Reviewer: review, Summary: review.Summary, RollbackTaskID: task.ID, GeneratedAt: time.Now()}
		if worktree != nil {
			report.Diff = worktree.Diff()
		}
		checkpoint.Report = &report
		checkpoint.Context.ModifiedFiles = modified
		checkpoint.Context.Validation = report.Validation
		checkpoint.Context.Summary = report.Summary
		_ = s.agentTaskStore.SaveReport(report, task.ID)
		if !review.GoalSatisfied {
			checkpoint.Status = agent.TaskFailed
			checkpoint.LastError = review.Summary
			_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
			return "", errors.New(review.Summary)
		}
		checkpoint.Status = agent.TaskCompleted
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
		updated, err = s.agentTasks.ApprovePlan(taskID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.agentTasks.Start(taskID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updated.Status = agent.TaskRunning
		writeJSON(w, http.StatusAccepted, updated)
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
				if plan.Steps[i].Status != "failed" && plan.Steps[i].Status != "verifying" {
					writeError(w, http.StatusConflict, "只有失败或验证中的步骤可以重试。")
					return
				}
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
	if len(parts) == 2 && parts[1] == "report" && r.Method == http.MethodGet {
		report, err := s.agentTaskStore.LoadReport(taskID)
		if err != nil {
			writeError(w, http.StatusNotFound, "任务报告尚未生成。")
			return
		}
		writeJSON(w, http.StatusOK, report)
		return
	}
	if len(parts) == 2 && parts[1] == "merge" && r.Method == http.MethodPost {
		if task, err := s.agentTasks.Get(taskID); err != nil {
			writeError(w, http.StatusNotFound, "任务不存在。")
		} else {
			var input struct {
				Confirm bool `json:"confirm"`
			}
			_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input)
			if task.Isolation != "worktree" {
				writeError(w, http.StatusConflict, "只有 worktree 任务可以合并。")
				return
			}
			if !input.Confirm {
				writeError(w, http.StatusBadRequest, "合并前必须明确确认。")
				return
			}
			report, reportErr := s.agentTaskStore.LoadReport(taskID)
			if reportErr != nil || strings.TrimSpace(report.Diff) == "" {
				writeError(w, http.StatusConflict, "任务没有可应用的 diff。")
				return
			}
			cmd := exec.Command("git", "-C", task.Root, "apply", "--whitespace=nowarn", "-")
			cmd.Stdin = bytes.NewBufferString(report.Diff)
			if output, applyErr := cmd.CombinedOutput(); applyErr != nil {
				writeError(w, http.StatusConflict, "合并失败："+strings.TrimSpace(string(output)))
				return
			}
			s.agentTasks.SetStatus(taskID, agent.TaskCompleted, "")
			s.agentTasks.EmitExternal(taskID, agent.Event{Type: "merge", Text: "worktree diff 已应用到主工作区"})
			writeJSON(w, http.StatusOK, map[string]any{"status": "merged", "taskId": taskID})
		}
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
	if after == 0 {
		after, _ = strconv.ParseInt(r.URL.Query().Get("lastEventId"), 10, 0)
	}
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

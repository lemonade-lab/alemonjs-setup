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
	Provider       string              `json:"provider"`
	Model          string              `json:"model"`
	Root           string              `json:"root"`
	SessionID      string              `json:"sessionId"`
	GoalID         string              `json:"goalId,omitempty"`
	Access         string              `json:"access"`
	Messages       []map[string]string `json:"messages"`
	Isolation      string              `json:"isolation,omitempty"`
	IdempotencyKey string              `json:"idempotencyKey,omitempty"`
}

const safeModelFailureMessage = "模型服务暂时无法继续处理，已保留当前进度。请稍后继续任务。"

// publicTask prevents historical checkpoints/tasks from leaking provider
// protocol errors into the UI. The full diagnostic stays on disk and in the
// server log for operators; the conversation gets a useful recovery action.
func publicTask(task agent.AgentTask) agent.AgentTask {
	task.LastError = publicAgentError(task.LastError)
	for index := range task.Plan.Steps {
		task.Plan.Steps[index].Result = publicAgentError(task.Plan.Steps[index].Result)
	}
	return task
}

func publicAgentError(text string) string {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "ai 请求失败") ||
		strings.Contains(lower, "tool_calls") ||
		strings.Contains(lower, "tool_call_id") ||
		strings.Contains(lower, "insufficient tool") {
		return safeModelFailureMessage
	}
	return text
}

func publicTaskEvent(event agent.TaskEvent) agent.TaskEvent {
	event.Text = publicAgentError(event.Text)
	event.Output = publicAgentError(event.Output)
	return event
}

func (s *server) agentTasksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tasks := s.agentTasks.List()
		for index := range tasks {
			tasks[index] = publicTask(tasks[index])
		}
		writeJSON(w, http.StatusOK, tasks)
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
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
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
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": created.ID, "sessionId": created.SessionID, "status": created.Status, "task": publicTask(created)})
}

func (s *server) createAgentTask(input agentTaskInput, _ bool) (agent.AgentTask, error) {
	if len(input.Messages) == 0 {
		return agent.AgentTask{}, errors.New("请填写要发送的消息。")
	}
	if input.Access == "" {
		// Interactive tasks should feel useful immediately for first-time users.
		// "auto" still stays within the project's safe tool allowlist; users can
		// explicitly choose ask/full in the UI. Scheduled and resumed goals keep
		// their stricter ask mode at their dedicated call sites.
		input.Access = "auto"
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
	task := agent.AgentTask{SessionID: sessionID, GoalID: input.GoalID, Root: input.Root, Provider: input.Provider, Model: input.Model, Access: input.Access, IdempotencyKey: input.IdempotencyKey}
	if input.Isolation == "" {
		input.Isolation = "workspace"
	}
	if input.Isolation != "workspace" && input.Isolation != "worktree" {
		return agent.AgentTask{}, errors.New("隔离模式无效。")
	}
	task.Isolation = input.Isolation
	goal := input.Messages[len(input.Messages)-1]["content"]
	task.Plan = defaultTaskPlan(goal)
	if !requiresWriteSteps(goal) {
		// Read-only questions use a shorter plan. A write-intent task still starts
		// immediately: the individual write/command tool is where ask-mode asks
		// the user for permission, rather than treating a plan as a permission.
		task.Plan = readOnlyTaskPlan(goal)
	}
	// “Approved” here means that the durable plan is executable by the state
	// machine. It is deliberately not user authorization for file changes:
	// askApprover continues to require confirmation for each risky tool call.
	task.Plan.Approved = true
	task.Status = agent.TaskQueued
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
	// Task checkpoints are for recovery, but they are not the user-facing
	// conversation history. Persist this task's new user turn separately so a
	// plan_pending task, a cancelled task, and a completed task all remain
	// visible after reopening the session.
	if user := latestUserMessage(input.Messages); user != "" {
		if err := s.agentSessions.Append(sessionID, agent.Message{Role: "user", Content: user}); err != nil {
			_ = s.agentTasks.SetStatus(created.ID, agent.TaskPaused, "保存会话记录失败："+err.Error())
			return agent.AgentTask{}, errors.New("保存会话记录失败：" + err.Error())
		}
	}
	checkpoint.TaskID = created.ID
	_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
	if err := s.taskService.Start(created.ID); err != nil {
		return agent.AgentTask{}, err
	}
	created.Status = agent.TaskRunning
	return created, nil
}

// latestUserMessage returns only the new user turn submitted for a task. The
// client includes prior turns as model context; appending all of them here
// would duplicate the transcript every time the user sends a message.
func latestUserMessage(messages []map[string]string) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i]["role"] == "user" {
			return strings.TrimSpace(messages[i]["content"])
		}
	}
	return ""
}

func defaultTaskPlan(goal string) agent.TaskPlan {
	return agent.TaskPlan{Goal: goal, Completion: "目标完成且验证命令通过", Steps: []agent.PlanStep{{ID: "understand", Title: "理解项目", Description: "使用项目地图和必要文件确认实现入口", Status: "pending"}, {ID: "implement", Title: "实现变更", Description: "按用户目标修改最少的相关文件", Status: "pending"}, {ID: "verify", Title: "验证结果", Description: "运行相关验证并修复失败", Status: "pending"}}, CurrentStep: 0}
}

func requiresWriteSteps(goal string) bool {
	lower := strings.ToLower(strings.TrimSpace(goal))
	for _, keyword := range []string{"修改", "修复", "创建", "新增", "添加", "删除", "重构", "实现", "改动", "写入", "fix", "edit", "create", "add", "delete", "refactor", "implement", "change", "write"} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func readOnlyTaskPlan(goal string) agent.TaskPlan {
	return agent.TaskPlan{Goal: goal, Completion: "已回答问题并给出依据", Steps: []agent.PlanStep{{ID: "answer", Title: "分析并回答", Description: "读取项目相关信息并直接回答用户问题", Status: "pending"}}, CurrentStep: 0}
}

func (s *server) makeAgentTaskRunner(cfg ai.Resolved, checkpoint agent.AgentCheckpoint, access string) agent.TaskRunner {
	return func(ctx context.Context, task agent.AgentTask, emit func(agent.Event)) (string, error) {
		var projectLease agent.LeaseManager
		if requiresWriteSteps(task.Plan.Goal) && s.opsStore != nil {
			projectLease = agent.NewLeaseManager(s.opsStore)
			leaseKey := "project:" + filepath.Clean(task.Root)
			if err := projectLease.Acquire(ctx, leaseKey, task.ID, 30*time.Minute); err != nil {
				return "", errors.New("项目写租约获取失败：" + err.Error())
			}
			leaseCtx, stopLease := context.WithCancel(ctx)
			ctx = leaseCtx
			defer stopLease()
			go func() {
				ticker := time.NewTicker(5 * time.Minute)
				defer ticker.Stop()
				for {
					select {
					case <-leaseCtx.Done():
						return
					case <-ticker.C:
						if err := projectLease.Renew(leaseCtx, leaseKey, task.ID, 30*time.Minute); err != nil {
							emit(agent.Event{Type: "error", Text: "项目写租约续期失败，任务将暂停：" + err.Error()})
							stopLease()
							return
						}
					}
				}
			}()
			defer func() { _ = projectLease.Release(context.Background(), leaseKey, task.ID) }()
		}
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
				if (stepID == "understand" || stepID == "answer" || stepID == "verify") && !writeSeen {
					// Read-only steps still follow the durable state machine. They
					// don't need a shell verification, but must pass through
					// verifying before Complete; otherwise MarkStep rejects the
					// transition and the task is incorrectly marked failed after
					// the model has already emitted its final answer.
					executor := agent.StepExecutor{Manager: s.agentTasks}
					if _, err := executor.MarkVerifying(task.ID, "已生成并检查回答"); err != nil {
						return "", err
					}
					if _, err := executor.Complete(task.ID, "已生成并检查回答"); err != nil {
						return "", err
					}
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
		if answer := strings.TrimSpace(result.Answer); answer != "" {
			if err := s.agentSessions.Append(task.SessionID, agent.Message{Role: "assistant", Content: answer}); err != nil {
				checkpoint.Status = agent.TaskFailed
				checkpoint.LastError = "保存会话记录失败：" + err.Error()
				_ = s.agentTaskStore.SaveCheckpoint(checkpoint)
				return "", errors.New(checkpoint.LastError)
			}
		}
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
		writeJSON(w, http.StatusOK, publicTask(task))
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
			wasPlanPending := task.Status == agent.TaskPlanPending
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
			if wasPlanPending {
				if err := s.agentTasks.Start(taskID); err != nil {
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				updated, _ = s.agentTasks.Get(taskID)
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
		writeJSON(w, http.StatusAccepted, publicTask(updated))
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
		writeJSON(w, http.StatusAccepted, publicTask(task))
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
		writeJSON(w, http.StatusOK, publicTask(task))
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
	wake, unsubscribe, subscribeErr := s.agentTasks.Subscribe(id)
	if subscribeErr != nil {
		writeError(w, http.StatusNotFound, subscribeErr.Error())
		return
	}
	defer unsubscribe()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	last := after
	for {
		events, err := s.agentTasks.Events(id, last)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		for _, event := range events {
			event = publicTaskEvent(event)
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
		case <-wake:
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

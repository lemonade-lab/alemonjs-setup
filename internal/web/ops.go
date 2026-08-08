package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"alemonx/internal/agent"
	"alemonx/internal/robot"
)

func (s *server) newOpsMonitor() *agent.OpsMonitor {
	aggregator := agent.NewIncidentAggregator(s.opsStore)
	return &agent.OpsMonitor{
		Aggregator: aggregator,
		Interval:   10 * time.Second,
		Source: func(ctx context.Context) ([]agent.ErrorEvent, error) {
			if s.opsPaused {
				return nil, nil
			}
			var events []agent.ErrorEvent
			for _, root := range s.directoryRoots {
				select {
				case <-ctx.Done():
					return events, ctx.Err()
				default:
				}
				result, err := s.robots.PM2Logs(root, 1)
				if err != nil {
					continue
				}
				processName := "pm2"
				if processes, processErr := s.robots.PM2Processes(root); processErr == nil && len(processes) > 0 && processes[0].Name != "" {
					processName = processes[0].Name
				}
				events = append(events, agent.ParsePM2LogOutput(root, processName, result.Output, time.Now())...)
			}
			return events, nil
		},
		OnIncident: func(incident agent.Incident, _ bool) {
			if incident.Status == agent.IncidentObserving {
				incident.Status = agent.IncidentTodo
				incident.Decision = "create_todo"
				incident.DecisionReason = "观察窗口内错误再次出现，停止自动修复并转人工"
				incident.Updated = time.Now()
				_ = s.opsStore.SaveIncident(incident)
				if runs, runsErr := s.opsStore.ListMaintenance(); runsErr == nil {
					for _, run := range runs {
						if run.IncidentID != incident.ID || run.Status != "observing" || run.TaskID == "" {
							continue
						}
						store := agent.NewSnapshotStoreAt(filepath.Join(s.agentTaskStore.TasksDir(), run.TaskID, "snapshots"))
						conflicts, rollbackErr := store.Rollback(run.TaskID, incident.ProjectRoot, false)
						if rollbackErr != nil {
							run.Status, run.Error = "recovery_required", rollbackErr.Error()+" conflicts="+strings.Join(conflicts, ",")
						} else {
							run.Status, run.RollbackPerformed = "rolled_back", true
						}
						now := time.Now()
						run.Finished = &now
						_ = s.opsStore.SaveMaintenance(run)
					}
				}
				return
			}
			if s.opsOrchestrator != nil {
				_, _, _ = s.opsOrchestrator.Analyze(incident.ID)
				return
			}
			incident.Status = agent.IncidentTriaged
			incident.Decision = "create_todo"
			incident.DecisionReason = "AI 运维编排器未初始化"
			_ = s.opsStore.SaveIncident(incident)
		},
		OnPoll: func() {
			if s.opsOrchestrator == nil {
				return
			}
			if runs, err := s.opsStore.ListMaintenance(); err == nil {
				for _, run := range runs {
					if run.Status == "observing" {
						_ = s.opsOrchestrator.Observe(run.IncidentID)
					}
				}
			}
		},
	}
}

func (s *server) opsHandler(w http.ResponseWriter, r *http.Request) {
	if s.opsStore == nil {
		writeError(w, http.StatusServiceUnavailable, "AI 运维中心尚未初始化")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/ops"), "/")
	parts := strings.Split(path, "/")
	if path == "incidents" && r.Method == http.MethodGet {
		items, err := s.opsStore.ListIncidents()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if path == "metrics" && r.Method == http.MethodGet {
		metrics, err := s.opsStore.Metrics()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, metrics)
		return
	}
	if path == "todos" && r.Method == http.MethodGet {
		items, err := s.opsStore.ListTodos()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if path == "maintenance" && r.Method == http.MethodGet {
		items, err := s.opsStore.ListMaintenance()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if path == "policies" && r.Method == http.MethodGet {
		items, err := s.opsStore.ListPolicies()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if len(parts) == 3 && parts[0] == "projects" && (parts[2] == "allow" || parts[2] == "revoke") && r.Method == http.MethodPost {
		root, _ := url.PathUnescape(parts[1])
		var input struct {
			Root string `json:"root"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input)
		if input.Root != "" {
			root = input.Root
		}
		if root == "" {
			writeError(w, 400, "缺少项目根目录")
			return
		}
		if _, err := (robot.Manager{}).Validate(root); err != nil {
			writeError(w, 400, "项目根目录无效")
			return
		}
		policy, err := s.opsStore.GetPolicy(root)
		if err != nil {
			policy = agent.OpsPolicy{ProjectRoot: root, Mode: "observe", MaxModifiedFiles: 10, MaxPM2Actions: 3, ObservationMinutes: 5, FailureCircuitBreak: 3}
		}
		policy.ProjectRoot, policy.AutoAllowed, policy.Updated, policy.Version = root, parts[2] == "allow", time.Now(), policy.Version+1
		if err := s.opsStore.SavePolicy(policy); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, policy)
		return
	}
	if path == "policy" && r.Method == http.MethodGet {
		root := r.URL.Query().Get("root")
		if root == "" {
			writeError(w, 400, "缺少 root")
			return
		}
		policy, err := s.opsStore.GetPolicy(root)
		if err != nil {
			policy = agent.OpsPolicy{ProjectRoot: root, Mode: "observe", MaxModifiedFiles: 10, MaxPM2Actions: 3, ObservationMinutes: 5, FailureCircuitBreak: 3}
		}
		writeJSON(w, 200, policy)
		return
	}
	if path == "policy" && r.Method == http.MethodPatch {
		var policy agent.OpsPolicy
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&policy) != nil || policy.ProjectRoot == "" {
			writeError(w, 400, "策略格式无效")
			return
		}
		if policy.Mode != "off" && policy.Mode != "observe" && policy.Mode != "auto" && policy.Mode != "strict" {
			writeError(w, 400, "运维模式无效")
			return
		}
		if policy.Mode == "auto" && !policy.AutoAllowed {
			writeError(w, 400, "auto 模式必须先加入项目白名单")
			return
		}
		if policy.MaxModifiedFiles < 0 || policy.MaxModifiedFiles > 100 || policy.MaxPM2Actions < 0 || policy.MaxPM2Actions > 20 || policy.ObservationMinutes < 0 || policy.ObservationMinutes > 1440 || policy.FailureCircuitBreak < 0 || policy.FailureCircuitBreak > 10 || policy.TokenBudget < 0 {
			writeError(w, 400, "策略限制不能为负数")
			return
		}
		if _, err := (robot.Manager{}).Validate(policy.ProjectRoot); err != nil {
			writeError(w, 400, "项目根目录无效")
			return
		}
		if policy.Version <= 0 {
			if old, oldErr := s.opsStore.GetPolicy(policy.ProjectRoot); oldErr == nil {
				policy.Version = old.Version + 1
			} else {
				policy.Version = 1
			}
		}
		policy.Updated = time.Now()
		if err := s.opsStore.SavePolicy(policy); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, policy)
		return
	}
	if len(parts) == 2 && parts[0] == "maintenance" && r.Method == http.MethodGet {
		item, err := s.opsStore.GetMaintenance(parts[1])
		if err != nil {
			writeError(w, 404, "维护记录不存在")
			return
		}
		writeJSON(w, 200, item)
		return
	}
	if len(parts) == 3 && parts[0] == "maintenance" && r.Method == http.MethodPost {
		run, err := s.opsStore.GetMaintenance(parts[1])
		if err != nil {
			writeError(w, 404, "维护记录不存在")
			return
		}
		switch parts[2] {
		case "observe":
			if s.opsOrchestrator == nil {
				writeError(w, 503, "AI 运维编排器尚未初始化")
				return
			}
			if err := s.opsOrchestrator.Observe(run.IncidentID); err != nil {
				writeError(w, 409, err.Error())
				return
			}
			run.Status = "resolved"
		case "rollback":
			if run.TaskID == "" {
				writeError(w, 409, "维护记录没有关联任务")
				return
			}
			incident, incidentErr := s.opsStore.GetIncident(run.IncidentID)
			if incidentErr != nil {
				writeError(w, 404, "关联事件不存在")
				return
			}
			store := agent.NewSnapshotStoreAt(filepath.Join(s.agentTaskStore.TasksDir(), run.TaskID, "snapshots"))
			conflicts, rollbackErr := store.Rollback(run.TaskID, incident.ProjectRoot, false)
			if rollbackErr != nil {
				writeJSON(w, http.StatusConflict, map[string]any{"error": rollbackErr.Error(), "conflicts": conflicts})
				return
			}
			run.RollbackPerformed, run.Status = true, "rolled_back"
		default:
			writeError(w, 404, "维护操作不存在")
			return
		}
		finished := time.Now()
		run.Finished = &finished
		if err := s.opsStore.SaveMaintenance(run); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, run)
		return
	}
	if len(parts) >= 2 && parts[0] == "incidents" {
		incident, err := s.opsStore.GetIncident(parts[1])
		if err != nil {
			writeError(w, 404, "事件不存在")
			return
		}
		if len(parts) == 2 && r.Method == http.MethodGet {
			writeJSON(w, 200, incident)
			return
		}
		if len(parts) == 3 && parts[2] == "events" && r.Method == http.MethodGet {
			events, eventsErr := s.opsStore.ListEvents(incident.ID)
			if eventsErr != nil {
				writeError(w, 500, eventsErr.Error())
				return
			}
			writeJSON(w, 200, events)
			return
		}
		if len(parts) == 3 && r.Method == http.MethodPost {
			switch parts[2] {
			case "silence":
				incident.Status = agent.IncidentSilenced
				incident.Updated = time.Now()
				_ = s.opsStore.SaveIncident(incident)
				writeJSON(w, 200, incident)
				return
			case "todo":
				todo := agent.OpsTodo{ID: "todo-" + incident.ID, IncidentID: incident.ID, ProjectRoot: incident.ProjectRoot, Title: "处理：" + incident.ProcessName, Summary: "错误 fingerprint " + incident.Fingerprint, Severity: incident.Severity, Reason: incident.DecisionReason, Status: "open", Created: time.Now(), Updated: time.Now()}
				if err := s.opsStore.SaveTodo(todo); err != nil {
					writeError(w, 500, err.Error())
					return
				}
				incident.Status = agent.IncidentTodo
				incident.TodoID = todo.ID
				incident.Updated = time.Now()
				_ = s.opsStore.SaveIncident(incident)
				writeJSON(w, 202, todo)
				return
			case "analyze":
				if s.opsOrchestrator == nil {
					writeError(w, 503, "AI 运维编排器尚未初始化")
					return
				}
				updated, decision, analyzeErr := s.opsOrchestrator.Analyze(incident.ID)
				if analyzeErr != nil {
					writeError(w, 500, analyzeErr.Error())
					return
				}
				writeJSON(w, 202, map[string]any{"incident": updated, "decision": decision})
				return
			case "retry":
				if s.opsOrchestrator == nil {
					writeError(w, 503, "AI 运维编排器尚未初始化")
					return
				}
				updated, decision, analyzeErr := s.opsOrchestrator.Analyze(incident.ID)
				if analyzeErr != nil {
					writeError(w, 500, analyzeErr.Error())
					return
				}
				writeJSON(w, 202, map[string]any{"incident": updated, "decision": decision, "retry": true})
				return
			case "resume":
				incident.Status = agent.IncidentTriaged
				incident.Updated = time.Now()
				if err := s.opsStore.SaveIncident(incident); err != nil {
					writeError(w, 500, err.Error())
					return
				}
				writeJSON(w, 200, incident)
				return
			case "approve":
				if s.opsOrchestrator == nil {
					writeError(w, 503, "AI 运维编排器尚未初始化")
					return
				}
				updated, approveErr := s.opsOrchestrator.Approve(incident.ID)
				if approveErr != nil {
					writeError(w, 409, approveErr.Error())
					return
				}
				writeJSON(w, 202, updated)
				return
			}
		}
	}
	if len(parts) >= 2 && parts[0] == "todos" {
		todo, err := s.opsStore.GetTodo(parts[1])
		if err != nil {
			writeError(w, 404, "待办不存在")
			return
		}
		if len(parts) == 2 && r.Method == http.MethodGet {
			writeJSON(w, 200, todo)
			return
		}
		if len(parts) == 2 && r.Method == http.MethodPatch {
			var update struct{ Status, Assignee, Title, Summary string }
			if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&update) != nil {
				writeError(w, 400, "待办格式无效")
				return
			}
			if update.Status != "" {
				todo.Status = update.Status
			}
			if update.Assignee != "" {
				todo.Assignee = update.Assignee
			}
			if update.Title != "" {
				todo.Title = update.Title
			}
			if update.Summary != "" {
				todo.Summary = update.Summary
			}
			todo.Updated = time.Now()
			if err := s.opsStore.SaveTodo(todo); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, todo)
			return
		}
	}
	if (path == "monitor/pause" || path == "monitor/emergency-stop") && r.Method == http.MethodPost {
		s.opsPaused = true
		if s.opsMonitor != nil {
			_ = s.opsMonitor.Stop()
		}
		writeJSON(w, 200, map[string]any{"paused": true})
		return
	}
	if path == "monitor/resume" && r.Method == http.MethodPost {
		s.opsPaused = false
		if s.opsMonitor != nil {
			_ = s.opsMonitor.Start(context.Background())
		}
		writeJSON(w, 200, map[string]any{"paused": false})
		return
	}
	writeError(w, 404, "运维操作不存在")
}

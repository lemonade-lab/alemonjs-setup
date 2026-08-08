package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"alemonx/internal/agent"
	"alemonx/internal/robot"
)

func (s *server) opsActor(r *http.Request) (string, string) {
	if s != nil && s.auth != nil {
		if status, err := s.auth.Status(s.authToken(r)); err == nil && status.Authenticated {
			role := "admin"
			if binding, roleErr := s.opsStore.GetRole(status.Account); roleErr == nil && binding.Role != "" {
				role = binding.Role
			}
			return status.Account, role
		}
	}
	actor, role := strings.TrimSpace(r.Header.Get("X-Operator")), strings.ToLower(strings.TrimSpace(r.Header.Get("X-Role")))
	if actor == "" {
		actor = "system"
	}
	if role == "" {
		role = "admin"
	} // backwards-compatible local API
	return actor, role
}

func (s *server) publishOpsEvent(event opsEvent) {
	if _, ok := s.publishEvent("ops", event.Type, event, nil); !ok {
		return
	}
	if s.opsEvents != nil {
		s.opsEvents.publish(event)
	}
}

func opsRoleLevel(role string) int {
	switch role {
	case "viewer":
		return 1
	case "operator":
		return 2
	case "approver":
		return 3
	case "admin":
		return 4
	default:
		return 0
	}
}

func (s *server) requireOpsRole(w http.ResponseWriter, r *http.Request, required, action, resource string) bool {
	actor, role := s.opsActor(r)
	if opsRoleLevel(role) < opsRoleLevel(required) {
		_ = s.opsStore.AppendAudit(agent.AuditEntry{Actor: actor, Role: role, Action: action, Resource: resource, Result: "denied", Reason: "权限不足", Created: time.Now()})
		writeError(w, http.StatusForbidden, "运维权限不足")
		return false
	}
	_ = s.opsStore.AppendAudit(agent.AuditEntry{Actor: actor, Role: role, Action: action, Resource: resource, Result: "accepted", Created: time.Now()})
	return true
}

func (s *server) newOpsMonitor() *agent.OpsMonitor {
	aggregator := agent.NewIncidentAggregator(s.opsStore)
	return &agent.OpsMonitor{
		Aggregator:  aggregator,
		CursorStore: s.opsStore,
		Lease:       agent.NewLeaseManager(s.opsStore),
		LeaseKey:    "ops-monitor",
		LeaseOwner:  s.nodeID,
		LeaseTTL:    45 * time.Second,
		AcquireLease: func() (func(), error) {
			return s.opsStore.AcquireOpsLease("ops-monitor", s.nodeID, 45*time.Second)
		},
		RenewLease: func() error {
			return s.opsStore.RenewOpsLease("ops-monitor", s.nodeID, 45*time.Second)
		},
		Interval: 60 * time.Second,
		Stream: func(ctx context.Context, emit func(agent.ErrorEvent)) error {
			var group sync.WaitGroup
			for _, root := range s.directoryRoots {
				root := root
				group.Add(1)
				go func() {
					defer group.Done()
					delay := time.Second
					for ctx.Err() == nil {
						if s.opsPaused {
							select {
							case <-ctx.Done():
								return
							case <-time.After(time.Second):
								continue
							}
						}
						processName := "pm2"
						if processes, err := s.robots.PM2Processes(root); err == nil && len(processes) > 0 && processes[0].Name != "" {
							processName = processes[0].Name
						}
						name := processName
						err := s.robots.StreamPM2Logs(ctx, root, func(line string) {
							for _, event := range agent.ParsePM2LogOutput(root, name, line, time.Now()) {
								emit(event)
							}
						})
						if ctx.Err() != nil {
							return
						}
						_ = err
						select {
						case <-ctx.Done():
							return
						case <-time.After(delay):
						}
						if delay < 30*time.Second {
							delay *= 2
							if delay > 30*time.Second {
								delay = 30 * time.Second
							}
						}
					}
				}()
			}
			<-ctx.Done()
			group.Wait()
			return ctx.Err()
		},
		Signals: func(ctx context.Context) ([]agent.OpsSignal, error) {
			var signals []agent.OpsSignal
			for _, root := range s.directoryRoots {
				select {
				case <-ctx.Done():
					return signals, ctx.Err()
				default:
				}
				status, err := s.robots.PM2Status(root)
				if err != nil {
					signals = append(signals, agent.OpsSignal{ProjectRoot: root, ProcessName: "pm2", Kind: "health", Status: "error", Message: err.Error(), Timestamp: time.Now()})
					continue
				}
				if !status.Running {
					signals = append(signals, agent.OpsSignal{ProjectRoot: root, ProcessName: "pm2", Kind: "process_exit", Status: status.Status, Message: "PM2 进程不在线", Timestamp: time.Now()})
				}
			}
			return signals, nil
		},
		OnSignal: func(signal agent.OpsSignal) {
			s.publishOpsEvent(opsEvent{Type: "signal.changed", Root: signal.ProjectRoot})
			_ = s.opsStore.AppendSignal(signal)
			if signal.Kind == "process_exit" || signal.Status == "error" {
				s.alerts.Notify(context.Background(), agent.Alert{ID: "signal-" + signal.ProjectRoot + "-" + signal.ProcessName, Severity: "high", Kind: signal.Kind, ProjectRoot: signal.ProjectRoot, Message: signal.Message, Timestamp: signal.Timestamp})
			}
			if signal.Kind == "process_exit" || signal.Status == "error" {
				event := agent.ErrorEvent{ProjectRoot: signal.ProjectRoot, ProcessName: signal.ProcessName, Timestamp: signal.Timestamp, RawMessage: signal.Kind + ": " + signal.Message}
				if incident, fresh, err := aggregator.Ingest(event); err == nil && fresh && s.opsOrchestrator != nil {
					_, _, _ = s.opsOrchestrator.Analyze(incident.ID)
				}
			}
		},
		OnIncident: func(incident agent.Incident, _ bool) {
			s.publishOpsEvent(opsEvent{Type: "incident.changed", Root: incident.ProjectRoot})
			if s.opsPaused {
				incident.Status = agent.IncidentTodo
				incident.Decision, incident.DecisionReason, incident.Updated = "create_todo", "全局 AI 运维已暂停，等待人工处理", time.Now()
				_ = s.opsStore.SaveIncident(incident)
				return
			}
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
				updated, decision, _ := s.opsOrchestrator.Analyze(incident.ID)
				if decision.RequiresHuman || decision.Action == "create_todo" || decision.Action == "escalate" {
					s.alerts.Notify(context.Background(), agent.Alert{ID: "alert-" + updated.ID, Severity: updated.Severity, Kind: "incident", ProjectRoot: updated.ProjectRoot, IncidentID: updated.ID, Message: decision.Reason, Fingerprint: updated.Fingerprint, Timestamp: time.Now()})
				}
				return
			}
			incident.Status = agent.IncidentTriaged
			incident.Decision = "create_todo"
			incident.DecisionReason = "运维编排器未初始化"
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

func (s *server) opsEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	if !s.requireOpsRole(w, r, "viewer", "ops.events", "ops") {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE 不受支持。")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if s.opsEvents == nil {
		writeError(w, http.StatusServiceUnavailable, "运维事件流尚未初始化。")
		return
	}
	sub := s.opsEvents.subscribe()
	defer s.opsEvents.unsubscribe(sub)
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event := <-sub:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) opsHandler(w http.ResponseWriter, r *http.Request) {
	if s.opsStore == nil {
		writeError(w, http.StatusServiceUnavailable, "运维中心尚未初始化")
		return
	}
	if r.Method != http.MethodGet {
		defer func() {
			s.publishOpsEvent(opsEvent{Type: "ops.changed", Root: r.URL.Query().Get("root")})
		}()
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
	if path == "overview" && r.Method == http.MethodGet {
		metrics, err := s.opsStore.Metrics()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		policies, _ := s.opsStore.ListPolicies()
		writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics, "policies": policies, "paused": s.opsPaused, "nodeId": s.nodeID})
		return
	}
	if path == "metrics/prometheus" && r.Method == http.MethodGet {
		metrics, err := s.opsStore.Metrics()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = fmt.Fprintf(w, "incident_total %d\nincident_deduplicated_total %d\nai_wakeup_total %d\nopen_todo_total %d\nmaintenance_runs_total %d\nmaintenance_success_total %d\nmaintenance_failure_total %d\nmaintenance_rollback_total %d\npm2_action_failure_total %d\nbudget_exhausted_total %d\nincident_resolved_total %d\nincident_mttr_seconds %f\nalert_delivery_total %d\nalert_delivery_failure_total %d\nlease_takeover_total %d\nrecovery_conflict_total %d\n", metrics.Incidents, metrics.IncidentDeduplicated, metrics.AIWakeups, metrics.OpenTodos, metrics.MaintenanceRuns, metrics.AutoFixSuccess, metrics.MaintenanceFailures, metrics.Rollbacks, metrics.PM2ActionFailures, metrics.BudgetExhausted, metrics.Resolved, metrics.AverageRecoverySecs, metrics.AlertDeliveryTotal, metrics.AlertDeliveryFailures, metrics.LeaseTakeovers, metrics.RecoveryConflicts)
		return
	}
	if path == "signals" && r.Method == http.MethodGet {
		signals, err := s.opsStore.ListSignals()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, signals)
		return
	}
	if path == "audit" && r.Method == http.MethodGet {
		if !s.requireOpsRole(w, r, "viewer", "audit.read", "ops") {
			return
		}
		items, err := s.opsStore.ListAudit()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if path == "roles" && r.Method == http.MethodGet {
		if !s.requireOpsRole(w, r, "viewer", "roles.read", "roles") {
			return
		}
		items, err := s.opsStore.ListRoles()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if path == "roles" && r.Method == http.MethodPatch {
		if !s.requireOpsRole(w, r, "admin", "roles.update", "roles") {
			return
		}
		var binding agent.OpsRoleBinding
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&binding) != nil || binding.Account == "" {
			writeError(w, 400, "角色配置无效")
			return
		}
		switch binding.Role {
		case "viewer", "operator", "approver", "admin":
		default:
			writeError(w, 400, "角色无效")
			return
		}
		binding.Updated = time.Now()
		if err := s.opsStore.SaveRole(binding); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, binding)
		return
	}
	if path == "alerts" && r.Method == http.MethodGet {
		items, err := s.opsStore.ListAlerts()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
		return
	}
	if path == "budget" && r.Method == http.MethodGet {
		root := r.URL.Query().Get("root")
		if root == "" {
			writeError(w, 400, "缺少 root")
			return
		}
		budget, err := s.opsStore.GetBudget(root)
		if err != nil {
			writeError(w, 404, "项目预算不存在")
			return
		}
		writeJSON(w, 200, budget)
		return
	}
	if path == "budget/reset" && r.Method == http.MethodPost {
		var input struct {
			Root string `json:"root"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input) != nil || input.Root == "" {
			writeError(w, 400, "缺少 root")
			return
		}
		if err := s.opsStore.ResetBudget(input.Root); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		budget, _ := s.opsStore.GetBudget(input.Root)
		writeJSON(w, 200, budget)
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
		if !s.requireOpsRole(w, r, "admin", "project."+parts[2], parts[1]) {
			return
		}
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
		if !s.requireOpsRole(w, r, "admin", "policy.update", "policy") {
			return
		}
		var policy agent.OpsPolicy
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&policy) != nil || policy.ProjectRoot == "" {
			writeError(w, 400, "策略格式无效")
			return
		}
		if policy.Mode != "off" && policy.Mode != "observe" && policy.Mode != "canary" && policy.Mode != "auto" && policy.Mode != "strict" {
			writeError(w, 400, "运维模式无效")
			return
		}
		if (policy.Mode == "auto" || policy.Mode == "canary") && !policy.AutoAllowed {
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
		if !s.requireOpsRole(w, r, "operator", "maintenance."+parts[2], parts[1]) {
			return
		}
		run, err := s.opsStore.GetMaintenance(parts[1])
		if err != nil {
			writeError(w, 404, "维护记录不存在")
			return
		}
		switch parts[2] {
		case "observe":
			if s.opsOrchestrator == nil {
				writeError(w, 503, "运维编排器尚未初始化")
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
		case "takeover":
			run.ApprovalSource = "human"
			run.Status = "human_review"
		case "stop":
			run.Status, run.Error = "cancelled", "人工停止维护任务"
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
			if parts[2] == "analyze" || parts[2] == "dry-run" {
				if !s.requireOpsRole(w, r, "operator", "incident."+parts[2], incident.ID) {
					return
				}
			}
			if parts[2] == "approve" || parts[2] == "approve-once" {
				if !s.requireOpsRole(w, r, "approver", "incident."+parts[2], incident.ID) {
					return
				}
			}
			if parts[2] == "retry" || parts[2] == "silence" || parts[2] == "resume" || parts[2] == "todo" {
				if !s.requireOpsRole(w, r, "operator", "incident."+parts[2], incident.ID) {
					return
				}
			}
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
				if s.opsPaused {
					writeError(w, http.StatusConflict, "全局 AI 运维已暂停")
					return
				}
				if s.opsOrchestrator == nil {
					writeError(w, 503, "运维编排器尚未初始化")
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
				if s.opsPaused {
					writeError(w, http.StatusConflict, "全局 AI 运维已暂停")
					return
				}
				if s.opsOrchestrator == nil {
					writeError(w, 503, "运维编排器尚未初始化")
					return
				}
				if _, budgetErr := s.opsStore.ConsumeBudget(incident.ProjectRoot, 0, 0, 1); budgetErr != nil {
					writeError(w, 429, budgetErr.Error())
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
				if s.opsPaused {
					writeError(w, http.StatusConflict, "全局 AI 运维已暂停")
					return
				}
				if s.opsOrchestrator == nil {
					writeError(w, 503, "运维编排器尚未初始化")
					return
				}
				updated, approveErr := s.opsOrchestrator.Approve(incident.ID)
				if approveErr != nil {
					writeError(w, 409, approveErr.Error())
					return
				}
				writeJSON(w, 202, updated)
				return
			case "dry-run":
				policy, _ := s.opsStore.GetPolicy(incident.ProjectRoot)
				decision := agent.DecideAutoFix(incident, policy)
				writeJSON(w, http.StatusOK, map[string]any{"incident": incident, "decision": decision, "dryRun": true})
				return
			case "approve-once":
				if s.opsPaused {
					writeError(w, http.StatusConflict, "全局 AI 运维已暂停")
					return
				}
				if s.opsOrchestrator == nil {
					writeError(w, 503, "运维编排器尚未初始化")
					return
				}
				updated, approveErr := s.opsOrchestrator.Approve(incident.ID)
				if approveErr != nil {
					writeError(w, 409, approveErr.Error())
					return
				}
				returnJSON := map[string]any{"incident": updated, "approvalSource": "human"}
				writeJSON(w, http.StatusAccepted, returnJSON)
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
			if !s.requireOpsRole(w, r, "operator", "todo.update", todo.ID) {
				return
			}
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
	if len(parts) == 2 && parts[0] == "alerts" {
		record, err := s.opsStore.GetAlert(parts[1])
		if err != nil {
			writeError(w, 404, "告警不存在")
			return
		}
		if r.Method == http.MethodPatch || r.Method == http.MethodPost {
			if !s.requireOpsRole(w, r, "operator", "alert.update", record.ID) {
				return
			}
			var update struct {
				Status         string `json:"status"`
				SilenceMinutes int    `json:"silenceMinutes"`
			}
			_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&update)
			if parts[0] == "alerts" && strings.HasSuffix(r.URL.Path, "/ack") {
				update.Status = "acked"
			}
			if update.Status != "" {
				record.Status = update.Status
			}
			if update.SilenceMinutes > 0 {
				record.Status = "silenced"
				record.SilencedUntil = time.Now().Add(time.Duration(update.SilenceMinutes) * time.Minute)
			}
			actor, _ := s.opsActor(r)
			record.Acknowledged, record.Updated = actor, time.Now()
			_ = s.opsStore.SaveAlert(record)
			writeJSON(w, 200, record)
			return
		}
	}
	if len(parts) == 3 && parts[0] == "alerts" && (parts[2] == "ack" || parts[2] == "silence") && r.Method == http.MethodPost {
		record, err := s.opsStore.GetAlert(parts[1])
		if err != nil {
			writeError(w, 404, "告警不存在")
			return
		}
		if !s.requireOpsRole(w, r, "operator", "alert."+parts[2], record.ID) {
			return
		}
		if parts[2] == "ack" {
			record.Status = "acked"
		} else {
			record.Status = "silenced"
			record.SilencedUntil = time.Now().Add(time.Hour)
		}
		record.Updated = time.Now()
		_ = s.opsStore.SaveAlert(record)
		writeJSON(w, 200, record)
		return
	}
	if (path == "monitor/pause" || path == "monitor/emergency-stop") && r.Method == http.MethodPost {
		if !s.requireOpsRole(w, r, "operator", path, "monitor") {
			return
		}
		s.opsPaused = true
		if s.opsMonitor != nil {
			_ = s.opsMonitor.Stop()
		}
		writeJSON(w, 200, map[string]any{"paused": true})
		return
	}
	if path == "monitor/resume" && r.Method == http.MethodPost {
		if !s.requireOpsRole(w, r, "operator", path, "monitor") {
			return
		}
		s.opsPaused = false
		if s.opsMonitor != nil {
			_ = s.opsMonitor.Start(context.Background())
		}
		writeJSON(w, 200, map[string]any{"paused": false})
		return
	}
	writeError(w, 404, "运维操作不存在")
}

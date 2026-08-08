package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OpsOrchestrator coordinates the policy decision and the side effects. The
// web layer supplies guarded PM2/task callbacks, keeping this package safe to
// test without spawning processes or calling an AI provider.
type OpsOrchestrator struct {
	Store    *OpsStore
	Policy   func(string) (OpsPolicy, error)
	StartFix func(Incident, AutoFixDecision) (string, error)
	PM2      func(string, string) (string, error)
	AI       func(Incident, OpsPolicy) (AutoFixDecision, error)
	Now      func() time.Time
}

func (o *OpsOrchestrator) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *OpsOrchestrator) Analyze(id string) (Incident, AutoFixDecision, error) {
	if o == nil || o.Store == nil {
		return Incident{}, AutoFixDecision{}, errors.New("AI 运维编排器未初始化")
	}
	incident, err := o.Store.GetIncident(id)
	if err != nil {
		return Incident{}, AutoFixDecision{}, err
	}
	policy := OpsPolicy{ProjectRoot: incident.ProjectRoot, Mode: "observe"}
	if o.Policy != nil {
		if value, policyErr := o.Policy(incident.ProjectRoot); policyErr == nil {
			policy = value
		}
	}
	decision := DecideAutoFix(incident, policy)
	if o.AI != nil {
		if candidate, aiErr := o.AI(incident, policy); aiErr == nil && validOpsDecision(candidate) && decision.Risk != "high" && policy.Mode != "off" && policy.Mode != "observe" && opsDecisionCanNarrow(decision.Action, candidate.Action) && !(candidate.Action == "auto_fix" && !policy.AllowCodeChanges) {
			decision = candidate
		}
	}
	incident.Decision, incident.DecisionReason, incident.Updated = decision.Action, decision.Reason, o.now()
	if decision.Action == "create_todo" {
		incident.Status = IncidentTodo
	} else if decision.Action == "observe_only" {
		incident.Status = IncidentObserving
	} else if decision.Action == "auto_fix" || decision.Action == "restart_process" {
		incident.Status = IncidentInvestigating
	}
	if err := o.Store.SaveIncident(incident); err != nil {
		return incident, decision, err
	}
	if decision.Action == "create_todo" {
		_ = o.createTodo(incident, decision)
		return incident, decision, nil
	}
	if decision.Action == "restart_process" {
		run := MaintenanceRun{ID: fmt.Sprintf("maint-%d", o.now().UnixNano()), IncidentID: incident.ID, Decision: decision, Status: "running", Created: o.now()}
		if o.PM2 == nil {
			run.Status, run.Error = "failed", "PM2 控制未配置"
		} else {
			output, actionErr := o.PM2(incident.ProjectRoot, "pm2-restart")
			run.PM2Actions = []string{"pm2-restart"}
			if actionErr != nil {
				run.Status, run.Error = "failed", actionErr.Error()
			} else {
				run.Status, run.VerificationOutput = "observing", output
				run.ObservationStarted = o.now()
				run.ObservationUntil = run.ObservationStarted.Add(time.Duration(policy.ObservationMinutes) * time.Minute)
				incident.Status = IncidentObserving
			}
		}
		_ = o.Store.SaveMaintenance(run)
		_ = o.Store.SaveIncident(incident)
		return incident, decision, nil
	}
	if decision.Action == "auto_fix" && o.StartFix != nil {
		run := MaintenanceRun{ID: fmt.Sprintf("maint-%d", o.now().UnixNano()), IncidentID: incident.ID, Decision: decision, Status: "queued", Created: o.now()}
		taskID, startErr := o.StartFix(incident, decision)
		if startErr != nil {
			run.Status, run.Error = "failed", startErr.Error()
			_ = o.createTodo(incident, decision)
		} else {
			run.TaskID, run.Status = taskID, "fixing"
			incident.LastTaskID, incident.Status = taskID, IncidentFixing
		}
		_ = o.Store.SaveMaintenance(run)
		_ = o.Store.SaveIncident(incident)
	}
	return incident, decision, nil
}

func opsDecisionCanNarrow(base, candidate string) bool {
	if candidate == "create_todo" || candidate == "observe_only" || candidate == "escalate" {
		return true
	}
	return base == candidate
}

func (o *OpsOrchestrator) createTodo(incident Incident, decision AutoFixDecision) error {
	todo := OpsTodo{ID: "todo-" + incident.ID, IncidentID: incident.ID, ProjectRoot: incident.ProjectRoot, Title: "处理：" + incident.ProcessName, Summary: incident.Sample, Severity: incident.Severity, Reason: decision.Reason, SuggestedPlan: decision.Plan, Status: "open", Created: o.now(), Updated: o.now()}
	return o.Store.SaveTodo(todo)
}

func validOpsDecision(decision AutoFixDecision) bool {
	switch decision.Action {
	case "observe_only", "restart_process", "auto_fix", "create_todo", "escalate":
		return true
	default:
		return false
	}
}

func ParseAutoFixDecision(raw string) (AutoFixDecision, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```")
		if idx := strings.IndexByte(raw, '\n'); idx >= 0 {
			raw = raw[idx+1:]
		}
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	}
	var decision AutoFixDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return decision, err
	}
	if !validOpsDecision(decision) {
		return decision, errors.New("AI 运维决策动作无效")
	}
	return decision, nil
}

func (o *OpsOrchestrator) Observe(id string) error {
	incident, err := o.Store.GetIncident(id)
	if err != nil {
		return err
	}
	if incident.Status != IncidentObserving {
		return fmt.Errorf("事件当前不在观察状态：%s", incident.Status)
	}
	window := 5 * time.Minute
	if o.Policy != nil {
		if policy, policyErr := o.Policy(incident.ProjectRoot); policyErr == nil && policy.ObservationMinutes > 0 {
			window = time.Duration(policy.ObservationMinutes) * time.Minute
		}
	}
	if o.now().Before(incident.LastSeen.Add(window)) {
		return errors.New("观察窗口尚未结束")
	}
	incident.Status, incident.Updated = IncidentResolved, o.now()
	if err := o.Store.SaveIncident(incident); err != nil {
		return err
	}
	items, _ := o.Store.ListMaintenance()
	for _, item := range items {
		if item.IncidentID == id && item.Status == "observing" {
			item.Status = "resolved"
			now := o.now()
			item.Finished = &now
			_ = o.Store.SaveMaintenance(item)
		}
	}
	return nil
}

func (o *OpsOrchestrator) Approve(id string) (Incident, error) {
	incident, err := o.Store.GetIncident(id)
	if err != nil {
		return Incident{}, err
	}
	decision := DecideAutoFix(incident, OpsPolicy{Mode: "auto", AllowCodeChanges: true, VerificationCommand: "approved by operator"})
	if decision.Action == "create_todo" {
		return incident, errors.New("该事件仍被安全策略阻止")
	}
	if o.StartFix == nil {
		return incident, errors.New("自动修复执行器未配置")
	}
	taskID, err := o.StartFix(incident, decision)
	if err != nil {
		return incident, err
	}
	incident.Status, incident.Decision, incident.LastTaskID, incident.Updated = IncidentFixing, "auto_fix_approved", taskID, o.now()
	if err := o.Store.SaveIncident(incident); err != nil {
		return incident, err
	}
	return incident, nil
}

func IsAllowedPM2Action(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "pm2-status", "pm2-logs", "pm2-restart", "pm2-reload":
		return true
	default:
		return false
	}
}

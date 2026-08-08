package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type IncidentStatus string

const (
	IncidentDetected      IncidentStatus = "detected"
	IncidentGrouped       IncidentStatus = "grouped"
	IncidentTriaged       IncidentStatus = "triaged"
	IncidentInvestigating IncidentStatus = "investigating"
	IncidentFixing        IncidentStatus = "fixing"
	IncidentVerifying     IncidentStatus = "verifying"
	IncidentObserving     IncidentStatus = "observing"
	IncidentResolved      IncidentStatus = "resolved"
	IncidentTodo          IncidentStatus = "todo"
	IncidentSilenced      IncidentStatus = "silenced"
)

type ErrorEvent struct {
	ID          string    `json:"id"`
	ProjectRoot string    `json:"projectRoot"`
	ProcessName string    `json:"processName"`
	Timestamp   time.Time `json:"timestamp"`
	RawMessage  string    `json:"rawMessage"`
	Normalized  string    `json:"normalized"`
	Fingerprint string    `json:"fingerprint"`
	File        string    `json:"file,omitempty"`
	Line        int       `json:"line,omitempty"`
	Stack       string    `json:"stack,omitempty"`
}

type OpsSignal struct {
	ProjectRoot string    `json:"projectRoot"`
	ProcessName string    `json:"processName"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	Timestamp   time.Time `json:"timestamp"`
}

type LogCursor struct {
	ProjectRoot string    `json:"projectRoot"`
	ProcessName string    `json:"processName"`
	LogPath     string    `json:"logPath,omitempty"`
	Device      int64     `json:"device,omitempty"`
	Inode       uint64    `json:"inode,omitempty"`
	Offset      int64     `json:"offset"`
	WindowHash  string    `json:"windowHash"`
	Updated     time.Time `json:"updated"`
}

type Incident struct {
	ID             string         `json:"id"`
	ProjectRoot    string         `json:"projectRoot"`
	ProcessName    string         `json:"processName"`
	Fingerprint    string         `json:"fingerprint"`
	Status         IncidentStatus `json:"status"`
	Severity       string         `json:"severity"`
	Occurrences    int            `json:"occurrences"`
	FirstSeen      time.Time      `json:"firstSeen"`
	LastSeen       time.Time      `json:"lastSeen"`
	LastTaskID     string         `json:"lastTaskId,omitempty"`
	TodoID         string         `json:"todoId,omitempty"`
	Decision       string         `json:"decision,omitempty"`
	DecisionReason string         `json:"decisionReason,omitempty"`
	Sample         string         `json:"sample,omitempty"`
	File           string         `json:"file,omitempty"`
	Line           int            `json:"line,omitempty"`
	Stack          string         `json:"stack,omitempty"`
	Updated        time.Time      `json:"updated"`
}

type OpsPolicy struct {
	ProjectRoot         string    `json:"projectRoot"`
	Mode                string    `json:"mode"` // off/observe/canary/auto/strict
	AutoAllowed         bool      `json:"autoAllowed"`
	AllowCodeChanges    bool      `json:"allowCodeChanges"`
	AllowPM2Control     bool      `json:"allowPm2Control"`
	MaxModifiedFiles    int       `json:"maxModifiedFiles"`
	MaxPM2Actions       int       `json:"maxPm2Actions"`
	ObservationMinutes  int       `json:"observationMinutes"`
	FailureCircuitBreak int       `json:"failureCircuitBreak"`
	TokenBudget         int       `json:"tokenBudget"`
	UsedTokens          int       `json:"usedTokens"`
	UsedPM2Actions      int       `json:"usedPm2Actions"`
	RetryCount          int       `json:"retryCount"`
	FailureCount        int       `json:"failureCount"`
	VerificationCommand string    `json:"verificationCommand,omitempty"`
	Updated             time.Time `json:"updated"`
	Version             int       `json:"version"`
	SingleApproval      bool      `json:"singleApproval"`
}

type OpsBudget struct {
	TokenLimit      int `json:"tokenLimit"`
	UsedTokens      int `json:"usedTokens"`
	MaxRetries      int `json:"maxRetries"`
	RetryCount      int `json:"retryCount"`
	MaxPM2Actions   int `json:"maxPm2Actions"`
	UsedPM2Actions  int `json:"usedPm2Actions"`
	MaxChangedFiles int `json:"maxChangedFiles"`
}

type AutoFixDecision struct {
	Action         string   `json:"action"`
	Confidence     float64  `json:"confidence"`
	Severity       string   `json:"severity"`
	Risk           string   `json:"risk"`
	Reason         string   `json:"reason"`
	AllowedActions []string `json:"allowedActions,omitempty"`
	Plan           TaskPlan `json:"plan"`
	RequiresHuman  bool     `json:"requiresHuman"`
}

type MaintenanceRun struct {
	ID                 string           `json:"id"`
	IncidentID         string           `json:"incidentId"`
	TaskID             string           `json:"taskId,omitempty"`
	Decision           AutoFixDecision  `json:"decision"`
	PM2Actions         []string         `json:"pm2Actions,omitempty"`
	ModifiedFiles      []string         `json:"modifiedFiles,omitempty"`
	VerificationOutput string           `json:"verificationOutput,omitempty"`
	ObservationStarted time.Time        `json:"observationStarted,omitempty"`
	ObservationUntil   time.Time        `json:"observationUntil,omitempty"`
	Status             string           `json:"status"`
	RollbackPerformed  bool             `json:"rollbackPerformed"`
	Error              string           `json:"error,omitempty"`
	Created            time.Time        `json:"created"`
	Finished           *time.Time       `json:"finished,omitempty"`
	RetryCount         int              `json:"retryCount"`
	PM2ActionCount     int              `json:"pm2ActionCount"`
	TokenUsage         int              `json:"tokenUsage"`
	DurationSeconds    int64            `json:"durationSeconds"`
	Score              MaintenanceScore `json:"score"`
	ApprovalSource     string           `json:"approvalSource,omitempty"` // ai/human/automatic
	NodeID             string           `json:"nodeId,omitempty"`
}

type OpsTodo struct {
	ID            string    `json:"id"`
	IncidentID    string    `json:"incidentId"`
	ProjectRoot   string    `json:"projectRoot"`
	Title         string    `json:"title"`
	Summary       string    `json:"summary"`
	Severity      string    `json:"severity"`
	Reason        string    `json:"reason"`
	SuggestedPlan TaskPlan  `json:"suggestedPlan"`
	Status        string    `json:"status"`
	Assignee      string    `json:"assignee,omitempty"`
	Created       time.Time `json:"created"`
	Updated       time.Time `json:"updated"`
}

type OpsMetrics struct {
	Incidents             int     `json:"incidents"`
	OpenTodos             int     `json:"openTodos"`
	MaintenanceRuns       int     `json:"maintenanceRuns"`
	AutoFixSuccess        int     `json:"autoFixSuccess"`
	Rollbacks             int     `json:"rollbacks"`
	PM2Failures           int     `json:"pm2Failures"`
	Resolved              int     `json:"resolved"`
	AverageRecoverySecs   float64 `json:"averageRecoverySecs"`
	SeenEventFingerprints int     `json:"seenEventFingerprints"`
	IncidentDeduplicated  int     `json:"incidentDeduplicated"`
	AIWakeups             int     `json:"aiWakeups"`
	MaintenanceFailures   int     `json:"maintenanceFailures"`
	PM2ActionFailures     int     `json:"pm2ActionFailures"`
	BudgetExhausted       int     `json:"budgetExhausted"`
	AlertDeliveryTotal    int     `json:"alertDeliveryTotal"`
	AlertDeliveryFailures int     `json:"alertDeliveryFailures"`
	LeaseTakeovers        int     `json:"leaseTakeovers"`
	RecoveryConflicts     int     `json:"recoveryConflicts"`
}

var volatileLogParts = regexp.MustCompile(`(?i)(\b(?:request|user|trace|correlation|session)?[_ -]?id\s*[=:]\s*)[^\s,;]+|(\buser\s*[=:]\s*)[^\s,;]+|\b[0-9a-f]{8}-[0-9a-f-]{27,}\b|\b\d{2,}\b|\b(?:\d{1,3}\.){3}\d{1,3}:\d+\b`)
var fileLinePattern = regexp.MustCompile(`([[:alnum:]_./-]+\.[[:alnum:]]+):(\d+)(?::\d+)?`)

func NormalizeErrorMessage(raw string) string {
	message := strings.TrimSpace(raw)
	message = volatileLogParts.ReplaceAllString(message, `$1<var>`)
	message = regexp.MustCompile(`\s+`).ReplaceAllString(message, " ")
	return message
}

func ErrorFingerprint(projectRoot, process, raw, stack string) string {
	normalized := NormalizeErrorMessage(raw)
	file, line := ExtractErrorLocation(raw + "\n" + stack)
	key := strings.Join([]string{filepath.Clean(projectRoot), process, normalized, file, fmt.Sprint(line)}, "\x00")
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:12])
}

func ExtractErrorLocation(text string) (string, int) {
	match := fileLinePattern.FindStringSubmatch(text)
	if len(match) != 3 {
		return "", 0
	}
	if match[1] == "" || match[1][0] >= '0' && match[1][0] <= '9' {
		return "", 0
	}
	var line int
	_, _ = fmt.Sscanf(match[2], "%d", &line)
	return match[1], line
}

type OpsStore struct {
	dir string
	mu  sync.Mutex
}

func NewOpsStoreAt(dir string) *OpsStore { return &OpsStore{dir: dir} }
func (s *OpsStore) ensure() error        { return os.MkdirAll(filepath.Join(s.dir, "events"), 0700) }
func (s *OpsStore) incidentPath(id string) string {
	return filepath.Join(s.dir, "incident-"+id+".json")
}
func (s *OpsStore) todoPath(id string) string { return filepath.Join(s.dir, "todo-"+id+".json") }
func (s *OpsStore) runPath(id string) string  { return filepath.Join(s.dir, "maintenance-"+id+".json") }
func (s *OpsStore) policyPath(root string) string {
	h := sha256.Sum256([]byte(filepath.Clean(root)))
	return filepath.Join(s.dir, "policy-"+hex.EncodeToString(h[:12])+".json")
}
func (s *OpsStore) seenPath() string { return filepath.Join(s.dir, "seen-events.json") }
func (s *OpsStore) MarkEventSeen(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return false, err
	}
	seen := map[string]time.Time{}
	_ = readJSONFile(s.seenPath(), &seen)
	if _, ok := seen[key]; ok {
		return true, nil
	}
	seen[key] = time.Now()
	// Keep the cursor bounded while retaining enough history for the PM2 log
	// window; old entries cannot be emitted again by the monitor.
	if len(seen) > 5000 {
		for item, at := range seen {
			if time.Since(at) > 24*time.Hour {
				delete(seen, item)
			}
		}
	}
	return false, atomicJSONFile(s.seenPath(), seen)
}
func (s *OpsStore) SaveIncident(item Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	return atomicJSONFile(s.incidentPath(item.ID), item)
}

func (s *OpsStore) AppendEvent(incidentID string, event ErrorEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	path := filepath.Join(s.dir, "events", incidentID+".jsonl")
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}
func (s *OpsStore) AppendSignal(signal OpsSignal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	path := filepath.Join(s.dir, "signals.jsonl")
	data, err := json.Marshal(signal)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}
func (s *OpsStore) ListSignals() ([]OpsSignal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(s.dir, "signals.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return []OpsSignal{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []OpsSignal
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var signal OpsSignal
		if json.Unmarshal([]byte(line), &signal) == nil {
			out = append(out, signal)
		}
	}
	return out, nil
}

func (s *OpsStore) logCursorPath(projectRoot, processName string) string {
	key := sha256Hex(filepath.Clean(projectRoot) + "\x00" + processName)
	return filepath.Join(s.dir, "cursor-"+key+".json")
}

func (s *OpsStore) SaveLogCursor(cursor LogCursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	if cursor.Updated.IsZero() {
		cursor.Updated = time.Now()
	}
	return atomicJSONFile(s.logCursorPath(cursor.ProjectRoot, cursor.ProcessName), cursor)
}

func (s *OpsStore) GetLogCursor(projectRoot, processName string) (LogCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cursor LogCursor
	err := readJSONFile(s.logCursorPath(projectRoot, processName), &cursor)
	return cursor, err
}
func (s *OpsStore) ListEvents(incidentID string) ([]ErrorEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, "events", incidentID+".jsonl")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []ErrorEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	var events []ErrorEvent
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event ErrorEvent
		if json.Unmarshal([]byte(line), &event) == nil {
			events = append(events, event)
		}
	}
	return events, nil
}
func (s *OpsStore) GetIncident(id string) (Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var item Incident
	err := readJSONFile(s.incidentPath(id), &item)
	return item, err
}
func (s *OpsStore) ListIncidents() ([]Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return listOpsJSON[Incident](s.dir, "incident-")
}
func (s *OpsStore) SaveTodo(item OpsTodo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	return atomicJSONFile(s.todoPath(item.ID), item)
}
func (s *OpsStore) GetTodo(id string) (OpsTodo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var item OpsTodo
	err := readJSONFile(s.todoPath(id), &item)
	return item, err
}
func (s *OpsStore) ListTodos() ([]OpsTodo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return listOpsJSON[OpsTodo](s.dir, "todo-")
}
func (s *OpsStore) SaveMaintenance(item MaintenanceRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	return atomicJSONFile(s.runPath(item.ID), item)
}
func (s *OpsStore) GetMaintenance(id string) (MaintenanceRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var item MaintenanceRun
	err := readJSONFile(s.runPath(id), &item)
	return item, err
}
func (s *OpsStore) ListMaintenance() ([]MaintenanceRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return listOpsJSON[MaintenanceRun](s.dir, "maintenance-")
}
func (s *OpsStore) SavePolicy(policy OpsPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	return atomicJSONFile(s.policyPath(policy.ProjectRoot), policy)
}
func (s *OpsStore) GetPolicy(root string) (OpsPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var policy OpsPolicy
	err := readJSONFile(s.policyPath(root), &policy)
	return policy, err
}
func (s *OpsStore) ListPolicies() ([]OpsPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return listOpsJSON[OpsPolicy](s.dir, "policy-")
}
func (s *OpsStore) GetBudget(root string) (OpsBudget, error) {
	policy, err := s.GetPolicy(root)
	if err != nil {
		return OpsBudget{}, err
	}
	return OpsBudget{TokenLimit: policy.TokenBudget, UsedTokens: policy.UsedTokens, MaxRetries: policy.FailureCircuitBreak, RetryCount: policy.RetryCount, MaxPM2Actions: policy.MaxPM2Actions, UsedPM2Actions: policy.UsedPM2Actions, MaxChangedFiles: policy.MaxModifiedFiles}, nil
}

// ConsumeBudget updates all counters under one store lock. A failed consume
// never partially increments any counter.
func (s *OpsStore) ConsumeBudget(root string, tokens, pm2Actions, retries int) (OpsBudget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var policy OpsPolicy
	if err := readJSONFile(s.policyPath(root), &policy); err != nil {
		return OpsBudget{}, err
	}
	if policy.TokenBudget > 0 && policy.UsedTokens+tokens > policy.TokenBudget {
		return OpsBudget{}, errors.New("AI Token 预算已耗尽")
	}
	if policy.MaxPM2Actions > 0 && policy.UsedPM2Actions+pm2Actions > policy.MaxPM2Actions {
		return OpsBudget{}, errors.New("PM2 操作预算已耗尽")
	}
	if policy.FailureCircuitBreak > 0 && policy.RetryCount+retries > policy.FailureCircuitBreak {
		return OpsBudget{}, errors.New("自动重试预算已耗尽")
	}
	policy.UsedTokens += tokens
	policy.UsedPM2Actions += pm2Actions
	policy.RetryCount += retries
	policy.Updated = time.Now()
	if err := atomicJSONFile(s.policyPath(root), policy); err != nil {
		return OpsBudget{}, err
	}
	return OpsBudget{TokenLimit: policy.TokenBudget, UsedTokens: policy.UsedTokens, MaxRetries: policy.FailureCircuitBreak, RetryCount: policy.RetryCount, MaxPM2Actions: policy.MaxPM2Actions, UsedPM2Actions: policy.UsedPM2Actions, MaxChangedFiles: policy.MaxModifiedFiles}, nil
}
func (s *OpsStore) ResetBudget(root string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var policy OpsPolicy
	if err := readJSONFile(s.policyPath(root), &policy); err != nil {
		return err
	}
	policy.UsedTokens, policy.UsedPM2Actions, policy.RetryCount, policy.Updated = 0, 0, 0, time.Now()
	return atomicJSONFile(s.policyPath(root), policy)
}
func (s *OpsStore) UpdateMaintenanceByTask(taskID, status, lastError string) error {
	items, err := s.ListMaintenance()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.TaskID != taskID {
			continue
		}
		item.Status = status
		item.Error = lastError
		if status == "completed" || status == "failed" || status == "cancelled" {
			now := time.Now()
			item.Finished = &now
		}
		if err := s.SaveMaintenance(item); err != nil {
			return err
		}
		if incident, incidentErr := s.GetIncident(item.IncidentID); incidentErr == nil {
			switch status {
			case "completed":
				incident.Status = IncidentObserving
			case "failed", "cancelled":
				incident.Status = IncidentTodo
			}
			incident.Updated = time.Now()
			_ = s.SaveIncident(incident)
		}
		return nil
	}
	return nil
}
func (s *OpsStore) ReconcileMaintenance(tasks []AgentTask) error {
	byID := make(map[string]AgentTask, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	runs, err := s.ListMaintenance()
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.Status != "fixing" && run.Status != "verifying" && run.Status != "observing" && run.Status != "queued" {
			continue
		}
		task, ok := byID[run.TaskID]
		if run.TaskID == "" || !ok {
			run.Status, run.Error = "recovery_required", "关联任务不存在，等待人工恢复"
		} else {
			switch task.Status {
			case TaskCompleted:
				run.Status = "observing"
			case TaskFailed:
				run.Status, run.Error = "failed", task.LastError
			case TaskCancelled:
				run.Status, run.Error = "cancelled", task.LastError
			case TaskPaused:
				run.Status = "paused"
			}
		}
		if err := s.SaveMaintenance(run); err != nil {
			return err
		}
	}
	return nil
}
func (s *OpsStore) Metrics() (OpsMetrics, error) {
	incidents, err := s.ListIncidents()
	if err != nil {
		return OpsMetrics{}, err
	}
	todos, err := s.ListTodos()
	if err != nil {
		return OpsMetrics{}, err
	}
	runs, err := s.ListMaintenance()
	if err != nil {
		return OpsMetrics{}, err
	}
	metrics := OpsMetrics{Incidents: len(incidents), MaintenanceRuns: len(runs)}
	var recoveryTotal time.Duration
	for _, todo := range todos {
		if todo.Status == "open" || todo.Status == "in_progress" {
			metrics.OpenTodos++
		}
	}
	for _, incident := range incidents {
		if incident.Status == IncidentResolved {
			metrics.Resolved++
		}
	}
	for _, run := range runs {
		if run.RollbackPerformed {
			metrics.Rollbacks++
		}
		if run.Status == "completed" || run.Status == "observing" || run.Status == "resolved" {
			metrics.AutoFixSuccess++
		}
		if run.Status == "failed" {
			metrics.PM2Failures++
			metrics.MaintenanceFailures++
		}
		if run.Finished != nil {
			recoveryTotal += run.Finished.Sub(run.Created)
		}
	}
	if len(runs) > 0 {
		metrics.AverageRecoverySecs = recoveryTotal.Seconds() / float64(len(runs))
	}
	var seen map[string]time.Time
	if readJSONFile(s.seenPath(), &seen) == nil {
		metrics.SeenEventFingerprints = len(seen)
	}
	return metrics, nil
}

func listOpsJSON[T any](dir, prefix string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []T{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []T{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		var item T
		if readJSONFile(filepath.Join(dir, entry.Name()), &item) == nil {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return opsJSONTime(out[i]).After(opsJSONTime(out[j]))
	})
	return out, nil
}

func opsJSONTime[T any](item T) time.Time {
	data, _ := json.Marshal(item)
	var value map[string]any
	_ = json.Unmarshal(data, &value)
	for _, key := range []string{"updated", "created", "lastSeen"} {
		if raw, ok := value[key].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

type IncidentAggregator struct {
	store  OpsRepository
	mu     sync.Mutex
	window time.Duration
}

func NewIncidentAggregator(store OpsRepository) *IncidentAggregator {
	return &IncidentAggregator{store: store, window: 5 * time.Minute}
}
func (a *IncidentAggregator) Ingest(event ErrorEvent) (Incident, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	event.Normalized = NormalizeErrorMessage(event.RawMessage)
	if event.Fingerprint == "" {
		event.Fingerprint = ErrorFingerprint(event.ProjectRoot, event.ProcessName, event.RawMessage, event.Stack)
	}
	items, err := a.store.ListIncidents()
	if err != nil {
		return Incident{}, false, err
	}
	for _, item := range items {
		if item.ProjectRoot == event.ProjectRoot && item.ProcessName == event.ProcessName && item.Fingerprint == event.Fingerprint && item.Status != IncidentResolved && item.Status != IncidentSilenced {
			item.Occurrences++
			item.LastSeen = event.Timestamp
			item.Updated = time.Now()
			if item.Sample == "" {
				item.Sample = event.RawMessage
			}
			if item.File == "" {
				item.File, item.Line = event.File, event.Line
			}
			if item.Stack == "" {
				item.Stack = event.Stack
			}
			if err := a.store.SaveIncident(item); err != nil {
				return Incident{}, false, err
			}
			_ = a.store.AppendEvent(item.ID, event)
			return item, false, nil
		}
	}
	id := fmt.Sprintf("inc-%d", time.Now().UnixNano())
	item := Incident{ID: id, ProjectRoot: event.ProjectRoot, ProcessName: event.ProcessName, Fingerprint: event.Fingerprint, Status: IncidentDetected, Severity: "medium", Occurrences: 1, FirstSeen: event.Timestamp, LastSeen: event.Timestamp, Sample: event.RawMessage, File: event.File, Line: event.Line, Stack: event.Stack, Updated: time.Now()}
	if err := a.store.SaveIncident(item); err != nil {
		return Incident{}, false, err
	}
	_ = a.store.AppendEvent(item.ID, event)
	return item, true, nil
}

var opsHighRisk = regexp.MustCompile(`(?i)\b(password|passwd|api[_ -]?key|secret|token|credential|ssh|firewall|sudo|root|database|db|migration|drop|delete|rm\s+-rf|chmod|system account)\b`)

// DecideAutoFix is deterministic by design. A model may later enrich the
// explanation, but the safety decision remains testable when AI is offline.
func DecideAutoFix(incident Incident, policy OpsPolicy) AutoFixDecision {
	if policy.Mode == "" {
		policy.Mode = "observe"
	}
	if policy.MaxModifiedFiles <= 0 {
		policy.MaxModifiedFiles = 10
	}
	if policy.MaxPM2Actions <= 0 {
		policy.MaxPM2Actions = 3
	}
	if policy.ObservationMinutes <= 0 {
		policy.ObservationMinutes = 5
	}
	if policy.FailureCircuitBreak <= 0 {
		policy.FailureCircuitBreak = 3
	}
	text := strings.Join([]string{incident.Sample, incident.Stack, incident.File}, " ")
	decision := AutoFixDecision{Action: "create_todo", Severity: incident.Severity, Risk: "medium", Confidence: 0.45, RequiresHuman: true, AllowedActions: []string{"agent_repo_map", "agent_find_symbol", "agent_find_references", "agent_verify", "create_todo"}}
	if opsHighRisk.MatchString(text) {
		decision.Risk, decision.Reason = "high", "错误涉及凭据、数据库或系统权限，禁止自动修改"
		return decision
	}
	if policy.Mode == "off" {
		decision.Action, decision.Reason = "observe_only", "项目已关闭 运维"
		return decision
	}
	if policy.Mode == "observe" {
		decision.Action, decision.Reason = "observe_only", "项目处于观察模式"
		return decision
	}
	if policy.Mode == "strict" {
		decision.Reason = "严格确认模式要求人工批准"
		return decision
	}
	if (policy.Mode == "auto" || policy.Mode == "canary") && !policy.AutoAllowed {
		decision.Reason = "项目尚未加入自动维护白名单"
		return decision
	}
	if policy.Mode == "canary" && incident.Occurrences > 1 {
		decision.Reason = "canary 模式仅允许单次 Incident，重复事件转人工"
		return decision
	}
	if policy.AllowCodeChanges && incident.File != "" && incident.Line > 0 && policy.VerificationCommand != "" {
		decision.Action, decision.Risk, decision.Confidence, decision.RequiresHuman = "auto_fix", "low", 0.86, false
		decision.AllowedActions = append(decision.AllowedActions, "agent_edit_file", "agent_run_command", "rollback")
		decision.Reason = "错误有明确文件定位、验证命令和快照回滚能力"
		return decision
	}
	if policy.AllowPM2Control && (strings.Contains(strings.ToLower(text), "crash") || strings.Contains(strings.ToLower(text), "fatal") || strings.Contains(strings.ToLower(text), "unhandled")) {
		decision.Action, decision.Risk, decision.Confidence, decision.RequiresHuman = "restart_process", "low", 0.78, false
		decision.AllowedActions = append(decision.AllowedActions, "pm2_status", "pm2_logs", "pm2_restart", "pm2_reload")
		decision.Reason = "进程级异常适合先执行一次受控 PM2 恢复"
		return decision
	}
	decision.Reason = "缺少明确定位或验证条件，转为人工待办"
	return decision
}

func ParsePM2LogOutput(root, process, output string, now time.Time) []ErrorEvent {
	if now.IsZero() {
		now = time.Now()
	}
	var events []ErrorEvent
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || !(strings.Contains(lower, "error") || strings.Contains(lower, "exception") || strings.Contains(lower, "unhandledrejection") || strings.Contains(lower, "fatal")) {
			continue
		}
		file, lineNo := ExtractErrorLocation(trimmed)
		events = append(events, ErrorEvent{ID: fmt.Sprintf("evt-%d", now.UnixNano()), ProjectRoot: root, ProcessName: process, Timestamp: now, RawMessage: trimmed, Normalized: NormalizeErrorMessage(trimmed), File: file, Line: lineNo, Stack: trimmed})
	}
	return events
}

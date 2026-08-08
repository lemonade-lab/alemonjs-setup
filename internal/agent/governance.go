package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// OpsRepository is the persistence seam for production backends. OpsStore is
// the default JSON implementation; a database-backed implementation can be
// introduced without changing the task/incident services.
type OpsRepository interface {
	Close() error
	SaveIncident(Incident) error
	GetIncident(string) (Incident, error)
	ListIncidents() ([]Incident, error)
	SaveTodo(OpsTodo) error
	GetTodo(string) (OpsTodo, error)
	ListTodos() ([]OpsTodo, error)
	SaveMaintenance(MaintenanceRun) error
	GetMaintenance(string) (MaintenanceRun, error)
	ListMaintenance() ([]MaintenanceRun, error)
	SavePolicy(OpsPolicy) error
	GetPolicy(string) (OpsPolicy, error)
	ListPolicies() ([]OpsPolicy, error)
	MarkEventSeen(string) (bool, error)
	AppendEvent(string, ErrorEvent) error
	ListEvents(string) ([]ErrorEvent, error)
	AppendSignal(OpsSignal) error
	ListSignals() ([]OpsSignal, error)
	SaveLogCursor(LogCursor) error
	GetLogCursor(string, string) (LogCursor, error)
	GetBudget(string) (OpsBudget, error)
	ConsumeBudget(string, int, int, int) (OpsBudget, error)
	ResetBudget(string) error
	UpdateMaintenanceByTask(string, string, string) error
	ReconcileMaintenance([]AgentTask) error
	Metrics() (OpsMetrics, error)
	AppendAudit(AuditEntry) error
	ListAudit() ([]AuditEntry, error)
	SaveAlert(AlertRecord) error
	GetAlert(string) (AlertRecord, error)
	ListAlerts() ([]AlertRecord, error)
	AcquireOpsLease(string, string, time.Duration) (func(), error)
	RenewOpsLease(string, string, time.Duration) error
	SaveScore(string, MaintenanceScore) error
	SaveRole(OpsRoleBinding) error
	GetRole(string) (OpsRoleBinding, error)
	ListRoles() ([]OpsRoleBinding, error)
}

// LeaseManager is the process boundary used by monitors, schedulers and
// maintenance workers. Implementations must make acquisition and renewal
// atomic across processes.
type LeaseManager interface {
	Acquire(ctx context.Context, key, owner string, ttl time.Duration) error
	Renew(ctx context.Context, key, owner string, ttl time.Duration) error
	Release(ctx context.Context, key, owner string) error
}

// JSONOpsRepository is the development/fallback implementation. Keeping the
// alias explicit makes dependency injection and a future SQLite/PostgreSQL
// adapter source-compatible.
type JSONOpsRepository = OpsStore

// Close is a no-op for the file-backed repository and keeps lifecycle code
// backend agnostic.
func (s *OpsStore) Close() error { return nil }

type OpsLease struct {
	Key       string    `json:"key"`
	OwnerID   string    `json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
	RenewedAt time.Time `json:"renewedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type OpsRoleBinding struct {
	Account string    `json:"account"`
	Role    string    `json:"role"`
	Updated time.Time `json:"updated"`
}

type AuditEntry struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	Role      string    `json:"role"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	RequestID string    `json:"requestId,omitempty"`
	Result    string    `json:"result"`
	Reason    string    `json:"reason,omitempty"`
	Created   time.Time `json:"created"`
}

type AlertPolicy struct {
	Severity       string        `json:"severity"`
	RepeatInterval time.Duration `json:"repeatInterval"`
	SilenceMinutes int           `json:"silenceMinutes"`
	MaxRetries     int           `json:"maxRetries"`
	RetryBackoff   time.Duration `json:"retryBackoff"`
	Escalation     []string      `json:"escalation,omitempty"`
	Recovery       bool          `json:"recovery"`
}

type AlertRecord struct {
	Alert
	Status        string    `json:"status"` // open/acked/silenced/resolved
	Acknowledged  string    `json:"acknowledgedBy,omitempty"`
	SilencedUntil time.Time `json:"silencedUntil,omitempty"`
	Updated       time.Time `json:"updated"`
}

type MaintenanceScore struct {
	GoalSatisfied bool    `json:"goalSatisfied"`
	Safe          bool    `json:"safe"`
	Verified      bool    `json:"verified"`
	UnrelatedDiff bool    `json:"unrelatedDiff"`
	Score         float64 `json:"score"`
}

var leaseMu sync.Mutex

func newID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return prefix + "-" + time.Now().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(b)
}

func (s *OpsStore) governancePath(name string) string { return filepath.Join(s.dir, name) }

func (s *OpsStore) AppendAudit(entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	if entry.ID == "" {
		entry.ID = newID("audit")
	}
	if entry.Created.IsZero() {
		entry.Created = time.Now()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.governancePath("audit.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func (s *OpsStore) ListAudit() ([]AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.governancePath("audit.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return []AuditEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []AuditEntry
	for _, line := range strings.Split(string(data), "\n") {
		var item AuditEntry
		if strings.TrimSpace(line) != "" && json.Unmarshal([]byte(line), &item) == nil {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

func (s *OpsStore) SaveAlert(record AlertRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	if record.ID == "" {
		record.ID = newID("alert")
	}
	if record.Updated.IsZero() {
		record.Updated = time.Now()
	}
	return atomicJSONFile(filepath.Join(s.dir, "alert-"+record.ID+".json"), record)
}

func (s *OpsStore) GetAlert(id string) (AlertRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out AlertRecord
	err := readJSONFile(filepath.Join(s.dir, "alert-"+id+".json"), &out)
	return out, err
}

func (s *OpsStore) ListAlerts() ([]AlertRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return listOpsJSON[AlertRecord](s.dir, "alert-")
}

func (s *OpsStore) AcquireOpsLease(key, owner string, ttl time.Duration) (func(), error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(owner) == "" {
		return nil, errors.New("租约参数无效")
	}
	leaseMu.Lock()
	defer leaseMu.Unlock()
	if err := s.ensure(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, "lease-"+sha256Hex(key)+".json")
	now := time.Now()
	var current OpsLease
	if readJSONFile(path, &current) == nil && current.ExpiresAt.After(now) && current.OwnerID != owner {
		return nil, errors.New("运维执行租约已被其他实例持有")
	}
	item := OpsLease{Key: key, OwnerID: owner, CreatedAt: now, RenewedAt: now, ExpiresAt: now.Add(ttl)}
	if err := atomicJSONFile(path, item); err != nil {
		return nil, err
	}
	return func() {
		leaseMu.Lock()
		defer leaseMu.Unlock()
		var latest OpsLease
		if readJSONFile(path, &latest) == nil && latest.OwnerID == owner {
			_ = os.Remove(path)
		}
	}, nil
}

func (s *OpsStore) RenewOpsLease(key, owner string, ttl time.Duration) error {
	leaseMu.Lock()
	defer leaseMu.Unlock()
	path := filepath.Join(s.dir, "lease-"+sha256Hex(key)+".json")
	var current OpsLease
	if err := readJSONFile(path, &current); err != nil || current.OwnerID != owner {
		return errors.New("运维租约不存在或不属于当前实例")
	}
	current.ExpiresAt = time.Now().Add(ttl)
	current.RenewedAt = time.Now()
	return atomicJSONFile(path, current)
}

func sha256Hex(value string) string {
	// Keep lease filenames stable without exposing project paths.
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func (s *OpsStore) SaveScore(runID string, score MaintenanceScore) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	return atomicJSONFile(filepath.Join(s.dir, "score-"+runID+".json"), score)
}

func (s *OpsStore) SaveRole(v OpsRoleBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return err
	}
	if v.Updated.IsZero() {
		v.Updated = time.Now()
	}
	return atomicJSONFile(filepath.Join(s.dir, "role-"+sha256Hex(v.Account)+".json"), v)
}
func (s *OpsStore) GetRole(account string) (OpsRoleBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v OpsRoleBinding
	err := readJSONFile(filepath.Join(s.dir, "role-"+sha256Hex(account)+".json"), &v)
	return v, err
}
func (s *OpsStore) ListRoles() ([]OpsRoleBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return listOpsJSON[OpsRoleBinding](s.dir, "role-")
}

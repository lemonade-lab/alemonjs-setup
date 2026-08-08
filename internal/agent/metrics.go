package agent

import (
	"sync"
	"time"
)

type TaskMetrics struct {
	Created, Completed, Failed, Cancelled           int64
	ToolCalls, VerificationFailures, ReviewerBlocks int64
	TotalDuration                                   time.Duration
}

type Metrics struct {
	mu     sync.Mutex
	byTask map[string]TaskMetrics
}

func NewMetrics() *Metrics { return &Metrics{byTask: map[string]TaskMetrics{}} }
func (m *Metrics) Add(taskID string, update func(*TaskMetrics)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value := m.byTask[taskID]
	update(&value)
	m.byTask[taskID] = value
}
func (m *Metrics) Get(taskID string) (TaskMetrics, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.byTask[taskID]
	return value, ok
}

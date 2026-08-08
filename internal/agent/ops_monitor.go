package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type OpsLogSource func(context.Context) ([]ErrorEvent, error)
type IncidentHandler func(Incident, bool)

// OpsMonitor is a process-local polling loop. PM2 integration supplies a log
// source, while aggregation and persistence remain independent and testable.
type OpsMonitor struct {
	Source     OpsLogSource
	Aggregator *IncidentAggregator
	OnIncident IncidentHandler
	OnPoll     func()
	Interval   time.Duration
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	seen       map[string]time.Time
}

func (m *OpsMonitor) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return nil
	}
	if m.Interval <= 0 {
		m.Interval = 5 * time.Second
	}
	if m.Source == nil || m.Aggregator == nil {
		m.mu.Unlock()
		return nil
	}
	loopCtx, cancel := context.WithCancel(ctx)
	m.cancel, m.done = cancel, make(chan struct{})
	if m.seen == nil {
		m.seen = map[string]time.Time{}
	}
	done := m.done
	m.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(m.Interval)
		defer ticker.Stop()
		m.poll(loopCtx)
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				m.poll(loopCtx)
			}
		}
	}()
	return nil
}

func (m *OpsMonitor) poll(ctx context.Context) {
	defer func() {
		if m.OnPoll != nil {
			m.OnPoll()
		}
	}()
	events, err := m.Source(ctx)
	if err != nil {
		return
	}
	for _, event := range events {
		keyBytes := sha256.Sum256([]byte(event.ProjectRoot + "\x00" + event.ProcessName + "\x00" + event.Normalized + "\x00" + event.RawMessage))
		key := hex.EncodeToString(keyBytes[:])
		duplicate := false
		if m.Aggregator.store != nil {
			if persisted, markErr := m.Aggregator.store.MarkEventSeen(key); markErr == nil {
				duplicate = persisted
			}
		} else {
			m.mu.Lock()
			_, duplicate = m.seen[key]
			m.seen[key] = time.Now()
			m.mu.Unlock()
		}
		if duplicate {
			continue
		}
		incident, fresh, ingestErr := m.Aggregator.Ingest(event)
		if ingestErr == nil && m.OnIncident != nil && (fresh || incident.Status == IncidentObserving) {
			m.OnIncident(incident, true)
		}
	}
}

func (m *OpsMonitor) Stop() error {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.cancel, m.done = nil, nil
	m.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if done != nil {
		<-done
	}
	return nil
}

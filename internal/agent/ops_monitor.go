package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

type OpsLogSource func(context.Context) ([]ErrorEvent, error)
type OpsStreamSource func(context.Context, func(ErrorEvent)) error
type OpsSignalSource func(context.Context) ([]OpsSignal, error)
type IncidentHandler func(Incident, bool)

type LogCursorStore interface {
	SaveLogCursor(LogCursor) error
	GetLogCursor(string, string) (LogCursor, error)
}

// OpsMonitor is a process-local polling loop. PM2 integration supplies a log
// source, while aggregation and persistence remain independent and testable.
type OpsMonitor struct {
	Source       OpsLogSource
	Stream       OpsStreamSource
	Signals      OpsSignalSource
	Aggregator   *IncidentAggregator
	CursorStore  LogCursorStore
	Lease        LeaseManager
	LeaseKey     string
	LeaseOwner   string
	LeaseTTL     time.Duration
	OnIncident   IncidentHandler
	OnPoll       func()
	OnSignal     func(OpsSignal)
	AcquireLease func() (func(), error)
	RenewLease   func() error
	Interval     time.Duration
	mu           sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
	releaseLease func()
	seen         map[string]time.Time
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
	if (m.Source == nil && m.Stream == nil) || m.Aggregator == nil {
		m.mu.Unlock()
		return nil
	}
	if m.Lease != nil {
		if m.LeaseKey == "" {
			m.LeaseKey = "ops-monitor"
		}
		if m.LeaseTTL <= 0 {
			m.LeaseTTL = 45 * time.Second
		}
		if err := m.Lease.Acquire(loopContextOrBackground(ctx), m.LeaseKey, m.LeaseOwner, m.LeaseTTL); err != nil {
			m.mu.Unlock()
			return err
		}
	} else if m.AcquireLease != nil {
		release, err := m.AcquireLease()
		if err != nil {
			m.mu.Unlock()
			return err
		}
		m.releaseLease = release
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
		defer cancel()
		ticker := time.NewTicker(m.Interval)
		defer ticker.Stop()
		m.poll(loopCtx)
		if m.Stream != nil {
			go m.stream(loopCtx)
		}
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				if m.Lease != nil {
					if err := m.Lease.Renew(loopCtx, m.LeaseKey, m.LeaseOwner, m.LeaseTTL); err != nil {
						return
					}
				} else if m.RenewLease != nil {
					_ = m.RenewLease()
				}
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
	if m.Source != nil {
		events, err := m.Source(ctx)
		if err == nil {
			m.ingestEvents(events)
		}
	}
	if m.Signals != nil {
		if signals, signalErr := m.Signals(ctx); signalErr == nil {
			for _, signal := range signals {
				keyBytes := sha256.Sum256([]byte("signal\x00" + signal.ProjectRoot + "\x00" + signal.ProcessName + "\x00" + signal.Kind + "\x00" + signal.Status + "\x00" + signal.Message))
				duplicate := false
				if m.Aggregator.store != nil {
					if persisted, markErr := m.Aggregator.store.MarkEventSeen(hex.EncodeToString(keyBytes[:])); markErr == nil {
						duplicate = persisted
					}
				}
				if !duplicate && m.OnSignal != nil {
					m.OnSignal(signal)
				}
			}
		}
	}
}

func (m *OpsMonitor) ingestEvents(events []ErrorEvent) {
	cursorHashes := map[string]string{}
	cursorCounts := map[string]int64{}
	for _, event := range events {
		cursorKey := event.ProjectRoot + "\x00" + event.ProcessName
		cursorHashes[cursorKey] = event.Fingerprint + "\x00" + event.Normalized
		cursorCounts[cursorKey]++
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
	if m.CursorStore != nil {
		for key, hash := range cursorHashes {
			parts := strings.SplitN(key, "\x00", 2)
			if len(parts) != 2 {
				continue
			}
			previous, _ := m.CursorStore.GetLogCursor(parts[0], parts[1])
			_ = m.CursorStore.SaveLogCursor(LogCursor{ProjectRoot: parts[0], ProcessName: parts[1], Offset: previous.Offset + cursorCounts[key], WindowHash: hash, Updated: time.Now()})
		}
	}
}

func (m *OpsMonitor) stream(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		err := m.Stream(ctx, func(event ErrorEvent) { m.ingestEvents([]ErrorEvent{event}) })
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			delay = time.Second
		}
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
}

func (m *OpsMonitor) Stop() error {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.cancel, m.done = nil, nil
	release := m.releaseLease
	m.releaseLease = nil
	m.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if done != nil {
		<-done
	}
	if m.Lease != nil {
		_ = m.Lease.Release(context.Background(), m.LeaseKey, m.LeaseOwner)
	} else if release != nil {
		release()
	}
	return nil
}

func loopContextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

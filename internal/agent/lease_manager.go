package agent

import (
	"context"
	"errors"
	"sync"
	"time"
)

// RepositoryLeaseManager adapts the repository's durable lease primitives to
// the context-aware interface used by background workers. Release functions
// are retained only by the process holding the lease; the ownership itself is
// persisted by the repository.
type RepositoryLeaseManager struct {
	repo OpsRepository
	mu   sync.Mutex
	held map[string]func()
}

func NewLeaseManager(repo OpsRepository) *RepositoryLeaseManager {
	return &RepositoryLeaseManager{repo: repo, held: make(map[string]func())}
}

func (m *RepositoryLeaseManager) Acquire(ctx context.Context, key, owner string, ttl time.Duration) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if m == nil || m.repo == nil {
		return errors.New("租约仓储未初始化")
	}
	release, err := m.repo.AcquireOpsLease(key, owner, ttl)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if previous := m.held[key]; previous != nil {
		previous()
	}
	m.held[key] = release
	m.mu.Unlock()
	return nil
}

func (m *RepositoryLeaseManager) Renew(ctx context.Context, key, owner string, ttl time.Duration) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if m == nil || m.repo == nil {
		return errors.New("租约仓储未初始化")
	}
	return m.repo.RenewOpsLease(key, owner, ttl)
}

func (m *RepositoryLeaseManager) Release(ctx context.Context, key, owner string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if m == nil {
		return errors.New("租约管理器未初始化")
	}
	m.mu.Lock()
	release := m.held[key]
	delete(m.held, key)
	m.mu.Unlock()
	if release != nil {
		release()
	}
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

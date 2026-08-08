package agent

import (
	"errors"
	"path/filepath"
	"sync"
	"time"
)

var projectLocks = struct {
	sync.Mutex
	held map[string]lockLease
}{held: map[string]lockLease{}}

type lockLease struct {
	owner string
	until time.Time
}

// AcquireProjectWriteLock serializes writes to one project within this
// process. Expired leases are reclaimed so a crashed task cannot block work.
func AcquireProjectWriteLock(root, owner string) (func(), error) {
	key, _ := filepath.Abs(root)
	projectLocks.Lock()
	defer projectLocks.Unlock()
	if lease, ok := projectLocks.held[key]; ok && lease.until.After(time.Now()) && lease.owner != owner {
		return nil, errors.New("项目已有 Agent 写任务运行")
	}
	projectLocks.held[key] = lockLease{owner: owner, until: time.Now().Add(30 * time.Minute)}
	released := false
	return func() {
		projectLocks.Lock()
		defer projectLocks.Unlock()
		if !released {
			delete(projectLocks.held, key)
			released = true
		}
	}, nil
}

package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var projectLocks = struct {
	sync.Mutex
	held map[string]lockLease
}{held: map[string]lockLease{}}

type lockLease struct {
	Owner     string    `json:"owner"`
	Root      string    `json:"root"`
	CreatedAt time.Time `json:"createdAt"`
	RenewedAt time.Time `json:"renewedAt"`
	Until     time.Time `json:"until"`
}

func defaultLockDir() string {
	if dir := os.Getenv("ALEMONX_AGENT_LOCK_DIR"); dir != "" {
		return dir
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "alx-agent", "locks")
	}
	return filepath.Join(config, "alemonjs", "alx-agent", "locks")
}

func lockPath(dir, root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".lock")
}

// AcquireProjectWriteLockAt persists the lease so a process restart cannot
// silently lose ownership information. Expired leases are reclaimed.
func AcquireProjectWriteLockAt(root, owner, dir string) (func(), error) {
	key, _ := filepath.Abs(root)
	projectLocks.Lock()
	defer projectLocks.Unlock()
	now := time.Now()
	if held, ok := projectLocks.held[key]; ok && held.Until.After(now) && held.Owner != owner {
		return nil, errors.New("项目已有 Agent 写任务运行")
	}
	path := lockPath(dir, key)
	if raw, err := os.ReadFile(path); err == nil {
		var lease lockLease
		if json.Unmarshal(raw, &lease) == nil && lease.Until.After(now) && lease.Owner != owner {
			return nil, errors.New("项目已有 Agent 写任务运行")
		}
	}
	lease := lockLease{Owner: owner, Root: key, CreatedAt: now, RenewedAt: now, Until: now.Add(30 * time.Minute)}
	if err := os.MkdirAll(dir, 0700); err != nil {
		dir = filepath.Join(os.TempDir(), "alx-agent", "locks")
		path = lockPath(dir, key)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	raw, _ := json.Marshal(lease)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		// Read-only sandboxes and locked user config directories should not make
		// a task unusable; fall back to the process temp directory.
		dir = filepath.Join(os.TempDir(), "alx-agent", "locks")
		if mkErr := os.MkdirAll(dir, 0700); mkErr != nil {
			return nil, err
		}
		path = lockPath(dir, key)
		tmp = path + ".tmp"
		if writeErr := os.WriteFile(tmp, raw, 0600); writeErr != nil {
			return nil, writeErr
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	projectLocks.held[key] = lease
	released := false
	return func() {
		projectLocks.Lock()
		defer projectLocks.Unlock()
		if released {
			return
		}
		if raw, err := os.ReadFile(path); err == nil {
			var current lockLease
			if json.Unmarshal(raw, &current) == nil && current.Owner == owner {
				_ = os.Remove(path)
			}
		}
		delete(projectLocks.held, key)
		released = true
	}, nil
}

// AcquireProjectWriteLock serializes writes to one project within this
// process. Expired leases are reclaimed so a crashed task cannot block work.
func AcquireProjectWriteLock(root, owner string) (func(), error) {
	key, _ := filepath.Abs(root)
	return AcquireProjectWriteLockAt(key, owner, defaultLockDir())
}

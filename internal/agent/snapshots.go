package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type FileSnapshot struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Mode       uint32 `json:"mode"`
	BeforeHash string `json:"beforeHash"`
	BeforeData []byte `json:"beforeData,omitempty"`
	AfterHash  string `json:"afterHash,omitempty"`
}

type SnapshotStore struct {
	dir string
	mu  sync.Mutex
}

func NewSnapshotStoreAt(dir string) *SnapshotStore { return &SnapshotStore{dir: dir} }
func (s *SnapshotStore) path(id string) string     { return filepath.Join(s.dir, id+".json") }

func (s *SnapshotStore) Capture(id, root, relative string) (FileSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clean := filepath.ToSlash(filepath.Clean(relative))
	if filepath.IsAbs(relative) || clean == ".." || strings.HasPrefix(clean, "../") {
		return FileSnapshot{}, fmt.Errorf("快照路径越界：%s", relative)
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	data, err := os.ReadFile(path)
	snap := FileSnapshot{Path: relative}
	if errors.Is(err, os.ErrNotExist) {
		return snap, nil
	}
	if err != nil {
		return snap, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return snap, err
	}
	snap.Exists = true
	snap.Mode = uint32(info.Mode().Perm())
	snap.BeforeData = data
	snap.BeforeHash = hashBytes(data)
	return snap, nil
}

func (s *SnapshotStore) Save(id string, snapshots []FileSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	return atomicJSONFile(s.path(id), snapshots)
}

func (s *SnapshotStore) Load(id string) ([]FileSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var snapshots []FileSnapshot
	if err := readJSONFile(s.path(id), &snapshots); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func (s *SnapshotStore) Rollback(id, root string, force bool) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var snapshots []FileSnapshot
	if err := readJSONFile(s.path(id), &snapshots); err != nil {
		return nil, err
	}
	var conflicts []string
	for _, snap := range snapshots {
		clean := filepath.ToSlash(filepath.Clean(snap.Path))
		if filepath.IsAbs(snap.Path) || clean == ".." || strings.HasPrefix(clean, "../") {
			return nil, fmt.Errorf("快照路径越界：%s", snap.Path)
		}
		path := filepath.Join(root, filepath.FromSlash(snap.Path))
		current, err := os.ReadFile(path)
		if !snap.Exists && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err == nil && snap.AfterHash != "" && hashBytes(current) != snap.AfterHash {
			conflicts = append(conflicts, snap.Path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if len(conflicts) > 0 && !force {
		return conflicts, fmt.Errorf("文件已被外部修改：%v", conflicts)
	}
	for _, snap := range snapshots {
		clean := filepath.ToSlash(filepath.Clean(snap.Path))
		if filepath.IsAbs(snap.Path) || clean == ".." || strings.HasPrefix(clean, "../") {
			return nil, fmt.Errorf("快照路径越界：%s", snap.Path)
		}
		path := filepath.Join(root, filepath.FromSlash(snap.Path))
		if !snap.Exists {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, snap.BeforeData, os.FileMode(snap.Mode)); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func hashBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func HashBytes(data []byte) string { return hashBytes(data) }

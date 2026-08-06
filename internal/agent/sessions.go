package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Session identifies one persisted agent conversation.
type Session struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Root     string    `json:"root"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Updated  time.Time `json:"updated"`
}

// sessionIndex lists all persisted sessions, newest first.
type sessionIndex struct {
	Sessions []Session `json:"sessions"`
}

// SessionStore persists agent conversations as JSONL files. It is modeled on
// the ai.Manager config store: one directory under the user config, one file
// per session, plus an index. It is safe for concurrent use.
type SessionStore struct {
	dir string
	mu  sync.Mutex
}

// NewSessionStore opens (or creates) the session directory under the user's
// config directory. It returns a usable store even if the index is missing.
func NewSessionStore() (*SessionStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &SessionStore{dir: filepath.Join(dir, "alemonjs", "alx-agent")}, nil
}

// SessionStoreDir exposes the backing directory for tests.
func (s *SessionStore) SessionStoreDir() string { return s.dir }

func (s *SessionStore) indexPath() string {
	return filepath.Join(s.dir, "sessions.json")
}

func (s *SessionStore) sessionPath(id string) string {
	return filepath.Join(s.dir, id+".jsonl")
}

func (s *SessionStore) loadIndex() (sessionIndex, error) {
	var index sessionIndex
	raw, err := os.ReadFile(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return index, nil
	}
	if err != nil {
		return index, err
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		return index, fmt.Errorf("会话索引无效：%w", err)
	}
	return index, nil
}

func (s *SessionStore) saveIndex(index sessionIndex) error {
	raw, _ := json.MarshalIndent(index, "", "  ")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(s.indexPath(), append(raw, '\n'), 0600)
}

// List returns all sessions newest-first.
func (s *SessionStore) List() ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(index.Sessions))
	out = append(out, index.Sessions...)
	return out, nil
}

// Get returns one session by id, or an error if absent.
func (s *SessionStore) Get(id string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.loadIndex()
	if err != nil {
		return Session{}, err
	}
	for _, session := range index.Sessions {
		if session.ID == id {
			return session, nil
		}
	}
	return Session{}, errors.New("会话不存在")
}

// Create records a new session with an empty transcript.
func (s *SessionStore) Create(root, provider, model string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	session := Session{
		ID:       newSessionID(),
		Root:     root,
		Provider: provider,
		Model:    model,
		Updated:  now,
	}
	session.Title = deriveTitle(root)
	index, err := s.loadIndex()
	if err != nil {
		return Session{}, err
	}
	index.Sessions = append([]Session{session}, index.Sessions...)
	if len(index.Sessions) > 100 {
		index.Sessions = index.Sessions[:100]
	}
	if err := s.saveIndex(index); err != nil {
		return Session{}, err
	}
	return session, nil
}

// Append writes one JSONL entry to a session's transcript.
func (s *SessionStore) Append(id string, message Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	path := s.sessionPath(id)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return err
	}
	// Touch the session's Updated timestamp in the index.
	index, err := s.loadIndex()
	if err != nil {
		return err
	}
	for i := range index.Sessions {
		if index.Sessions[i].ID == id {
			index.Sessions[i].Updated = time.Now()
			break
		}
	}
	return s.saveIndex(index)
}

// Load reads a session's transcript as messages, newest-last (replay order).
func (s *SessionStore) Load(id string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.sessionPath(id)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var messages []Message
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message Message
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}
		messages = append(messages, message)
	}
	return messages, scanner.Err()
}

// Delete removes a session's transcript and index entry.
func (s *SessionStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Remove(s.sessionPath(id))
	index, err := s.loadIndex()
	if err != nil {
		return err
	}
	filtered := make([]Session, 0, len(index.Sessions))
	for _, session := range index.Sessions {
		if session.ID != id {
			filtered = append(filtered, session)
		}
	}
	index.Sessions = filtered
	return s.saveIndex(index)
}

func deriveTitle(root string) string {
	base := filepath.Base(strings.TrimRight(root, string(filepath.Separator)))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "未命名会话"
	}
	return base
}

func newSessionID() string {
	now := time.Now()
	return fmt.Sprintf("s%d", now.UnixNano())
}

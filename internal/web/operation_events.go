package web

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const operationHistoryLimit = 40
const operationEventRetention = 7 * 24 * time.Hour
const operationEventMaxBytes = 10 * 1024 * 1024

type eventEnvelope struct {
	ID        int64           `json:"id"`
	Topic     string          `json:"topic"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"createdAt"`
	Data      json.RawMessage `json:"data"`
}

// OperationEventStore owns both operation snapshots and the durable, global
// event journal. WAL makes task updates durable without blocking SSE readers.
type OperationEventStore struct {
	mu     sync.Mutex
	db     *sql.DB
	path   string
	writes int
}

func newOperationEventStore(path string) *OperationEventStore {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return &OperationEventStore{}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return &OperationEventStore{}
	}
	s := &OperationEventStore{db: db, path: path}
	if _, err = db.Exec(`PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS event_journal(id INTEGER PRIMARY KEY AUTOINCREMENT, topic TEXT NOT NULL, type TEXT NOT NULL, payload BLOB NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS operation_snapshots(id TEXT PRIMARY KEY, payload BLOB NOT NULL, created_at TEXT NOT NULL, position INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS event_journal_topic_id ON event_journal(topic,id);
CREATE INDEX IF NOT EXISTS event_journal_created_at ON event_journal(created_at);`); err != nil {
		_ = db.Close()
		return &OperationEventStore{}
	}
	s.migrateJSON(filepath.Join(filepath.Dir(path), "events.json"))
	s.pruneLocked(time.Now())
	return s
}

func (s *OperationEventStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *OperationEventStore) snapshot() []operationTask {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT payload FROM operation_snapshots ORDER BY position ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []operationTask
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) == nil {
			var item operationTask
			if json.Unmarshal(raw, &item) == nil {
				out = append(out, item)
			}
		}
	}
	return out
}

func (s *OperationEventStore) append(topic, kind string, data any, operations []operationTask) (eventEnvelope, error) {
	if s == nil || s.db == nil {
		return eventEnvelope{}, os.ErrInvalid
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return eventEnvelope{}, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return eventEnvelope{}, err
	}
	if operations != nil {
		if len(operations) > operationHistoryLimit {
			operations = operations[:operationHistoryLimit]
		}
		if _, err = tx.Exec(`DELETE FROM operation_snapshots`); err == nil {
			for position, item := range operations {
				raw, marshalErr := json.Marshal(item)
				if marshalErr != nil {
					err = marshalErr
					break
				}
				_, err = tx.Exec(`INSERT INTO operation_snapshots(id,payload,created_at,position) VALUES(?,?,?,?)`, item.ID, raw, now.Format(time.RFC3339Nano), position)
				if err != nil {
					break
				}
			}
		}
	}
	var insert sql.Result
	if err == nil {
		insert, err = tx.Exec(`INSERT INTO event_journal(topic,type,payload,created_at) VALUES(?,?,?,?)`, topic, kind, payload, now.Format(time.RFC3339Nano))
	}
	if err != nil {
		_ = tx.Rollback()
		return eventEnvelope{}, err
	}
	id, err := insert.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return eventEnvelope{}, err
	}
	if err = tx.Commit(); err != nil {
		return eventEnvelope{}, err
	}
	s.writes++
	if s.writes%100 == 0 {
		s.pruneLocked(now)
	}
	return eventEnvelope{ID: id, Topic: topic, Type: kind, CreatedAt: now, Data: payload}, nil
}

func (s *OperationEventStore) after(id int64, topics map[string]bool) []eventEnvelope {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,topic,type,payload,created_at FROM event_journal WHERE id>? ORDER BY id ASC`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []eventEnvelope
	for rows.Next() {
		var item eventEnvelope
		var created string
		if rows.Scan(&item.ID, &item.Topic, &item.Type, &item.Data, &created) == nil && (len(topics) == 0 || topics[item.Topic]) {
			item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			out = append(out, item)
		}
	}
	return out
}

func (s *OperationEventStore) bounds() (int64, int64) {
	if s == nil || s.db == nil {
		return 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var earliest, latest int64
	_ = s.db.QueryRow(`SELECT COALESCE(MIN(id),0),COALESCE(MAX(id),0) FROM event_journal`).Scan(&earliest, &latest)
	return earliest, latest
}

func (s *OperationEventStore) pruneLocked(now time.Time) {
	if s == nil || s.db == nil {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM event_journal WHERE created_at < ?`, now.Add(-operationEventRetention).UTC().Format(time.RFC3339Nano))
	for {
		var bytes int64
		_ = s.db.QueryRow(`SELECT COALESCE(SUM(length(payload)),0) FROM event_journal`).Scan(&bytes)
		if bytes <= operationEventMaxBytes {
			break
		}
		result, _ := s.db.Exec(`DELETE FROM event_journal WHERE id=(SELECT id FROM event_journal ORDER BY id ASC LIMIT 1)`)
		changed, _ := result.RowsAffected()
		if changed == 0 {
			break
		}
	}
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
}

func (s *OperationEventStore) migrateJSON(path string) {
	if s == nil || s.db == nil {
		return
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	var exists int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM event_journal`).Scan(&exists)
	if exists > 0 {
		_ = os.Rename(path, path+".migrated")
		return
	}
	var legacy struct {
		Operations []operationTask `json:"operations"`
		Events     []struct {
			Event     robotEvent `json:"event"`
			CreatedAt time.Time  `json:"createdAt"`
		} `json:"events"`
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &legacy) != nil {
		return
	}
	for _, item := range legacy.Events {
		_, _ = s.append("robot", item.Event.Type, item.Event, nil)
	}
	if len(legacy.Operations) > 0 {
		_, _ = s.append("migration", "operations.migrated", map[string]bool{"ok": true}, legacy.Operations)
	}
	_ = os.Rename(path, path+".migrated")
}

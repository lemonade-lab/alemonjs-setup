package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MigrateOpsJSONToSQLite migrates the portable JSON store into the pure-Go
// SQLite repository. The source remains untouched and is backed up before any
// records are written to the destination.
func MigrateOpsJSONToSQLite(sourceDir, databasePath, backupDir string) error {
	if strings.TrimSpace(sourceDir) == "" || strings.TrimSpace(databasePath) == "" {
		return errors.New("迁移路径不能为空")
	}
	entries := make([]string, 0)
	err := filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), "backups") {
			return nil
		}
		entries = append(entries, path)
		return nil
	})
	if err != nil {
		return err
	}
	if backupDir == "" {
		backupDir = filepath.Join(sourceDir, "backups")
	}
	backup := filepath.Join(backupDir, time.Now().UTC().Format("20060102-150405.000000000"))
	for _, path := range entries {
		rel, relErr := filepath.Rel(sourceDir, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(backup, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			return err
		}
	}

	repo, err := NewSQLiteOpsRepository(databasePath)
	if err != nil {
		return err
	}
	source := NewOpsStoreAt(sourceDir)
	incidents, err := source.ListIncidents()
	if err != nil {
		return err
	}
	for _, value := range incidents {
		if err := repo.SaveIncident(value); err != nil {
			return err
		}
	}
	todos, err := source.ListTodos()
	if err != nil {
		return err
	}
	for _, value := range todos {
		if err := repo.SaveTodo(value); err != nil {
			return err
		}
	}
	runs, err := source.ListMaintenance()
	if err != nil {
		return err
	}
	for _, value := range runs {
		if err := repo.SaveMaintenance(value); err != nil {
			return err
		}
	}
	policies, err := source.ListPolicies()
	if err != nil {
		return err
	}
	for _, value := range policies {
		if err := repo.SavePolicy(value); err != nil {
			return err
		}
	}
	alerts, err := source.ListAlerts()
	if err != nil {
		return err
	}
	for _, value := range alerts {
		if err := repo.SaveAlert(value); err != nil {
			return err
		}
	}
	roles, err := source.ListRoles()
	if err != nil {
		return err
	}
	for _, value := range roles {
		if err := repo.SaveRole(value); err != nil {
			return err
		}
	}
	audits, err := source.ListAudit()
	if err != nil {
		return err
	}
	for _, value := range audits {
		if err := repo.AppendAudit(value); err != nil {
			return err
		}
	}
	signals, err := source.ListSignals()
	if err != nil {
		return err
	}
	for _, value := range signals {
		if err := repo.AppendSignal(value); err != nil {
			return err
		}
	}
	// Import persisted event samples and deduplication keys without depending
	// on the JSON layout beyond the public store API.
	for _, incident := range incidents {
		events, err := source.ListEvents(incident.ID)
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := repo.AppendEvent(incident.ID, event); err != nil {
				return err
			}
		}
	}
	seenPath := filepath.Join(sourceDir, "seen-events.json")
	var seen map[string]time.Time
	if err := readJSONFile(seenPath, &seen); err == nil {
		for key := range seen {
			if _, err := repo.MarkEventSeen(key); err != nil {
				return err
			}
		}
	}
	return nil
}

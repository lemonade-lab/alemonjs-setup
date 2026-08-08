package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpsLeaseRejectsSecondOwnerAndReleases(t *testing.T) {
	store := NewOpsStoreAt(t.TempDir())
	release, err := store.AcquireOpsLease("monitor", "one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcquireOpsLease("monitor", "two", time.Minute); err == nil {
		t.Fatal("second owner should be rejected")
	}
	release()
	if next, err := store.AcquireOpsLease("monitor", "two", time.Minute); err != nil {
		t.Fatal(err)
	} else {
		next()
	}
}

func TestMigrateJSONToSQLiteKeepsSource(t *testing.T) {
	source, db := t.TempDir(), filepath.Join(t.TempDir(), "ops.db")
	store := NewOpsStoreAt(source)
	if err := store.SaveIncident(Incident{ID: "i1", ProjectRoot: "/tmp/project", Status: IncidentDetected, Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := MigrateOpsJSONToSQLite(source, db, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.incidentPath("i1")); err != nil {
		t.Fatalf("source should remain: %v", err)
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("database missing: %v", err)
	}
}

func TestSQLiteOpsRepositoryRoundTrip(t *testing.T) {
	repo, err := NewSQLiteOpsRepository(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	incident := Incident{ID: "sqlite-1", ProjectRoot: "/p", Status: IncidentDetected, Updated: time.Now()}
	if err := repo.SaveIncident(incident); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM incidents WHERE id=?`, incident.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("业务表未写入 count=%d err=%v", count, err)
	}
	loaded, err := repo.GetIncident(incident.ID)
	if err != nil || loaded.ID != incident.ID {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if err := repo.SaveLogCursor(LogCursor{ProjectRoot: "/p", ProcessName: "web", Offset: 12, WindowHash: "h"}); err != nil {
		t.Fatal(err)
	}
	cursor, err := repo.GetLogCursor("/p", "web")
	if err != nil || cursor.Offset != 12 || cursor.WindowHash != "h" {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryLeaseManagerHonorsContextAndOwnership(t *testing.T) {
	repo := NewOpsStoreAt(t.TempDir())
	manager := NewLeaseManager(repo)
	if err := manager.Acquire(context.Background(), "worker", "one", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := manager.Acquire(context.Background(), "worker", "two", time.Minute); err == nil {
		t.Fatal("second owner should be rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Renew(ctx, "worker", "one", time.Minute); err == nil {
		t.Fatal("cancelled context should stop renewal")
	}
	if err := manager.Release(context.Background(), "worker", "one"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Acquire(context.Background(), "worker", "two", time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestAuditAndAlertPersistence(t *testing.T) {
	store := NewOpsStoreAt(t.TempDir())
	if err := store.AppendAudit(AuditEntry{Actor: "alice", Role: "approver", Action: "incident.approve", Result: "accepted"}); err != nil {
		t.Fatal(err)
	}
	audits, err := store.ListAudit()
	if err != nil || len(audits) != 1 || audits[0].Actor != "alice" {
		t.Fatalf("audits = %#v, err=%v", audits, err)
	}
	if err := store.SaveAlert(AlertRecord{Alert: Alert{ID: "a1", Severity: "high", Message: "boom"}, Status: "open"}); err != nil {
		t.Fatal(err)
	}
	alerts, err := store.ListAlerts()
	if err != nil || len(alerts) != 1 || alerts[0].ID != "a1" {
		t.Fatalf("alerts = %#v, err=%v", alerts, err)
	}
}

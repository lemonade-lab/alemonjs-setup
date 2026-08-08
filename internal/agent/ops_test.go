package agent

import (
	"context"
	"testing"
	"time"
)

func TestErrorFingerprintNormalizesVolatileValues(t *testing.T) {
	a := ErrorFingerprint("/srv/app", "api", "Error requestId=abc user=1001 port 127.0.0.1:3000", "src/api.ts:42")
	b := ErrorFingerprint("/srv/app", "api", "Error requestId=def user=2002 port 127.0.0.1:4000", "src/api.ts:42")
	if a != b {
		t.Fatalf("volatile values should not change fingerprint: %s != %s", a, b)
	}
}

func TestIncidentAggregatorDeduplicates(t *testing.T) {
	store := NewOpsStoreAt(t.TempDir())
	agg := NewIncidentAggregator(store)
	event := ErrorEvent{ProjectRoot: "/srv/app", ProcessName: "api", RawMessage: "TypeError: failed requestId=a", Timestamp: time.Now()}
	first, fresh, err := agg.Ingest(event)
	if err != nil || !fresh {
		t.Fatalf("first event = %#v %v %v", first, fresh, err)
	}
	second, fresh, err := agg.Ingest(event)
	if err != nil || fresh {
		t.Fatalf("duplicate event = %#v %v %v", second, fresh, err)
	}
	if second.ID != first.ID || second.Occurrences != 2 {
		t.Fatalf("dedup result = %#v", second)
	}
}

func TestOpsMonitorStopsIdempotently(t *testing.T) {
	store := NewOpsStoreAt(t.TempDir())
	count := 0
	monitor := &OpsMonitor{Interval: time.Millisecond, Aggregator: NewIncidentAggregator(store), Source: func(context.Context) ([]ErrorEvent, error) { count++; return nil, nil }}
	if err := monitor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	_ = monitor.Stop()
	_ = monitor.Stop()
	if count == 0 {
		t.Fatal("monitor did not poll")
	}
}

func TestDecideAutoFixEscalatesHighRisk(t *testing.T) {
	decision := DecideAutoFix(Incident{Sample: "database migration failed with password token"}, OpsPolicy{Mode: "auto", AllowCodeChanges: true, VerificationCommand: "npm test"})
	if decision.Action != "create_todo" || decision.Risk != "high" || !decision.RequiresHuman {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestDecideAutoFixAllowsLowRiskCodeFix(t *testing.T) {
	decision := DecideAutoFix(Incident{Sample: "TypeError: cannot read property", File: "src/app.ts", Line: 42}, OpsPolicy{Mode: "auto", AutoAllowed: true, AllowCodeChanges: true, VerificationCommand: "npm test"})
	if decision.Action != "auto_fix" || decision.RequiresHuman || decision.Confidence < 0.8 {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestDecideAutoFixRequiresWhitelist(t *testing.T) {
	decision := DecideAutoFix(Incident{Sample: "TypeError", File: "src/app.ts", Line: 42}, OpsPolicy{Mode: "auto", AllowCodeChanges: true, VerificationCommand: "npm test"})
	if decision.Action != "create_todo" || !decision.RequiresHuman {
		t.Fatalf("unwhitelisted project must not auto-fix: %+v", decision)
	}
}

func TestOpsOrchestratorCreatesTodoForObservePolicy(t *testing.T) {
	store := NewOpsStoreAt(t.TempDir())
	incident := Incident{ID: "inc-1", ProjectRoot: "/tmp/project", ProcessName: "app", Sample: "TypeError", Status: IncidentDetected, Severity: "medium", Updated: time.Now()}
	if err := store.SaveIncident(incident); err != nil {
		t.Fatal(err)
	}
	o := &OpsOrchestrator{Store: store, Policy: func(string) (OpsPolicy, error) { return OpsPolicy{Mode: "observe"}, nil }}
	updated, decision, err := o.Analyze(incident.ID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "observe_only" || updated.Status != IncidentObserving {
		t.Fatalf("unexpected result: %+v %+v", updated, decision)
	}
}

func TestOpsMonitorPersistsEventDeduplication(t *testing.T) {
	dir := t.TempDir()
	store := NewOpsStoreAt(dir)
	event := ErrorEvent{ProjectRoot: "/project", ProcessName: "app", RawMessage: "Error: failed at src/app.ts:1"}
	m := &OpsMonitor{Aggregator: NewIncidentAggregator(store), Source: func(context.Context) ([]ErrorEvent, error) { return []ErrorEvent{event}, nil }, Interval: time.Hour}
	seen := 0
	m.OnIncident = func(Incident, bool) { seen++ }
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	_ = m.Stop()
	if seen != 1 {
		t.Fatalf("expected one incident, got %d", seen)
	}
	m2 := &OpsMonitor{Aggregator: NewIncidentAggregator(NewOpsStoreAt(dir)), Source: func(context.Context) ([]ErrorEvent, error) { return []ErrorEvent{event}, nil }, Interval: time.Hour}
	seen = 0
	m2.OnIncident = func(Incident, bool) { seen++ }
	_ = m2.Start(context.Background())
	time.Sleep(10 * time.Millisecond)
	_ = m2.Stop()
	if seen != 0 {
		t.Fatalf("persisted duplicate should not wake AI, got %d", seen)
	}
}

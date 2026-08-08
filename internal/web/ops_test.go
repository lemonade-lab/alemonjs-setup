package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alemonx/internal/agent"
)

func TestOpsHandlerIncidentEventsAndMetrics(t *testing.T) {
	store := agent.NewOpsStoreAt(t.TempDir())
	incident := agent.Incident{ID: "inc-test", ProjectRoot: t.TempDir(), ProcessName: "app", Fingerprint: "fp", Status: agent.IncidentDetected, Severity: "medium", Occurrences: 1, Updated: time.Now()}
	if err := store.SaveIncident(incident); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(incident.ID, agent.ErrorEvent{ID: "evt-1", RawMessage: "Error: test"}); err != nil {
		t.Fatal(err)
	}
	s := &server{opsStore: store}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ops/incidents/inc-test/events", nil)
	rec := httptest.NewRecorder()
	s.opsHandler(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("evt-1")) {
		t.Fatalf("events response: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ops/metrics", nil)
	rec = httptest.NewRecorder()
	s.opsHandler(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"incidents":1`)) {
		t.Fatalf("metrics response: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOpsPolicyRequiresAutoWhitelist(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"ops-test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store := agent.NewOpsStoreAt(t.TempDir())
	s := &server{opsStore: store}
	body, _ := json.Marshal(agent.OpsPolicy{ProjectRoot: root, Mode: "auto", AllowCodeChanges: true})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/ops/policy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.opsHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("auto without whitelist should fail, got %d", rec.Code)
	}
	body, _ = json.Marshal(agent.OpsPolicy{ProjectRoot: root, Mode: "observe"})
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/ops/policy", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.opsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("observe policy should save, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOpsPrometheusMetrics(t *testing.T) {
	store := agent.NewOpsStoreAt(t.TempDir())
	s := &server{opsStore: store}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ops/metrics/prometheus", nil)
	rec := httptest.NewRecorder()
	s.opsHandler(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("incident_total")) {
		t.Fatalf("prometheus response: %d %s", rec.Code, rec.Body.String())
	}
}

package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func newTestServer() http.Handler {
	return NewServer("test", fstest.MapFS{"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}})
}

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"version":"test"`)) {
		t.Fatalf("response does not include version: %s", response.Body.String())
	}
}

func TestGoalsAndTaskCreation(t *testing.T) {
	handler := newTestServer()
	goals := httptest.NewRecorder()
	handler.ServeHTTP(goals, httptest.NewRequest(http.MethodGet, "/api/v1/goals", nil))
	if goals.Code != http.StatusOK || !bytes.Contains(goals.Body.Bytes(), []byte(`"id":"develop"`)) {
		t.Fatalf("goals response = %d %s", goals.Code, goals.Body.String())
	}

	task := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBufferString(`{"goalId":"develop"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(task, request)
	if task.Code != http.StatusCreated || !bytes.Contains(task.Body.Bytes(), []byte(`"status":"ready"`)) {
		t.Fatalf("task response = %d %s", task.Code, task.Body.String())
	}
}

func TestRobotTasksStartsAsJSONArray(t *testing.T) {
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/robot/tasks", nil))
	if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("empty robot tasks = %d %q, want JSON array", response.Code, response.Body.String())
	}
}

func TestInvalidTaskGoalReturnsChineseError(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", bytes.NewBufferString(`{"goalId":"unknown"}`))
	newTestServer().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("不存在")) {
		t.Fatalf("error should be Chinese: %s", response.Body.String())
	}
}

func TestChecks(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/checks", bytes.NewBufferString(`{"goalId":"desktop"}`))
	newTestServer().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"goalId":"desktop"`)) {
		t.Fatalf("checks response = %s", response.Body.String())
	}
}

func TestCleanWebChecksNodeAndGit(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/checks", bytes.NewBufferString(`{"goalId":"web","variant":"clean"}`))
	NewServer("test", fstest.MapFS{"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"id":"node"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"id":"git"`)) {
		t.Fatalf("clean web checks should include node and git: %s", response.Body.String())
	}
}

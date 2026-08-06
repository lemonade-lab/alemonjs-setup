package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"alemonx/internal/access"
	"alemonx/internal/robot"
)

func newTestServer() http.Handler {
	return NewServer("test", fstest.MapFS{"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}})
}

func TestWebViewHTMLRewriteAndRestrictedBridge(t *testing.T) {
	html := rewriteWebViewHTML(`<!doctype html><head><link href="/favicon.ico"><link href="/assets/app.css"></head><body><script src="/assets/app.js"></script></body>`)
	for _, expected := range []string{`href="favicon.ico"`, `href="assets/app.css"`, `src="assets/app.js"`, `<script src="bridge.js"></script></head>`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("rewritten HTML misses %q: %s", expected, html)
		}
	}
	bridge := webViewBridge(robot.WebViewEntry{Package: `plugin"name`, Name: "页面"})
	for _, expected := range []string{`window.__alxWebview`, `./api/`, `plugin\"name`, `window.appDesktopAPI`, `'message'`, `'events'`, `'api-error'`, `response.clone().json()`} {
		if !strings.Contains(bridge, expected) {
			t.Fatalf("bridge misses %q", expected)
		}
	}
	if strings.Contains(bridge, "WailsInvoke") || strings.Contains(bridge, "Shell") {
		t.Fatalf("bridge must not expose desktop privileges: %s", bridge)
	}
	for _, expected := range []string{"@alemonjs/process", "events[data.type]", "process.stdin.on"} {
		if !strings.Contains(defaultWebViewDesktopScript, expected) {
			t.Fatalf("desktop script misses %q", expected)
		}
	}
}

func TestDirectoryLocationNamesWindowsDrives(t *testing.T) {
	name, kind := directoryLocation(`C:\\`, `C:\\Users\\tester`, "windows")
	if name != "本地磁盘（C:）" || kind != "volume" {
		t.Fatalf("Windows root label = (%q, %q), want local C drive", name, kind)
	}
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

func TestIdentityProtectionRequiresLoginAfterEnable(t *testing.T) {
	identity, err := access.NewAt(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithAuth("test", fstest.MapFS{"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}, identity)
	if _, err := identity.Enable("lemonade", "secret", "secret"); err != nil {
		t.Fatal(err)
	}
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/api/v1/goals", nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", blocked.Code)
	}
	login := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"account":"lemonade","password":"secret"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, request)
	if login.Code != http.StatusOK || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login = %d %s", login.Code, login.Body.String())
	}
	allowed := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/goals", nil)
	request.AddCookie(login.Result().Cookies()[0])
	handler.ServeHTTP(allowed, request)
	if allowed.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", allowed.Code)
	}
}

func TestRobotWebViewUsesItsOwnFramePolicyAndBypassesManagementLogin(t *testing.T) {
	identity, err := access.NewAt(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Enable("lemonade", "secret", "secret"); err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithAuth("test", fstest.MapFS{"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}, identity)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/robot/webview/not-a-root/plugin/", nil))

	if response.Code == http.StatusUnauthorized {
		t.Fatalf("WebView route must not require the management cookie")
	}
	if got := response.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("WebView X-Frame-Options = %q, want empty for cross-loopback frame", got)
	}
}

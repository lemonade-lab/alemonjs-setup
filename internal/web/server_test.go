package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"alemonx/internal/access"
	"alemonx/internal/robot"
)

var errInternalTest = errors.New("internal test failure")

func newTestServer() http.Handler {
	return NewServer("test", fstest.MapFS{"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}})
}

// TestOperationWriterBuffersPartialLines ensures chunked writes that split a
// newline are only appended as complete lines, matching how a supervised
// process may emit output in arbitrary chunk sizes.
func TestOperationWriterBuffersPartialLines(t *testing.T) {
	s := newStatefulTestServer()
	s.operations = []operationTask{{ID: "dev-1", Output: ""}}
	writer := newOperationWriter("dev-1", s)

	// A chunk that splits "hello\n" across writes.
	if _, err := writer.Write([]byte("hel")); err != nil {
		t.Fatal(err)
	}
	if got := s.operations[0].Output; got != "" {
		t.Fatalf("partial line leaked before newline: %q", got)
	}
	if _, err := writer.Write([]byte("lo\n")); err != nil {
		t.Fatal(err)
	}
	if got := s.operations[0].Output; got != "hello\n" {
		t.Fatalf("output = %q, want hello\n", got)
	}
	// Multiple complete lines in one write.
	if _, err := writer.Write([]byte("a\nb\n")); err != nil {
		t.Fatal(err)
	}
	if got := s.operations[0].Output; got != "hello\na\nb\n" {
		t.Fatalf("output = %q, want three lines", got)
	}
}

// newStatefulTestServer builds a server whose internal maps are populated so
// tests can exercise the console, stop and mutual-exclusion paths directly
// without a real PM2 daemon or a listening listener.
func newStatefulTestServer() *server {
	return &server{
		robots:        robot.Manager{},
		operations:    []operationTask{},
		development:   map[string]developmentProcess{},
		stopping:      map[string]bool{},
		consoleCache:  map[string]consoleSnapshot{},
		pm2Status:     func(string) (robot.PM2Status, error) { return robot.PM2Status{}, nil },
		directoryRoots: managedDirectoryRoots(),
	}
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRuntimeProcessOutputPrefersRunningProcess covers the case where both a
// dev and an app run have history: the currently running one must win, and a
// fresh dev run after an older foreground run must not be hidden.
func TestRuntimeProcessOutputPrefersRunningProcess(t *testing.T) {
	root := t.TempDir()
	s := newStatefulTestServer()
	finished := time.Now()
	s.operations = []operationTask{
		{ID: "dev-new", Root: root, Action: "dev", Status: "running", Output: "dev live"},
		{ID: "app-old", Root: root, Action: "app", Status: "completed", Output: "app old", FinishedAt: &finished},
	}
	output, status, _, mode := s.runtimeProcessOutput(root)
	if status != "running" || output != "dev live" || mode != "开发模式" {
		t.Fatalf("running process not preferred: output=%q status=%q mode=%q", output, status, mode)
	}

	// No running process: fall back to the newest history (newest-first list).
	s.operations = []operationTask{
		{ID: "app-new", Root: root, Action: "app", Status: "completed", Output: "app new", FinishedAt: &finished},
		{ID: "dev-old", Root: root, Action: "dev", Status: "completed", Output: "dev old", FinishedAt: &finished},
	}
	output, status, _, mode = s.runtimeProcessOutput(root)
	if status != "completed" || output != "app new" || mode != "前台运行" {
		t.Fatalf("newest history not returned: output=%q status=%q mode=%q", output, status, mode)
	}
}

func TestRobotConsoleSeparatesSnapshotAndOutput(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "package.json", `{"name":"bot","version":"1.0.0","scripts":{"dev":"node index.js"}}`)
	writeFixture(t, root, "index.js", "console.log('hi')\n")
	s := newStatefulTestServer()
	s.operations = []operationTask{
		{ID: "dev-1", Root: root, Action: "dev", Status: "running", Output: "ready line"},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/robot/console?root="+url.QueryEscape(root), nil)
	s.robotConsoleHandler(recorder, request)

	var payload consolePayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !payload.Running {
		t.Fatalf("running = false, want true for a live dev task")
	}
	if !strings.Contains(payload.Output, "开发模式实时输出") || !strings.Contains(payload.Output, "ready line") {
		t.Fatalf("output = %q, want live dev output", payload.Output)
	}
	if !strings.Contains(payload.Snapshot, "$ pwd") || !strings.Contains(payload.Snapshot, "$ package.json") {
		t.Fatalf("snapshot = %q, want static project context", payload.Snapshot)
	}
	if payload.Mode != "开发模式" {
		t.Fatalf("mode = %q, want 开发模式", payload.Mode)
	}
}

func TestRobotConsoleRefreshBypassesSnapshotCache(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "package.json", `{"name":"bot","version":"1.0.0"}`)
	s := newStatefulTestServer()
	// Prime the cache with stale content; a non-refresh poll must return it.
	s.consoleCache[root] = consoleSnapshot{output: "STALE-SNAPSHOT", at: time.Now()}

	plain := httptest.NewRecorder()
	s.robotConsoleHandler(plain, httptest.NewRequest(http.MethodGet, "/api/v1/robot/console?root="+url.QueryEscape(root), nil))
	var stalePayload consolePayload
	if err := json.Unmarshal(plain.Body.Bytes(), &stalePayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stalePayload.Snapshot, "STALE-SNAPSHOT") {
		t.Fatalf("non-refresh poll did not reuse cache: %q", stalePayload.Snapshot)
	}

	fresh := httptest.NewRecorder()
	s.robotConsoleHandler(fresh, httptest.NewRequest(http.MethodGet, "/api/v1/robot/console?root="+url.QueryEscape(root)+"&refresh=1", nil))
	var freshPayload consolePayload
	if err := json.Unmarshal(fresh.Body.Bytes(), &freshPayload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(freshPayload.Snapshot, "STALE-SNAPSHOT") {
		t.Fatalf("refresh=1 reused stale snapshot: %q", freshPayload.Snapshot)
	}
	if !strings.Contains(freshPayload.Snapshot, "$ pwd") {
		t.Fatalf("refresh did not regenerate snapshot: %q", freshPayload.Snapshot)
	}
}

func TestRobotConsoleShowsExitReasonOnFailure(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "package.json", `{"name":"bot","version":"1.0.0"}`)
	s := newStatefulTestServer()
	finished := time.Now()
	s.operations = []operationTask{
		{ID: "dev-1", Root: root, Action: "dev", Status: "failed", Output: "boom\n", Error: "开发进程已退出：exit status 1", FinishedAt: &finished},
	}

	recorder := httptest.NewRecorder()
	s.robotConsoleHandler(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/robot/console?root="+url.QueryEscape(root), nil))
	var payload consolePayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.Output, "开发进程已退出：exit status 1") {
		t.Fatalf("output misses exit reason: %q", payload.Output)
	}
	if payload.Running {
		t.Fatalf("failed task must not be reported as running")
	}
}

func TestLocalStartBlockedByPM2Running(t *testing.T) {
	root := t.TempDir()
	s := newStatefulTestServer()
	s.pm2Status = func(string) (robot.PM2Status, error) {
		return robot.PM2Status{Configured: true, Managed: true, Running: true, Status: "online"}, nil
	}
	message, blocked := s.localStartBlockedByPM2(root)
	if !blocked || !strings.Contains(message, "后台（PM2）运行") {
		t.Fatalf("blocked = %v, message = %q", blocked, message)
	}
}

func TestLocalStartNotBlockedWhenPM2Idle(t *testing.T) {
	root := t.TempDir()
	s := newStatefulTestServer()
	if _, blocked := s.localStartBlockedByPM2(root); blocked {
		t.Fatalf("idle PM2 must not block local start")
	}
	// A PM2 status read failure must also never block.
	s.pm2Status = func(string) (robot.PM2Status, error) {
		return robot.PM2Status{}, errInternalTest
	}
	if _, blocked := s.localStartBlockedByPM2(root); blocked {
		t.Fatalf("PM2 read error must not block local start")
	}
}

func TestPM2StartBlockedByLocalRunning(t *testing.T) {
	root := t.TempDir()
	s := newStatefulTestServer()
	s.development[root] = developmentProcess{TaskID: "dev-1"}
	message, blocked := s.pm2StartBlockedByLocal(root)
	if !blocked || !strings.Contains(message, "本机（开发/前台）运行") {
		t.Fatalf("blocked = %v, message = %q", blocked, message)
	}
	delete(s.development, root)
	if _, blocked := s.pm2StartBlockedByLocal(root); blocked {
		t.Fatalf("no local process must not block PM2 start")
	}
}

func TestStopDevelopmentWithoutProcessFinishesImmediately(t *testing.T) {
	root := t.TempDir()
	s := newStatefulTestServer()
	if s.stopDevelopment(root, "开发模式") {
		t.Fatalf("stopDevelopment reported a running process that does not exist")
	}
}

func TestCompletePendingStopTasks(t *testing.T) {
	root := t.TempDir()
	finished := time.Now()
	s := newStatefulTestServer()
	s.operations = []operationTask{
		{ID: "stop-1", Root: root, Action: "dev-stop", Status: "running"},
		{ID: "stop-2", Root: root, Action: "app-stop", Status: "running"},
		{ID: "other-root", Root: "/elsewhere", Action: "dev-stop", Status: "running"},
		{ID: "install-1", Root: root, Action: "install", Status: "running"},
	}
	s.completePendingStopTasks(root, finished)
	byID := map[string]operationTask{}
	for _, item := range s.operations {
		byID[item.ID] = item
	}
	if byID["stop-1"].Status != "completed" || !strings.Contains(byID["stop-1"].Output, "已停止开发模式") {
		t.Fatalf("stop-1 = %+v, want completed 开发模式", byID["stop-1"])
	}
	if byID["stop-2"].Status != "completed" || !strings.Contains(byID["stop-2"].Output, "已停止前台运行") {
		t.Fatalf("stop-2 = %+v, want completed 前台运行", byID["stop-2"])
	}
	if byID["other-root"].Status != "running" {
		t.Fatalf("a stop task on another root must stay running")
	}
	if byID["install-1"].Status != "running" {
		t.Fatalf("a non-stop task must stay running")
	}
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

func TestGoals(t *testing.T) {
	handler := newTestServer()
	goals := httptest.NewRecorder()
	handler.ServeHTTP(goals, httptest.NewRequest(http.MethodGet, "/api/v1/goals", nil))
	if goals.Code != http.StatusOK || !bytes.Contains(goals.Body.Bytes(), []byte(`"id":"develop"`)) {
		t.Fatalf("goals response = %d %s", goals.Code, goals.Body.String())
	}
}

func TestRobotTasksStartsAsJSONArray(t *testing.T) {
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/robot/tasks", nil))
	if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("empty robot tasks = %d %q, want JSON array", response.Code, response.Body.String())
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

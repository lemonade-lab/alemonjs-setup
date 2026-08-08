package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"

	"alemonx/internal/access"
	"alemonx/internal/robot"
	"alemonx/internal/setupplugin"
)

var errInternalTest = errors.New("internal test failure")

func newTestServer() http.Handler {
	return NewServer("test", fstest.MapFS{"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}})
}

// TestLoggableRequestBodyRedactsSecretsAndRestoresStream ensures the request
// logger prints the action for the console while redacting tokens/passwords
// and keeping the body readable by the downstream handler.
func TestLoggableRequestBodyRedactsSecretsAndRestoresStream(t *testing.T) {
	s := newStatefulTestServer()
	payload := `{"action":"npm-publish","token":"sekrit","package":"@alemonjs/onebot","values":{"a":"b"}}`
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/robot", bytes.NewBufferString(payload))
	logged := s.loggableRequestBody(ginCtx)
	if strings.Contains(logged, "sekrit") {
		t.Fatalf("token leaked into log: %s", logged)
	}
	if !strings.Contains(logged, `"action":"npm-publish"`) {
		t.Fatalf("action missing from log: %s", logged)
	}
	if !strings.Contains(logged, `"[REDACTED]"`) {
		t.Fatalf("secrets not redacted: %s", logged)
	}
	// The body must still be readable by the handler after logging.
	after, err := io.ReadAll(ginCtx.Request.Body)
	if err != nil || string(after) != payload {
		t.Fatalf("body not restored: %q, %v", after, err)
	}
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
		robots:         robot.Manager{},
		operations:     []operationTask{},
		development:    map[string]developmentProcess{},
		stopping:       map[string]bool{},
		consoleCache:   map[string]consoleSnapshot{},
		pm2Status:      func(string) (robot.PM2Status, error) { return robot.PM2Status{}, nil },
		directoryRoots: managedDirectoryRoots(),
		events:         newRobotEventHub(),
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
}

// TestLocalStartBlockedWhenPM2StatusUnreadable is the strict behaviour: a PM2
// state that cannot be read (daemon mismatch, timeout) must still block a local
// start, otherwise a running PM2 service would silently share the app port.
func TestLocalStartBlockedWhenPM2StatusUnreadable(t *testing.T) {
	root := t.TempDir()
	s := newStatefulTestServer()
	s.pm2Status = func(string) (robot.PM2Status, error) {
		return robot.PM2Status{}, errInternalTest
	}
	message, blocked := s.localStartBlockedByPM2(root)
	if !blocked || !strings.Contains(message, "无法读取后台（PM2）服务状态") {
		t.Fatalf("unreadable PM2 state must block local start, blocked=%v msg=%q", blocked, message)
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

// TestStopPM2ForStartSkipsWhenIdle verifies the "谁最后启动谁为准" helper only
// attempts a PM2 stop when the service is running or unreadable, not when it is
// confirmed idle.
func TestStopPM2ForStartSkipsWhenIdle(t *testing.T) {
	root := t.TempDir()
	s := newStatefulTestServer()
	s.pm2Status = func(string) (robot.PM2Status, error) {
		return robot.PM2Status{Configured: true, Managed: true, Running: false}, nil
	}
	if err := s.stopPM2ForStart(root); err != nil {
		t.Fatalf("idle PM2 should be skipped, got %v", err)
	}
}

// TestStopLocalForStartSkipsWhenNothingRunning verifies a local stop is a no-op
// when no supervised process exists.
func TestStopLocalForStartSkipsWhenNothingRunning(t *testing.T) {
	root := t.TempDir()
	s := newStatefulTestServer()
	if err := s.stopLocalForStart(root); err != nil {
		t.Fatalf("no local process should be a no-op, got %v", err)
	}
}

// TestRobotEventHubPublishesOutput verifies appendOperationOutput fans out an
// incremental output event to SSE subscribers.
func TestRobotEventHubPublishesOutput(t *testing.T) {
	s := newStatefulTestServer()
	s.operations = []operationTask{{ID: "dev-1", Output: ""}}
	sub := s.events.subscribe()
	defer s.events.unsubscribe(sub)
	s.appendOperationOutput("dev-1", "hello line\n")
	select {
	case event := <-sub:
		if event.Type != "output" || event.TaskID != "dev-1" || event.Text != "hello line\n" {
			t.Fatalf("event = %+v, want output for dev-1", event)
		}
	default:
		t.Fatal("expected an output event")
	}
}

// TestIsNumeric validates the PID filter used by forceFreePort.
func TestIsNumeric(t *testing.T) {
	for _, ok := range []string{"1234", "0", "99999"} {
		if !isNumeric(ok) {
			t.Fatalf("%q should be numeric", ok)
		}
	}
	for _, bad := range []string{"", "12a", "-1", "1.5", "abc"} {
		if isNumeric(bad) {
			t.Fatalf("%q should not be numeric", bad)
		}
	}
}

// TestRecordAndForgetProcessPersistsMarker verifies the process marker is
// written and removed from the persisted store.
func TestRecordAndForgetProcessPersistsMarker(t *testing.T) {
	t.Setenv("ALEMONX_PROCESS_FILE", filepath.Join(t.TempDir(), "processes.json"))
	s := newStatefulTestServer()
	root := "/robots/bot"
	s.recordProcess(root, "task-1", 12345, "dev")
	items := loadPersistedProcesses()
	found := false
	for _, item := range items {
		if item.Root == root && item.TaskID == "task-1" && item.PGID == 12345 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("persisted marker not found after record")
	}
	s.forgetProcess(root, "task-1")
	items = loadPersistedProcesses()
	for _, item := range items {
		if item.Root == root && item.TaskID == "task-1" {
			t.Fatal("marker not removed after forget")
		}
	}
	savePersistedProcesses(nil)
}

// TestAppProxyFramePolicy verifies /api/v1/robot/app/ responses are allowed in
// frames (no X-Frame-Options DENY) so the application service can render inside
// the workspace iframe, and are exempt from management auth like webviews.
func TestAppProxyFramePolicy(t *testing.T) {
	router := gin.New()
	router.Use((&server{}).ginHeaders())
	router.Any("/api/v1/robot/app/", func(c *gin.Context) { c.Status(http.StatusOK) })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/robot/app/?root=%2Frobots%2Fbot", nil)
	router.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("app proxy X-Frame-Options = %q, want empty so it can be framed", got)
	}
}

// TestModifyRobotAppResponse verifies the /api/v1/robot/app/ proxy patches
// proxied documents so absolute app links (/app/, /apps/x/) and relative assets
// stay inside the proxy mount (which carries the robot root token) instead of
// escaping to the management page origin, and that redirects stay within the
// mount.
func TestModifyRobotAppResponse(t *testing.T) {
	target, err := url.Parse("http://127.0.0.1:18110")
	if err != nil {
		t.Fatal(err)
	}
	const mount = "/api/v1/robot/app/"
	appPrefix := mount + robotAppToken("/robots/bot") + "/"
	htmlResponse := func(body string) *http.Response {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
		}
	}

	navigation := func(path string) *http.Request {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", "text/html")
		return request
	}

	t.Run("launchpad root", func(t *testing.T) {
		response := htmlResponse(`<html><head><title>launchpad</title></head><body>` +
			`<a href="/app/">Main</a><a href="/apps/demo/">Plugin</a>` +
			`<script src="./assets/app.js"></script>` +
			`<link rel="icon" href="/favicon.ico"></body></html>`)
		modifyRobotAppResponse(response, target, appPrefix, appPrefix, navigation(appPrefix))
		body, _ := io.ReadAll(response.Body)
		for _, want := range []string{
			`<base href="` + appPrefix + `">`,
			`href="` + appPrefix + `app/"`,
			`href="` + appPrefix + `apps/demo/"`,
			`href="` + appPrefix + `favicon.ico"`,
			`src="./assets/app.js"`,
			`scrollbar-width:none`,
		} {
			if !strings.Contains(string(body), want) {
				t.Errorf("launchpad body missing %q\nbody:\n%s", want, body)
			}
		}
		if strings.Contains(string(body), `href="/app/"`) {
			t.Errorf("launchpad body still contains un-prefixed /app/ link\nbody:\n%s", body)
		}
		if got := response.Header.Get("Content-Length"); got != strconv.Itoa(len(body)) {
			t.Errorf("Content-Length = %q, want %d", got, len(body))
		}
	})

	t.Run("plugin page base", func(t *testing.T) {
		path := appPrefix + "apps/demo/"
		response := htmlResponse(`<html><head><title>demo</title><script src="./assets/index.js"></script></head>` +
			`<body><a href="/config/qq">Config</a></body></html>`)
		modifyRobotAppResponse(response, target, appPrefix, path, navigation(path))
		body, _ := io.ReadAll(response.Body)
		for _, want := range []string{
			`<base href="` + path + `">`,
			`src="./assets/index.js"`,
			`href="` + appPrefix + `config/qq"`,
		} {
			if !strings.Contains(string(body), want) {
				t.Errorf("plugin body missing %q\nbody:\n%s", want, body)
			}
		}
	})

	t.Run("no head falls back to html tag", func(t *testing.T) {
		response := htmlResponse(`<html><body><a href="/app/">x</a></body></html>`)
		modifyRobotAppResponse(response, target, appPrefix, appPrefix, navigation(appPrefix))
		body, _ := io.ReadAll(response.Body)
		if !strings.Contains(string(body), `<base href="`+appPrefix+`">`) {
			t.Errorf("missing injected base\nbody:\n%s", body)
		}
	})

	t.Run("scheme and protocol-relative URLs untouched", func(t *testing.T) {
		response := htmlResponse(`<a href="https://example.com/x">a</a><a href="//cdn.example.com/x">b</a>`)
		modifyRobotAppResponse(response, target, appPrefix, appPrefix, navigation(appPrefix))
		body, _ := io.ReadAll(response.Body)
		for _, want := range []string{`href="https://example.com/x"`, `href="//cdn.example.com/x"`} {
			if !strings.Contains(string(body), want) {
				t.Errorf("external URL changed: missing %q\nbody:\n%s", want, body)
			}
		}
	})

	t.Run("redirect re-prefixed", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"/app/"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}
		modifyRobotAppResponse(response, target, appPrefix, appPrefix+"app", navigation(appPrefix+"app"))
		if got := response.Header.Get("Location"); got != appPrefix+"app/" {
			t.Errorf("Location = %q, want %q", got, appPrefix+"app/")
		}
	})

	t.Run("full target-host redirect re-prefixed", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://127.0.0.1:18110/app/?q=1"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}
		modifyRobotAppResponse(response, target, appPrefix, appPrefix+"app", navigation(appPrefix+"app"))
		if got := response.Header.Get("Location"); got != appPrefix+"app/?q=1" {
			t.Errorf("Location = %q, want %q", got, appPrefix+"app/?q=1")
		}
	})

	t.Run("unmatched page navigation returns to launchpad", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":404}`)),
		}
		path := appPrefix + "apps/demo/missing"
		modifyRobotAppResponse(response, target, appPrefix, path, navigation(path))
		if response.StatusCode != http.StatusFound {
			t.Fatalf("status = %d, want 302 redirect to launchpad", response.StatusCode)
		}
		if got := response.Header.Get("Location"); got != appPrefix {
			t.Errorf("Location = %q, want %q", got, appPrefix)
		}
		if body, _ := io.ReadAll(response.Body); len(body) != 0 {
			t.Errorf("redirect body should be empty, got %q", body)
		}
	})

	t.Run("unmatched asset request is not redirected", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":404}`)),
		}
		path := appPrefix + "apps/demo/assets/missing.js"
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", "*/*")
		modifyRobotAppResponse(response, target, appPrefix, path, request)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (asset request must pass through)", response.StatusCode)
		}
		if got := response.Header.Get("Location"); got != "" {
			t.Errorf("asset request must not redirect, got Location %q", got)
		}
	})

	t.Run("launchpad 404 never redirects to itself", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		}
		modifyRobotAppResponse(response, target, appPrefix, appPrefix, navigation(appPrefix))
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("launchpad 404 must stay 404, got %d", response.StatusCode)
		}
	})

	t.Run("assets and API payloads pass through", func(t *testing.T) {
		response := htmlResponse(`{"api":"/status"}`)
		response.Header.Set("Content-Type", "application/json")
		path := appPrefix + "api/status"
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", "application/json")
		modifyRobotAppResponse(response, target, appPrefix, path, request)
		body, _ := io.ReadAll(response.Body)
		if string(body) != `{"api":"/status"}` {
			t.Errorf("JSON body changed: %q", body)
		}
	})
}

// TestRobotAppTokenRoundTrip verifies the root token survives a decode round
// trip and that the legacy query form still resolves from the handler.
func TestRobotAppTokenRoundTrip(t *testing.T) {
	const root = "/Users/lemonade/Desktop/alemonjs-setup/alemonb"
	token := robotAppToken(root)
	decoded, raw := robotAppRootFromPath("/api/v1/robot/app/"+token+"/apps/demo/", "/api/v1/robot/app/")
	if decoded != root || raw != token {
		t.Fatalf("round trip = (%q, %q), want (%q, %q)", decoded, raw, root, token)
	}
	if strings.ContainsAny(token, "+/=") {
		t.Fatalf("token must be base64url-safe, got %q", token)
	}
	// A path whose first segment is not valid base64url must yield no root.
	if root, _ := robotAppRootFromPath("/api/v1/robot/app/apps/demo/", "/api/v1/robot/app/"); root != "" {
		t.Fatalf("non-token path decoded root = %q, want empty", root)
	}
}

// flushWriter adapts httptest.ResponseRecorder to http.Flusher so the SSE
// handler can be driven without binding a real port (the test sandbox forbids
// listen).
type flushWriter struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (f *flushWriter) Flush() {}
func (f *flushWriter) Write(data []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ResponseRecorder.Write(data)
}
func (f *flushWriter) Snapshot() string { f.mu.Lock(); defer f.mu.Unlock(); return f.Body.String() }

// TestSetupPluginEventsSSE verifies /setup/plugins/events emits a change event
// when the plugin registry changes and stays quiet otherwise.
func TestSetupPluginEventsSSE(t *testing.T) {
	root := t.TempDir()
	registry := setupplugin.NewRegistry(root)
	registry.Rescan()
	s := &server{plugins: registry}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/plugins/events", nil).WithContext(ctx)
	recorder := &flushWriter{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		s.setupPluginEventsHandler(recorder, request)
		close(done)
	}()

	// No initial event is emitted.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(recorder.Snapshot()) > 0 {
			cancel()
			t.Fatalf("unexpected early SSE data: %q", recorder.Snapshot())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Adding a plugin bumps the revision and must push a change event.
	directory := filepath.Join(root, "newone")
	if err := os.MkdirAll(filepath.Join(directory, "web"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"newone","name":"New","version":"1.0.0","web":{"root":"web"}}`
	if err := os.WriteFile(filepath.Join(directory, "alx.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	registry.Rescan()

	waitFor := time.Now().Add(time.Second)
	for !strings.Contains(recorder.Snapshot(), "data: {}") && time.Now().Before(waitFor) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(recorder.Snapshot(), "data: {}") {
		t.Fatalf("SSE stream did not emit a change event, body: %q", recorder.Snapshot())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
}

// TestSetupPluginWebEmbeddable verifies setup plugin pages are frame-embeddable
// (ginHeaders must not add X-Frame-Options DENY), so the management UI can
// render them in an iframe.
func TestSetupPluginWebEmbeddable(t *testing.T) {
	router := gin.New()
	router.Use((&server{}).ginHeaders())
	router.Any("/api/v1/setup/plugins/web/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/plugins/web/alemonx-qq/index.html", nil)
	router.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("setup plugin web X-Frame-Options = %q, want empty", got)
	}
}

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAgentChatRejectsInvalidRoot verifies the endpoint wiring: an invalid
// project root is rejected with a clean JSON error before any provider
// resolution, in both the plain and streaming modes.
func TestAgentChatRejectsInvalidRoot(t *testing.T) {
	for _, stream := range []string{"0", "1"} {
		body := `{"provider":"deepseek","root":"/definitely/not/a/robot","messages":[{"role":"user","content":"你好"}]}`
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat?stream="+stream, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		newTestServer().ServeHTTP(response, request)

		if response.Code != http.StatusBadRequest {
			t.Fatalf("stream=%s 状态码 = %d，应为 400", stream, response.Code)
		}
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("stream=%s 响应不是 JSON：%s", stream, response.Body.String())
		}
		if !strings.Contains(payload.Error, "机器人目录") {
			t.Errorf("stream=%s 错误消息错误：%q", stream, payload.Error)
		}
	}
}

// TestAgentChatRejectsEmptyMessages guards the request contract: no messages
// means no work is attempted.
func TestAgentChatRejectsEmptyMessages(t *testing.T) {
	body := `{"provider":"deepseek","root":".","messages":[]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，应为 400", response.Code)
	}
}

// TestAgentChatValidatesDecodedAccess ensures the permission mode is read
// after JSON decoding. This guards the ask/auto/full confirmation contract.
func TestAgentChatRejectsInvalidAccess(t *testing.T) {
	body := `{"access":"unsafe","root":"/definitely/not/a/robot","messages":[{"role":"user","content":"你好"}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，应为 400", response.Code)
	}
	if !strings.Contains(response.Body.String(), "权限模式无效") {
		t.Fatalf("应返回权限模式错误，实际响应：%s", response.Body.String())
	}
}

func TestAgentTasksRejectInvalidRoot(t *testing.T) {
	body := `{"provider":"deepseek","root":"/definitely/not/a/robot","messages":[{"role":"user","content":"你好"}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/tasks", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "机器人目录") {
		t.Fatalf("任务接口应拒绝无效目录：%d %s", response.Code, response.Body.String())
	}
}

// TestAgentSessionsInvalidRoot verifies session creation rejects an invalid
// project root with a clean JSON error.
func TestAgentSessionsInvalidRoot(t *testing.T) {
	body := `{"root":"/definitely/not/a/robot","provider":"deepseek","model":"m"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/sessions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，应为 400", response.Code)
	}
}

// TestAgentSessionInvalidID guards the session detail route contract.
func TestAgentSessionInvalidID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/foo/bar", nil)
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，应为 400", response.Code)
	}
}

func TestTitleFromMessage(t *testing.T) {
	cases := []struct{ in, want string }{
		{"帮我看一下机器人项目结构", "帮我看一下机器人"},
		{"你好", "你好"},
		{"  修复登录报错  ", "修复登录报错"},
		{"# 优化性能", "优化性能"},
		{"很短的", "很短的"},
	}
	for _, c := range cases {
		if got := titleFromMessage(c.in); got != c.want {
			t.Errorf("titleFromMessage(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestCORSDevOrigin validates the preflight response for the Vite dev origin,
// which the SSE direct-connect path relies on.
func TestCORSDevOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/agent/chat?stream=1", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("预检状态码 = %d，应为 204", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("缺少 Access-Control-Allow-Methods")
	}
}

// TestCORSUnknownOriginRejected ensures non-dev origins get no CORS headers.
func TestCORSUnknownOriginRejected(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/agent/chat?stream=1", nil)
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("未知 origin 不应有 CORS 头：%q", got)
	}
}

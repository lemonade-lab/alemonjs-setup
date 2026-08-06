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

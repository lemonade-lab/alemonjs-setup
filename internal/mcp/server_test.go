package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestServeInitializesAndListsTools(t *testing.T) {
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n")
	var output bytes.Buffer
	if err := NewServer("test", fstest.MapFS{}).Serve(input, &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("response count = %d, want 2: %s", len(lines), output.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize response = %s", lines[0])
	}
	if !strings.Contains(lines[1], `"name":"alemonjs_write_project_file"`) || !strings.Contains(lines[1], `"name":"alemonjs_start_project_action"`) {
		t.Fatalf("tools/list response = %s", lines[1])
	}
}

func TestWriteToolRequiresConfirmation(t *testing.T) {
	arguments, err := json.Marshal(map[string]any{"root": "/not-a-project", "action": "build", "confirm": false})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewServer("test", nil).execute("alemonjs_start_project_action", arguments)
	if err == nil || result != "" || !strings.Contains(err.Error(), "confirm=true") {
		t.Fatalf("execute() = %q, %v; want confirmation error", result, err)
	}
}

func TestUnknownToolReturnsMCPToolError(t *testing.T) {
	response := NewServer("test", nil).handle(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: json.RawMessage(`{"name":"unknown","arguments":{}}`)})
	encoded, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	if response.Error != nil || !strings.Contains(string(encoded), `"isError":true`) {
		t.Fatalf("response = %+v", response)
	}
}

func TestProjectActionRunsAsPollableTask(t *testing.T) {
	server := NewServer("test", nil)
	arguments, err := json.Marshal(map[string]any{"root": "/not-a-project", "action": "build", "confirm": true})
	if err != nil {
		t.Fatal(err)
	}
	text, err := server.execute("alemonjs_start_project_action", arguments)
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	var started operationTask
	if err := json.Unmarshal([]byte(text), &started); err != nil || started.ID == "" || started.Status != "running" {
		t.Fatalf("started task = %s, %v", text, err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		current, ok := server.getTask(started.ID)
		if ok && current.Status == "failed" {
			return
		}
	}
	t.Fatalf("task %s did not complete with expected failure", started.ID)
}

func TestHTTPHandlerRequiresBearerTokenAndServesMCP(t *testing.T) {
	server := NewServer("test", nil)
	unauthorized := httptest.NewRecorder()
	server.HTTPHandler("secret").ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	request.Header.Set("Authorization", "Bearer secret")
	server.HTTPHandler("secret").ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize HTTP response = %d %s", authorized.Code, authorized.Body.String())
	}
}

func TestPolicyRejectsProjectOutsideAllowedRoots(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	server := NewServerWithPolicy("test", nil, Policy{AllowedRoots: []string{allowed}})
	arguments, err := json.Marshal(map[string]any{"root": outside})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.execute("alemonjs_project_status", arguments)
	if err == nil || !strings.Contains(err.Error(), "MCP_ALLOWED_ROOTS") {
		t.Fatalf("policy error = %v", err)
	}
}

package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestToolSchemasNeverEmitNullRequired(t *testing.T) {
	encoded, err := json.Marshal(tools())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"required":null`) {
		t.Fatalf("tool schema contains invalid null required: %s", encoded)
	}
}

func TestServeAcceptsInitializedNotificationAndReadsCapabilities(t *testing.T) {
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\",\"capabilities\":{},\"clientInfo\":{\"name\":\"Codex\",\"version\":\"test\"}}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"resources/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"resources/read\",\"params\":{\"uri\":\"alemonjs://mcp/capabilities\"}}\n")
	var output bytes.Buffer
	if err := NewServer("test", fstest.MapFS{}).Serve(input, &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("response count = %d, want 3: %s", len(lines), output.String())
	}
	if !strings.Contains(lines[1], "alemonjs://mcp/capabilities") || !strings.Contains(lines[2], "protected local Streamable HTTP") {
		t.Fatalf("resource responses = %q / %q", lines[1], lines[2])
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
	if got := authorized.Header().Get("MCP-Protocol-Version"); got != protocolVersion {
		t.Fatalf("response protocol version = %q", got)
	}

	unsupportedVersion := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("MCP-Protocol-Version", "1999-01-01")
	server.HTTPHandler("secret").ServeHTTP(unsupportedVersion, request)
	if unsupportedVersion.Code != http.StatusBadRequest {
		t.Fatalf("unsupported protocol status = %d", unsupportedVersion.Code)
	}

	foreignOrigin := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Origin", "https://untrusted.example")
	server.HTTPHandler("secret").ServeHTTP(foreignOrigin, request)
	if foreignOrigin.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin status = %d", foreignOrigin.Code)
	}

	get := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/mcp", nil)
	request.Header.Set("Authorization", "Bearer secret")
	server.HTTPHandler("secret").ServeHTTP(get, request)
	if get.Code != http.StatusMethodNotAllowed || get.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("GET stream response = %d Allow=%q", get.Code, get.Header().Get("Allow"))
	}

	notification := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("MCP-Protocol-Version", protocolVersion)
	server.HTTPHandler("secret").ServeHTTP(notification, request)
	if notification.Code != http.StatusAccepted || notification.Body.Len() != 0 {
		t.Fatalf("notification HTTP response = %d %q", notification.Code, notification.Body.String())
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

func TestPolicyRejectsSymlinkEscapingAllowedRoot(t *testing.T) {
	allowed, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "package.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	server := NewServerWithPolicy("test", nil, Policy{AllowedRoots: []string{allowed}})
	arguments, err := json.Marshal(map[string]any{"root": link})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.execute("alemonjs_project_status", arguments)
	if err == nil || !strings.Contains(err.Error(), "MCP_ALLOWED_ROOTS") {
		t.Fatalf("symlink policy error = %v", err)
	}
}

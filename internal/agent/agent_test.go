package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"alemonx/internal/ai"
)

// roundTripFunc adapts a function to http.RoundTripper so tests can serve
// canned provider responses without binding a network port.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// withTransport installs a fake transport for the package-level client and
// restores the original on test end.
func withTransport(t *testing.T, fn roundTripFunc) *http.Request {
	t.Helper()
	original := httpClient.Transport
	httpClient.Transport = fn
	t.Cleanup(func() { httpClient.Transport = original })
	return &http.Request{}
}

func jsonResponse(t *testing.T, req *http.Request, payload string) (*http.Response, error) {
	t.Helper()
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    req,
	}, nil
}

func toolsFixture() []Tool {
	return []Tool{
		{
			Name:        "read_project_file",
			Description: "读取项目文件",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"},
			},
		},
	}
}

func messageFixture() []Message {
	return []Message{
		{Role: "user", Content: "看一下 src/index.js"},
	}
}

func TestOpenAIPayload(t *testing.T) {
	var got map[string]any
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/chat/completions" {
			t.Fatalf("路径错误：%s", req.URL.Path)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization 错误：%q", got)
		}
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			t.Fatalf("请求体解析失败：%v", err)
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_project_file","arguments":"{\"path\":\"src/index.js\"}"}}]},"finish_reason":"tool_calls"}]}`)
	})

	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "test-model", APIKey: "secret"}
	result, err := RoundTrip(context.Background(), cfg, messageFixture(), toolsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if got["stream"] != false {
		t.Errorf("OpenAI payload 应禁用 stream")
	}
	wire, _ := got["messages"].([]any)
	if len(wire) != 1 {
		t.Fatalf("messages 数量错误：%d", len(wire))
	}
	tools, _ := got["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools 数量错误：%d", len(tools))
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "read_project_file" {
		t.Fatalf("ToolCalls 解析错误：%+v", result.ToolCalls)
	}
	if string(result.ToolCalls[0].Arguments) != `{"path":"src/index.js"}` {
		t.Errorf("Arguments 解析错误：%s", result.ToolCalls[0].Arguments)
	}
	if result.StopReason != "tool_calls" {
		t.Errorf("StopReason 错误：%s", result.StopReason)
	}
}

func TestAnthropicPayloadAndToolResultMerge(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "看一下 src/index.js"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "tool_1", Name: "read_project_file", Arguments: json.RawMessage(`{"path":"src/index.js"}`)}}},
		{Role: "tool", ToolCallID: "tool_1", Content: "文件内容"},
	}
	var got map[string]any
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/messages" {
			t.Fatalf("路径错误：%s", req.URL.Path)
		}
		if got := req.Header.Get("x-api-key"); got != "secret" {
			t.Fatalf("x-api-key 错误：%q", got)
		}
		if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("anthropic-version 错误：%q", got)
		}
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			t.Fatalf("请求体解析失败：%v", err)
		}
		return jsonResponse(t, req, `{"content":[{"type":"text","text":"看到了。"}],"stop_reason":"end_turn"}`)
	})

	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "test-model", APIKey: "secret", Anthropic: true}
	result, err := RoundTrip(context.Background(), cfg, messages, toolsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "看到了。" {
		t.Errorf("Content 错误：%q", result.Content)
	}
	if got["system"] != "system prompt" {
		t.Errorf("system 未正确提取：%v", got["system"])
	}
	wire, _ := got["messages"].([]any)
	if len(wire) != 3 {
		t.Fatalf("messages 数量错误：%d", len(wire))
	}
	last, _ := wire[2].(map[string]any)
	if last["role"] != "user" {
		t.Fatalf("tool_result 应合并进 user 消息，实际 role=%v", last["role"])
	}
	blocks, _ := last["content"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("tool_result block 数量错误：%d", len(blocks))
	}
	block, _ := blocks[0].(map[string]any)
	if block["type"] != "tool_result" || block["tool_use_id"] != "tool_1" || block["content"] != "文件内容" {
		t.Errorf("tool_result block 错误：%+v", block)
	}
}

func TestAnthropicToolUseParsing(t *testing.T) {
	var got map[string]any
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(req.Body).Decode(&got)
		return jsonResponse(t, req, `{"content":[{"type":"tool_use","id":"tool_9","name":"read_project_file","input":{"path":"src/index.js"}}],"stop_reason":"tool_use"}`)
	})

	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "test-model", APIKey: "secret", Anthropic: true}
	result, err := RoundTrip(context.Background(), cfg, messageFixture(), toolsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "read_project_file" {
		t.Fatalf("ToolCalls 解析错误：%+v", result.ToolCalls)
	}
	if string(result.ToolCalls[0].Arguments) != `{"path":"src/index.js"}` {
		t.Errorf("Arguments 错误：%s", result.ToolCalls[0].Arguments)
	}
	if result.StopReason != "tool_use" {
		t.Errorf("StopReason 错误：%s", result.StopReason)
	}
	tools, _ := got["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("Anthropic tools 数量错误：%d", len(tools))
	}
	first, _ := tools[0].(map[string]any)
	if first["name"] != "read_project_file" || first["input_schema"] == nil {
		t.Errorf("Anthropic tool 结构错误：%+v", first)
	}
}

// TestLoopToolRoundTrip drives the loop against a fake provider that asks for
// one tool call, receives the result, then answers. This proves the loop feeds
// tool results back and terminates on a final text response.
func TestLoopToolRoundTrip(t *testing.T) {
	calls := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(req.Body).Decode(&payload)
		var roles []string
		for _, m := range payload.Messages {
			roles = append(roles, m["role"].(string))
		}
		calls++
		if calls == 2 && !strings.Contains(strings.Join(roles, ","), "tool") {
			t.Errorf("第 2 轮应包含 tool 消息，roles=%s", strings.Join(roles, ","))
		}
		switch calls {
		case 1:
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_project_file","arguments":"{\"path\":\"src/index.js\"}"}}]},"finish_reason":"tool_calls"}]}`)
		case 2:
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"文件内容是 export const x = 1"},"finish_reason":"stop"}]}`)
		default:
			t.Fatalf("调用次数过多：%d", calls)
			return nil, nil
		}
	})

	registry := NewRegistry()
	registry.Add(Tool{Name: "read_project_file", Description: "读取文件", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "文件内容是 export const x = 1", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "test-model", APIKey: "secret"}
	loop := NewLoop(cfg, registry, "system prompt", 10)
	result, err := loop.Run(context.Background(), messageFixture())
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "文件内容是 export const x = 1" {
		t.Errorf("Answer 错误：%q", result.Answer)
	}
	if calls != 2 {
		t.Errorf("应调用 provider 2 次，实际 %d", calls)
	}
	if !hasRole(result.Messages, "tool") || !hasRole(result.Messages, "assistant") {
		t.Errorf("transcript 应包含 assistant 与 tool 消息：%+v", rolesOf(result.Messages))
	}
}

// TestLoopUnknownTool ensures an unknown tool name becomes an error tool
// result instead of crashing the loop.
func TestLoopUnknownTool(t *testing.T) {
	calls := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"nope","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"好的"},"finish_reason":"stop"}]}`)
	})

	registry := NewRegistry()
	registry.Add(Tool{Name: "read_project_file", Description: "读取文件", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "test-model", APIKey: "secret"}
	loop := NewLoop(cfg, registry, "", 10)
	result, err := loop.Run(context.Background(), messageFixture())
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "好的" {
		t.Errorf("Answer 错误：%q", result.Answer)
	}
	for _, m := range result.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "未知工具") {
			return
		}
	}
	t.Errorf("未知工具应产生错误 tool 消息：%+v", rolesOf(result.Messages))
}

// TestLoopEmitsEventsInOrder asserts the observer receives the exact event
// sequence a streaming client consumes: tool → result → done.
func TestLoopEmitsEventsInOrder(t *testing.T) {
	calls := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"read_project_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"最终答案"},"finish_reason":"stop"}]}`)
	})

	registry := NewRegistry()
	registry.Add(Tool{Name: "read_project_file", Description: "读文件", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "内容", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}
	events := []Event{}
	loop := NewLoop(cfg, registry, "", 10)
	loop.WithObserver(func(event Event) { events = append(events, event) })
	if _, err := loop.Run(context.Background(), messageFixture()); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Type)
	}
	want := []string{"tool", "result", "done"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("事件序列错误：%v，应为 %v", kinds, want)
	}
	if events[0].Tool != "read_project_file" {
		t.Errorf("tool 事件应带工具名：%+v", events[0])
	}
	if events[1].Output != "内容" {
		t.Errorf("result 事件应带输出：%+v", events[1])
	}
	if events[2].Text != "最终答案" {
		t.Errorf("done 事件应带最终答案：%+v", events[2])
	}
}

// TestLoopEmitsErrorEventOnProviderFailure ensures a provider error surfaces
// as an error event so a streaming client can show it.
func TestLoopEmitsErrorEventOnProviderFailure(t *testing.T) {
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"服务不可用"}}`)), Header: http.Header{}, Request: req}, nil
	})

	registry := NewRegistry()
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}
	events := []Event{}
	loop := NewLoop(cfg, registry, "", 10)
	loop.WithObserver(func(event Event) { events = append(events, event) })
	_, err := loop.Run(context.Background(), messageFixture())
	if err == nil {
		t.Fatal("provider 500 应返回错误")
	}
	if len(events) == 0 || events[len(events)-1].Type != "error" {
		t.Fatalf("应发出 error 事件：%+v", events)
	}
	if !strings.Contains(events[len(events)-1].Text, "服务不可用") {
		t.Errorf("error 事件应含 provider 消息：%+v", events[len(events)-1])
	}
}

// TestLoopApproverRejects ensures a rejected tool call is fed back to the
// model as a refusal instead of executing or crashing.
func TestLoopApproverRejects(t *testing.T) {
	calls := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"write_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"明白，我不改了"},"finish_reason":"stop"}]}`)
	})

	executed := false
	registry := NewRegistry()
	registry.AddWrite(Tool{Name: "write_file", Description: "写文件", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		executed = true
		return "已写入", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "test-model", APIKey: "secret"}
	loop := NewLoop(cfg, registry, "", 10)
	loop.WithApprover(func(ctx context.Context, call ToolCall) error {
		return errors.New("用户拒绝")
	})
	result, err := loop.Run(context.Background(), messageFixture())
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Fatal("被拒绝的工具不应执行")
	}
	found := false
	for _, m := range result.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "用户拒绝") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("拒绝应作为 tool 消息回传给模型：%+v", rolesOf(result.Messages))
	}
}

// TestLoopMaxTurns confirms the loop stops at the turn budget.
func TestLoopMaxTurns(t *testing.T) {
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"read_project_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	})

	registry := NewRegistry()
	registry.Add(Tool{Name: "read_project_file", Description: "读取文件", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "test-model", APIKey: "secret"}
	loop := NewLoop(cfg, registry, "", 3)
	if _, err := loop.Run(context.Background(), messageFixture()); err == nil {
		t.Fatal("应达到最大轮数并报错")
	}
}

func hasRole(messages []Message, role string) bool {
	for _, m := range messages {
		if m.Role == role {
			return true
		}
	}
	return false
}

func rolesOf(messages []Message) []string {
	roles := make([]string, 0, len(messages))
	for _, m := range messages {
		roles = append(roles, m.Role)
	}
	return roles
}

// TestLoopSkipsApproverForReadTools ensures read-only tools never hit the
// approval gate — the regression that blocked list/read/search for users.
func TestLoopSkipsApproverForReadTools(t *testing.T) {
	calls := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"r","type":"function","function":{"name":"read_project_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"读到了"},"finish_reason":"stop"}]}`)
	})

	approverCalled := false
	executed := false
	registry := NewRegistry()
	registry.Add(Tool{Name: "read_project_file", Description: "读文件", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		executed = true
		return "内容", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "test-model", APIKey: "secret"}
	loop := NewLoop(cfg, registry, "", 10)
	loop.WithApprover(func(ctx context.Context, call ToolCall) error {
		approverCalled = true
		return errors.New("不应调用")
	})
	result, err := loop.Run(context.Background(), messageFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !executed {
		t.Fatal("只读工具应直接执行")
	}
	if approverCalled {
		t.Error("只读工具不应经过 approver")
	}
	if result.Answer != "读到了" {
		t.Errorf("Answer 错误：%q", result.Answer)
	}
}

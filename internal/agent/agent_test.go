package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

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
	want := []string{"turn", "tool", "result", "turn", "done"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("事件序列错误：%v，应为 %v", kinds, want)
	}
	if events[1].Tool != "read_project_file" {
		t.Errorf("tool 事件应带工具名：%+v", events[1])
	}
	if events[1].CallID != "c" || events[2].CallID != "c" {
		t.Errorf("tool/result 事件应共享 call ID：%+v", events)
	}
	if events[2].Output != "内容" {
		t.Errorf("result 事件应带输出：%+v", events[2])
	}
	if events[4].Text != "最终答案" {
		t.Errorf("done 事件应带最终答案：%+v", events[4])
	}
}

// TestLoopEmitsErrorEventOnProviderFailure ensures a provider failure pauses
// safely without leaking the provider's raw implementation message to users.
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
	if events[len(events)-1].Text != userSafeModelFailure {
		t.Errorf("error 事件应使用脱敏提示：%+v", events[len(events)-1])
	}
	if !IsRecoverable(err) {
		t.Errorf("provider 故障应标记为可恢复：%T %v", err, err)
	}
}

// TestLoopMixedToolCallsKeepResponsesContiguous guards the OpenAI/DeepSeek
// protocol invariant that every result of an assistant tool_calls batch is
// emitted together before the synthetic verification exchange begins.
func TestLoopMixedToolCallsKeepResponsesContiguous(t *testing.T) {
	var requests []struct {
		Messages []map[string]any `json:"messages"`
	}
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, payload)
		if len(requests) == 1 {
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"write","type":"function","function":{"name":"agent_edit_file","arguments":"{}"}},{"id":"read","type":"function","function":{"name":"agent_read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"完成"},"finish_reason":"stop"}]}`)
	})

	registry := NewRegistry()
	registry.AddWrite(Tool{Name: "agent_edit_file", Parameters: map[string]any{"type": "object"}}, func(context.Context, json.RawMessage) (string, error) { return "已修改", nil })
	registry.Add(Tool{Name: "agent_read_file", Parameters: map[string]any{"type": "object"}}, func(context.Context, json.RawMessage) (string, error) { return "已读取", nil })
	registry.Add(Tool{Name: verifyToolName, Parameters: map[string]any{"type": "object"}}, func(context.Context, json.RawMessage) (string, error) { return "验证通过", nil })
	loop := NewLoop(ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}, registry, "", 3).WithAutoVerify()
	if _, err := loop.Run(context.Background(), []Message{{Role: "user", Content: "修改并读取"}}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("应有两次模型请求，实际 %d", len(requests))
	}
	var roles []string
	for _, message := range requests[1].Messages {
		roles = append(roles, message["role"].(string))
	}
	// user, assistant(original calls), tool(write), tool(read), assistant(verify), tool(verify)
	want := "user,assistant,tool,tool,assistant,tool"
	if got := strings.Join(roles, ","); got != want {
		t.Fatalf("工具结果必须连续且验证在其后，得到 %s，应为 %s", got, want)
	}
}

func TestLoopRetriesRecoverableToolProtocolFailure(t *testing.T) {
	requests := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"An assistant message with tool_calls must be followed by tool messages"}}`)), Header: http.Header{}, Request: req}, nil
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"已自动恢复并继续完成。"},"finish_reason":"stop"}]}`)
	})
	var events []Event
	loop := NewLoop(ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}, NewRegistry(), "", 2)
	loop.WithObserver(func(event Event) { events = append(events, event) })
	result, err := loop.Run(context.Background(), []Message{{Role: "user", Content: "继续"}})
	if err != nil {
		t.Fatalf("协议错误应自动恢复：%v", err)
	}
	if requests != 2 || result.Answer != "已自动恢复并继续完成。" {
		t.Fatalf("应重试一次并完成，请求=%d，结果=%+v", requests, result)
	}
	foundRetry := false
	for _, event := range events {
		if event.Type == "text" && strings.Contains(event.Text, "自动重试") {
			foundRetry = true
		}
	}
	if !foundRetry {
		t.Fatalf("应向用户给出不含原始错误的重试进度：%+v", events)
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

func TestCompressIfOverBudget(t *testing.T) {
	// 大 content 消息使总量远超 budget。
	messages := []Message{
		{Role: "user", Content: "问题"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "t1", Name: "read_project_file", Arguments: nil}}},
		{Role: "tool", ToolCallID: "t1", Content: strings.Repeat("a", 50000)},
		{Role: "assistant", Content: "继续"},
		{Role: "tool", ToolCallID: "t1", Content: "最近结果"},
	}
	compressed := compressIfOverBudget(messages, 1000)
	if len(compressed) >= len(messages) {
		t.Errorf("应压缩消息，原 %d 条 → %d 条", len(messages), len(compressed))
	}
	// 折叠标记应存在。
	foundNote := false
	for _, message := range compressed {
		if message.Role == "user" && strings.Contains(message.Content, "已折叠") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Errorf("压缩后应含折叠标记：%+v", compressed)
	}
	// 最近的消息应保留；末尾的孤立 tool（无前置 tool_calls）会被清除。
	last := compressed[len(compressed)-1]
	if last.Role != "assistant" || last.Content != "继续" {
		t.Errorf("最近合法消息应保留：%+v", last)
	}
}

func TestCompressIfOverBudgetDisabled(t *testing.T) {
	messages := []Message{{Role: "user", Content: strings.Repeat("x", 5000)}}
	if got := compressIfOverBudget(messages, 0); len(got) != len(messages) {
		t.Error("budget=0 时不应压缩")
	}
}

func TestCompressIfOverBudgetUnderLimit(t *testing.T) {
	messages := []Message{{Role: "user", Content: "短"}}
	if got := compressIfOverBudget(messages, 10000); len(got) != 1 {
		t.Error("未超限时不应压缩")
	}
}

func TestPruneOrphanTools(t *testing.T) {
	// 孤立 tool（无前置 tool_calls）应被删除。
	messages := []Message{
		{Role: "tool", ToolCallID: "orphan", Content: "没有对应声明"},
		{Role: "user", Content: "问题"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "t1", Name: "read_project_file", Arguments: nil}}},
		{Role: "tool", ToolCallID: "t1", Content: "结果"},
	}
	pruned := PruneOrphanTools(messages)
	if len(pruned) != 3 {
		t.Fatalf("应删除孤立 tool，保留 3 条，实际 %d：%+v", len(pruned), pruned)
	}
	if pruned[0].Role != "user" {
		t.Errorf("第一个应为 user：%+v", pruned[0])
	}
}

// 压缩后若边界切开 tool_calls/tool 配对，孤立 tool 应被删除。
func TestCompressDropsOrphanedTools(t *testing.T) {
	// 大 output 使总超限；让 assistant(tool_calls) 在折叠边界外。
	messages := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "t1", Name: "read_project_file", Arguments: nil}}},
		{Role: "tool", ToolCallID: "t1", Content: strings.Repeat("x", 50000)},
		{Role: "user", Content: "继续"},
	}
	compressed := compressIfOverBudget(messages, 500)
	for _, message := range compressed {
		if message.Role == "tool" {
			t.Fatalf("不应存在孤立 tool 消息：%+v", message)
		}
	}
}

// TestParallelReadTools verifies read-only tools in one round run concurrently
// (not serially), cutting latency when the model asks for multiple reads.
func TestParallelReadTools(t *testing.T) {
	// 第一轮返回两个只读调用，第二轮停止。
	calls := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"a","type":"function","function":{"name":"read_project_file","arguments":"{}"}},{"id":"b","type":"function","function":{"name":"read_project_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"完成"},"finish_reason":"stop"}]}`)
	})
	active := 0
	var mu sync.Mutex
	var maxActive int
	registry := NewRegistry()
	registry.Add(Tool{Name: "read_project_file", Description: "读", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		// 阻塞模拟耗时读。
		select {
		case <-ctx.Done():
			return "取消", ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		mu.Lock()
		active--
		mu.Unlock()
		return "内容", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}
	loop := NewLoop(cfg, registry, "", 3)
	if _, err := loop.Run(context.Background(), []Message{{Role: "user", Content: "读两个文件"}}); err != nil {
		t.Fatal(err)
	}
	if maxActive < 2 {
		t.Errorf("两个只读工具应并发执行，maxActive=%d", maxActive)
	}
}

// TestLoopMessagesIncludeFinalAnswer ensures the returned transcript contains
// the final assistant answer, so session persistence can restore it.
func TestLoopMessagesIncludeFinalAnswer(t *testing.T) {
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"最终总结"},"finish_reason":"stop"}]}`)
	})
	registry := NewRegistry()
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}
	loop := NewLoop(cfg, registry, "", 10)
	result, err := loop.Run(context.Background(), messageFixture())
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "最终总结" {
		t.Errorf("Answer 错误：%q", result.Answer)
	}
	found := false
	for _, message := range result.Messages {
		if message.Role == "assistant" && message.Content == "最终总结" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("transcript 应包含最终 assistant 回答：%+v", rolesOf(result.Messages))
	}
}

// TestPruneOrphanToolsRemovesUnansweredCalls ensures an assistant message
// carrying tool_calls without matching tool responses is pruned — the exact
// case DeepSeek rejects with "tool_calls must be followed by tool messages".
func TestPruneOrphanToolsRemovesUnansweredCalls(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "问题"},
		{Role: "assistant", Content: "调用工具", ToolCalls: []ToolCall{{ID: "t1", Name: "read_project_file", Arguments: nil}}},
		// t1 没有对应的 tool 响应 —— 孤立
		{Role: "assistant", Content: "最终回答"},
	}
	pruned := PruneOrphanTools(messages)
	for _, message := range pruned {
		if len(message.ToolCalls) > 0 {
			t.Fatalf("不应保留无响应的 tool_calls：%+v", message)
		}
	}
	if len(pruned) != 2 {
		t.Errorf("应保留 user 和最终回答：%+v", rolesOf(pruned))
	}
}

func TestPruneOrphanToolsRequiresContiguousResponses(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "问题"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "t1", Name: "read_project_file"}}},
		// The matching ID exists, but a user turn cut the required contiguous
		// assistant/tool group. This is rejected by OpenAI-compatible APIs.
		{Role: "user", Content: "补充说明"},
		{Role: "tool", ToolCallID: "t1", Content: "结果"},
		{Role: "assistant", Content: "继续"},
	}
	pruned := PruneOrphanTools(messages)
	for _, message := range pruned {
		if len(message.ToolCalls) > 0 || message.Role == "tool" {
			t.Fatalf("不应保留被打断的工具交换：%+v", message)
		}
	}
	if got := rolesOf(pruned); !reflect.DeepEqual(got, []string{"user", "user", "assistant"}) {
		t.Fatalf("清理后的角色序列 = %v", got)
	}
}

func TestPruneToolCallsMissingReasoningKeepsSurroundingConversation(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "检查项目"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "legacy", Name: "agent_repo_map"}}},
		{Role: "tool", ToolCallID: "legacy", Content: "旧索引结果"},
		{Role: "user", Content: "继续"},
		{Role: "assistant", ReasoningContent: "完整推理", ToolCalls: []ToolCall{{ID: "new", Name: "agent_search"}}},
		{Role: "tool", ToolCallID: "new", Content: "新结果"},
	}
	pruned, changed := PruneToolCallsMissingReasoning(messages)
	if !changed {
		t.Fatal("缺少 reasoning_content 的工具回合应被识别")
	}
	if got := rolesOf(pruned); !reflect.DeepEqual(got, []string{"user", "user", "assistant", "tool"}) {
		t.Fatalf("清理后角色序列 = %v", got)
	}
	if pruned[2].ReasoningContent != "完整推理" {
		t.Fatalf("完整的 thinking 工具回合不应被删除：%+v", pruned[2])
	}
}

func TestLoopRecoversFromLegacyThinkingToolCall(t *testing.T) {
	calls := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		calls++
		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(req.Body).Decode(&payload)
		for _, message := range payload.Messages {
			if message["role"] == "assistant" && message["tool_calls"] != nil && message["reasoning_content"] == nil {
				return jsonResponse(t, req, `{"error":{"message":"The reasoning_content in the thinking mode must be passed back to the API."}}`)
			}
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"已从恢复点继续"},"finish_reason":"stop"}]}`)
	})
	loop := NewLoop(ai.Resolved{BaseURL: "https://provider.test", Model: "deepseek-reasoner", APIKey: "k"}, NewRegistry(), "", 3)
	result, err := loop.Run(context.Background(), []Message{
		{Role: "user", Content: "恢复任务"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "legacy", Name: "agent_repo_map"}}},
		{Role: "tool", ToolCallID: "legacy", Content: "旧结果"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "已从恢复点继续" || calls != 2 {
		t.Fatalf("应在清理旧工具回合后恢复，answer=%q calls=%d", result.Answer, calls)
	}
}

// TestReasoningContentRoundTrip verifies DeepSeek's reasoning_content is parsed
// from the response and carried through to the next request.
func TestReasoningContentRoundTrip(t *testing.T) {
	// 第一轮：模型返回 reasoning_content + tool_calls
	// 第二轮：验证请求里带回了 reasoning_content
	calls := 0
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"思考中…","tool_calls":[{"id":"c1","type":"function","function":{"name":"read_project_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(req.Body).Decode(&payload)
		// 检查 assistant tool_calls 消息是否带回 reasoning_content
		for _, m := range payload.Messages {
			if m["role"] == "assistant" && m["tool_calls"] != nil {
				if got, ok := m["reasoning_content"].(string); !ok || got != "思考中…" {
					t.Fatalf("assistant 消息应带回 reasoning_content，实际 %v", m["reasoning_content"])
				}
			}
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"完成"},"finish_reason":"stop"}]}`)
	})
	registry := NewRegistry()
	registry.Add(Tool{Name: "read_project_file", Description: "读", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "内容", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}
	loop := NewLoop(cfg, registry, "", 10)
	if _, err := loop.Run(context.Background(), messageFixture()); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("应有两轮请求，实际 %d", calls)
	}
}

// TestCompressThenPruneRemovesUnansweredCalls ensures that after context
// compression cuts the transcript, an assistant message with unanswered
// tool_calls is pruned before the next request.
func TestCompressThenPruneRemovesUnansweredCalls(t *testing.T) {
	// 构造超预算的对话，其中一轮 assistant tool_calls 的 tool 响应被压缩掉。
	bigOutput := strings.Repeat("y", 60000)
	messages := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "t1", Name: "read", Arguments: nil}}},
		{Role: "tool", ToolCallID: "t1", Content: bigOutput},                                 // 大响应触发压缩
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "t2", Name: "read", Arguments: nil}}}, // t2 无响应
		{Role: "user", Content: "继续"},
	}
	compressed := compressIfOverBudget(messages, 1000)
	pruned := PruneOrphanTools(compressed)
	for _, message := range pruned {
		if len(message.ToolCalls) > 0 {
			t.Fatalf("压缩后不应保留无响应的 tool_calls：%+v", message)
		}
	}
}

// TestCompressPreservesReasoningContent ensures assistant messages carrying
// DeepSeek reasoning_content are never folded away by context compression.
func TestCompressPreservesReasoningContent(t *testing.T) {
	// 大 tool 输出触发压缩，但带 reasoning_content 的 assistant 必须保留。
	messages := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "思考中", ReasoningContent: "推理过程", ToolCalls: []ToolCall{{ID: "t1", Name: "read", Arguments: nil}}},
		{Role: "tool", ToolCallID: "t1", Content: strings.Repeat("x", 80000)},
		{Role: "user", Content: "继续"},
	}
	compressed := compressIfOverBudget(messages, 2000)
	found := false
	for _, message := range compressed {
		if message.ReasoningContent == "推理过程" {
			found = true
			break
		}
	}
	if !found {
		t.Error("压缩不应丢失 reasoning_content")
	}
}

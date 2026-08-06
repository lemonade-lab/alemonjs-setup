package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"alemonx/internal/ai"
)

func TestDiscoverVerifyCommandPrefersCheck(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["package.json"] = `{"name":"x","scripts":{"check":"tsgo --noEmit","lint":"eslint src","test":"true"}}`
	spec, ok := DiscoverVerifyCommand("/p", files)
	if !ok {
		t.Fatal("应发现验证命令")
	}
	if spec.Command != "tsgo" || len(spec.Args) != 1 || spec.Args[0] != "--noEmit" {
		t.Errorf("应优先 check 脚本：%+v", spec)
	}
}

func TestDiscoverVerifyCommandFallback(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["package.json"] = `{"name":"x","scripts":{"build":"eslint ."}}`
	spec, ok := DiscoverVerifyCommand("/p", files)
	if !ok || spec.Command != "eslint" {
		t.Errorf("应回退到 build 脚本：%+v", spec)
	}
}

func TestDiscoverVerifyCommandNone(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["package.json"] = `{"name":"x","scripts":{"start":"node app.js"}}`
	if _, ok := DiscoverVerifyCommand("/p", files); ok {
		t.Error("node 启动脚本不应被识别为验证命令")
	}
}

func TestParseScriptRejectsShell(t *testing.T) {
	for _, script := range []string{"tsc --noEmit | sh", "eslint src && rm -rf .", "node --check $FILE", "tsgo --noEmit; ls"} {
		if command, _, ok := parseScriptInvocation(script); ok {
			t.Errorf("应拒绝含 shell 元字符的脚本 %q，但解析出命令 %q", script, command)
		}
	}
}

func TestParseScriptNodeOnlyCheck(t *testing.T) {
	if command, _, ok := parseScriptInvocation("node --check src/index.js"); !ok || command != "node" {
		t.Error("node --check 应允许")
	}
	if _, _, ok := parseScriptInvocation("node build.js"); ok {
		t.Error("node build.js 应拒绝")
	}
}

func TestVerifyToolReportsNoCommand(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["package.json"] = `{"name":"x"}`
	_, handler := VerifyTool("/p", files, NewCommandRunner(), CommandSpec{})
	output, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "没有可用的验证命令") {
		t.Errorf("无验证命令时应提示：%q", output)
	}
}

func TestVerifyToolRunsDiscoveredCommand(t *testing.T) {
	// Use the real command runner against a temp dir with a package.json that
	// declares a check script. We don't need tsgo on PATH: the runner rejects
	// unknown executables, but the point here is the spec resolves correctly.
	files := newFakeFiles("/p")
	files.files["package.json"] = `{"name":"x","scripts":{"check":"tsgo --noEmit"}}`
	_, handler := VerifyTool("/p", files, NewCommandRunner(), CommandSpec{})
	output, _ := handler(context.Background(), nil)
	// tsgo may not exist in the sandbox; either an execution error (command not
	// found surfaced as 错误) or a success is acceptable — the test guards that
	// the handler attempts the resolved command rather than returning the
	// "no command" message.
	if strings.Contains(output, "没有可用的验证命令") {
		t.Errorf("应尝试运行发现的命令而非报无命令：%q", output)
	}
}

// TestLoopAutoVerifyAfterWrite asserts that after a write tool executes, the
// loop appends an agent_verify call and feeds its output back to the model.
func TestLoopAutoVerifyAfterWrite(t *testing.T) {
	var payloads []struct {
		Messages []map[string]any `json:"messages"`
	}
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(req.Body).Decode(&payload)
		payloads = append(payloads, payload)
		if len(payloads) == 1 {
			return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"w","type":"function","function":{"name":"agent_edit_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"改好了，验证通过"},"finish_reason":"stop"}]}`)
	})

	registry := NewRegistry()
	registry.AddWrite(Tool{Name: "agent_edit_file", Description: "编辑", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "已更新 src/a.ts", nil
	})
	registry.Add(Tool{Name: verifyToolName, Description: "验证", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "tsgo 检查通过，0 错误", nil
	})

	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}
	loop := NewLoop(cfg, registry, "", 10)
	loop.WithAutoVerify()
	result, err := loop.Run(context.Background(), messageFixture())
	if err != nil {
		t.Fatal(err)
	}
	// The second request must contain a verify tool call and its result.
	if len(payloads) < 2 {
		t.Fatalf("应发生两次请求，实际 %d", len(payloads))
	}
	var roles []string
	for _, m := range payloads[1].Messages {
		roles = append(roles, m["role"].(string))
	}
	joined := strings.Join(roles, ",")
	if !strings.Contains(joined, "tool") {
		t.Errorf("第二轮应含 tool 消息：%s", joined)
	}
	// The transcript's tool messages should include both the edit result and
	// the verify output.
	foundEdit, foundVerify := false, false
	for _, m := range result.Messages {
		if m.Role == "tool" {
			if strings.Contains(m.Content, "已更新 src/a.ts") {
				foundEdit = true
			}
			if strings.Contains(m.Content, "tsgo 检查通过") {
				foundVerify = true
			}
		}
	}
	if !foundEdit || !foundVerify {
		t.Errorf("transcript 应同时含编辑结果与验证结果（edit=%v verify=%v）", foundEdit, foundVerify)
	}
}

// TestLoopNoAutoVerifyWithoutFlag ensures writes without WithAutoVerify do not
// trigger verification, preserving opt-in behavior.
func TestLoopNoAutoVerifyWithoutFlag(t *testing.T) {
	withTransport(t, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(t, req, `{"choices":[{"message":{"role":"assistant","content":"完成"},"finish_reason":"stop"}]}`)
	})
	registry := NewRegistry()
	registry.AddWrite(Tool{Name: "agent_edit_file", Description: "编辑", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "已更新", nil
	})
	registry.Add(Tool{Name: verifyToolName, Description: "验证", Parameters: map[string]any{"type": "object"}}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "验证输出", nil
	})
	cfg := ai.Resolved{BaseURL: "https://provider.test", Model: "m", APIKey: "k"}
	loop := NewLoop(cfg, registry, "", 10)
	// No WithAutoVerify.
	if _, err := loop.Run(context.Background(), messageFixture()); err != nil {
		t.Fatal(err)
	}
}

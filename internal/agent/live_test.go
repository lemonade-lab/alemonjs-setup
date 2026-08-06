package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alemonx/internal/ai"
)

// osFiles is a direct filesystem FileService used by the live test. It is not
// the production implementation (the web layer reuses robot.Manager's path and
// sensitivity checks); it exists only so the real DeepSeek round-trip can read
// a real project.
type osFiles struct{}

func (osFiles) ReadFile(root, path string) (string, error) {
	resolved := filepath.Join(root, path)
	if !strings.HasPrefix(resolved, filepath.Clean(root)+string(filepath.Separator)) {
		return "", os.ErrPermission
	}
	raw, err := os.ReadFile(resolved)
	return string(raw), err
}
func (osFiles) WriteFile(root, path, content string) error {
	return os.WriteFile(filepath.Join(root, path), []byte(content), 0o644)
}
func (osFiles) ListFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() && (name == ".git" || name == "node_modules") {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			relative, err := filepath.Rel(root, current)
			if err == nil {
				files = append(files, filepath.ToSlash(relative))
			}
		}
		return nil
	})
	return files, err
}

// TestLiveDeepSeekRoundTrip is an opt-in end-to-end test that drives the real
// agent loop against the DeepSeek API. It requires ALX_LIVE_DEEPSEEK_KEY to be
// set; otherwise it skips. The key is read only from the environment and never
// written to a file or echoed.
//
// It proves the production chain — provider adapter → tool registry → loop →
// real model tool call → tool result → final answer — works against a real
// provider, using the real alemonb robot project in this repository.
func TestLiveDeepSeekRoundTrip(t *testing.T) {
	key := os.Getenv("ALX_LIVE_DEEPSEEK_KEY")
	if key == "" {
		t.Skip("未设置 ALX_LIVE_DEEPSEEK_KEY，跳过真实 API 测试")
	}
	cfg := ai.Resolved{
		BaseURL:   "https://api.deepseek.com",
		Model:     "deepseek-chat",
		APIKey:    key,
		Anthropic: false,
	}
	projectRoot := filepath.Join("..", "..", "alemonb")
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	files := osFiles{}
	registry := ProjectTools(absRoot, files, NewCommandRunner())
	loop := NewLoop(cfg, registry, BuildSystemPrompt(absRoot, files, "测试"), 20)
	result, err := loop.Run(context.Background(), []Message{
		{Role: "user", Content: "用 read_project_file 工具读取 src/index.js 和 src/expose.js，然后总结这个机器人项目是做什么的（用中文，两三句）。"},
	})
	if err != nil {
		t.Fatalf("真实 DeepSeek 运行失败：%v", err)
	}
	if strings.TrimSpace(result.Answer) == "" {
		t.Fatal("真实 DeepSeek 返回了空答案")
	}
	t.Logf("真实 DeepSeek 答案：%s", result.Answer)

	// The transcript must contain tool messages: the model must have actually
	// invoked read_project_file for this test to be meaningful.
	foundRead := false
	for _, message := range result.Messages {
		if message.Role == "tool" && strings.Contains(message.Content, "alemonjs") {
			foundRead = true
			break
		}
	}
	if !foundRead {
		t.Error("模型未真正调用 read_project_file 读取项目文件")
	}

	// Print the tool-call sequence for the record.
	for _, message := range result.Messages {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			for _, call := range message.ToolCalls {
				t.Logf("工具调用：%s %s", call.Name, string(call.Arguments))
			}
		}
	}
}

// TestLiveDeepSeekToolEcho is a second opt-in check: ask the model to use
// agent_edit_file on a throwaway copy? No — this test only verifies read-only
// behavior. Destructive edits stay manual.
func TestLiveDeepSeekToolEcho(t *testing.T) {
	key := os.Getenv("ALX_LIVE_DEEPSEEK_KEY")
	if key == "" {
		t.Skip("未设置 ALX_LIVE_DEEPSEEK_KEY，跳过真实 API 测试")
	}
	cfg := ai.Resolved{BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKey: key}
	registry := NewRegistry()
	registry.Add(Tool{Name: "echo", Description: "回显一段文字", Parameters: map[string]any{
		"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"},
	}}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct{ Text string `json:"text"` }
		_ = json.Unmarshal(arguments, &in)
		return "你让我回显：" + in.Text, nil
	})
	loop := NewLoop(cfg, registry, "你是测试助手", 10)
	result, err := loop.Run(context.Background(), []Message{{Role: "user", Content: "用 echo 工具传 'hello'，然后把结果告诉我"}})
	if err != nil {
		t.Fatalf("真实工具调用失败：%v", err)
	}
	if strings.TrimSpace(result.Answer) == "" {
		t.Fatal("空答案")
	}
	t.Logf("echo 测试答案：%s", result.Answer)
}

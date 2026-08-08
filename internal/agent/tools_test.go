package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeFiles struct {
	root   string
	files  map[string]string
	loaded []string
}

func newFakeFiles(root string) *fakeFiles {
	return &fakeFiles{root: root, files: map[string]string{}}
}

func (f *fakeFiles) ReadFile(root, path string) (string, error) {
	content, ok := f.files[path]
	if !ok {
		return "", errors.New("文件不存在")
	}
	return content, nil
}

func (f *fakeFiles) WriteFile(root, path, content string) error {
	f.files[path] = content
	f.loaded = append(f.loaded, path)
	return nil
}

func (f *fakeFiles) CreateFile(root, path, content string) error {
	f.files[path] = content
	f.loaded = append(f.loaded, path)
	return nil
}

func (f *fakeFiles) DeleteFile(root, path string) error {
	delete(f.files, path)
	f.loaded = append(f.loaded, path)
	return nil
}

func (f *fakeFiles) ListFiles(root string) ([]string, error) {
	out := make([]string, 0, len(f.files))
	for path := range f.files {
		out = append(out, path)
	}
	return out, nil
}

func callTool(t *testing.T, registry *Registry, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	handler, ok := registry.handlers[name]
	if !ok {
		t.Fatalf("工具 %s 未注册", name)
	}
	return handler(context.Background(), raw)
}

func TestEditFileReplacesUniquely(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/index.js"] = "const a = 1\nconst b = 2\n"
	registry := ProjectTools("/p", files, NewCommandRunner())

	out, err := callTool(t, registry, "agent_edit_file", map[string]any{
		"path": "src/index.js",
		"edits": []map[string]any{
			{"old": "const b = 2", "new": "const b = 3"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "已更新") {
		t.Errorf("输出应为成功提示：%q", out)
	}
	if got := files.files["src/index.js"]; got != "const a = 1\nconst b = 3\n" {
		t.Errorf("文件内容错误：%q", got)
	}
}

func TestEditFileRejectsAmbiguousOld(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/index.js"] = "const x = 1\nconst x = 1\n"
	registry := ProjectTools("/p", files, NewCommandRunner())

	if _, err := callTool(t, registry, "agent_edit_file", map[string]any{
		"path": "src/index.js", "edits": []map[string]any{
			{"old": "const x = 1", "new": "const y = 1"},
		},
	}); err == nil || !strings.Contains(err.Error(), "出现 2 次") {
		t.Fatalf("应拒绝歧义匹配，实际：%v", err)
	}
	if files.files["src/index.js"] != "const x = 1\nconst x = 1\n" {
		t.Errorf("歧义时不应修改文件")
	}
}

func TestEditFileRejectsMissingOld(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/index.js"] = "const a = 1\n"
	registry := ProjectTools("/p", files, NewCommandRunner())

	if _, err := callTool(t, registry, "agent_edit_file", map[string]any{
		"path": "src/index.js", "edits": []map[string]any{
			{"old": "const zzz = 9", "new": "x"},
		},
	}); err == nil {
		t.Fatal("应拒绝不存在的 old")
	}
}

func TestSearchProject(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/a.ts"] = "const bot = createBot()\nconst other = 1\n"
	files.files["src/b.js"] = "// createBot usage here\n"
	registry := ProjectTools("/p", files, NewCommandRunner())

	out, err := callTool(t, registry, "agent_search", map[string]any{"pattern": "createBot"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "src/a.ts:1") || !strings.Contains(out, "src/b.js:1") {
		t.Errorf("搜索应命中两个文件：\n%s", out)
	}
}

func TestSearchProjectGlob(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/a.ts"] = "createBot\n"
	files.files["lib/b.js"] = "createBot\n"
	registry := ProjectTools("/p", files, NewCommandRunner())

	out, err := callTool(t, registry, "agent_search", map[string]any{"pattern": "createBot", "glob": "src/**"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "lib/b.js") {
		t.Errorf("glob 应排除 lib/b.js：\n%s", out)
	}
	if !strings.Contains(out, "src/a.ts") {
		t.Errorf("glob 应包含 src/a.ts：\n%s", out)
	}
}

func TestSearchInvalidRegex(t *testing.T) {
	registry := ProjectTools("/p", newFakeFiles("/p"), NewCommandRunner())
	if _, err := callTool(t, registry, "agent_search", map[string]any{"pattern": "["}); err == nil {
		t.Fatal("无效正则应报错")
	}
}

// TestProjectToolsSchemasAreValid asserts every registered tool's parameters
// serializes to a JSON object with a non-null properties field, which OpenAI
// rejects when null.
func TestProjectToolsSchemasAreValid(t *testing.T) {
	registry := ProjectTools("/p", newFakeFiles("/p"), NewCommandRunner())
	for _, tool := range registry.List() {
		raw, err := json.Marshal(tool.Parameters)
		if err != nil {
			t.Fatalf("工具 %s 参数序列化失败：%v", tool.Name, err)
		}
		var schema struct {
			Type       string          `json:"type"`
			Properties json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("工具 %s 参数不是合法 JSON：%v", tool.Name, err)
		}
		if schema.Type != "object" {
			t.Errorf("工具 %s 参数 type = %q，应为 object", tool.Name, schema.Type)
		}
		if string(schema.Properties) == "null" {
			t.Errorf("工具 %s 的 properties 是 null，OpenAI 会拒绝", tool.Name)
		}
		if string(schema.Properties) == "" {
			t.Errorf("工具 %s 缺少 properties 字段", tool.Name)
		}
	}
}

func TestEditFileMultiHunk(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/a.ts"] = "const a = 1\nconst b = 2\nconst c = 3\n"
	registry := ProjectTools("/p", files, NewCommandRunner())

	out, err := callTool(t, registry, "agent_edit_file", map[string]any{
		"path": "src/a.ts",
		"edits": []map[string]any{
			{"old": "const a = 1", "new": "const a = 10"},
			{"old": "const c = 3", "new": "const c = 30"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2 处") {
		t.Errorf("应报告 2 处修改：%q", out)
	}
	if got := files.files["src/a.ts"]; got != "const a = 10\nconst b = 2\nconst c = 30\n" {
		t.Errorf("多 hunk 修改错误：%q", got)
	}
}

// TestEditFileAtomicRollback: 第二个 hunk 不匹配时，第一个 hunk 不应被写入。
func TestEditFileAtomicRollback(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/a.ts"] = "const a = 1\nconst b = 2\n"
	registry := ProjectTools("/p", files, NewCommandRunner())

	_, err := callTool(t, registry, "agent_edit_file", map[string]any{
		"path": "src/a.ts",
		"edits": []map[string]any{
			{"old": "const a = 1", "new": "const a = 10"},
			{"old": "不存在的文本", "new": "x"},
		},
	})
	if err == nil {
		t.Fatal("应拒绝失败的 hunk")
	}
	if got := files.files["src/a.ts"]; got != "const a = 1\nconst b = 2\n" {
		t.Errorf("hunk 失败时不应写入任何修改：%q", got)
	}
}

func TestEditFileCreate(t *testing.T) {
	files := newFakeFiles("/p")
	registry := ProjectTools("/p", files, NewCommandRunner())

	out, err := callTool(t, registry, "agent_edit_file", map[string]any{
		"path": "src/new.ts", "mode": "create", "content": "export const x = 1\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "已创建") {
		t.Errorf("应提示创建：%q", out)
	}
	if files.files["src/new.ts"] != "export const x = 1\n" {
		t.Errorf("新文件内容错误：%q", files.files["src/new.ts"])
	}
}

func TestEditFileDelete(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/old.ts"] = "export const x = 1\n"
	registry := ProjectTools("/p", files, NewCommandRunner())

	out, err := callTool(t, registry, "agent_edit_file", map[string]any{
		"path": "src/old.ts", "mode": "delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "已删除") {
		t.Errorf("应提示删除：%q", out)
	}
	if _, ok := files.files["src/old.ts"]; ok {
		t.Error("文件应已被删除")
	}
}

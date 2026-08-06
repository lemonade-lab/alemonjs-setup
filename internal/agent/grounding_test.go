package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptIncludesAgents(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["AGENTS.md"] = "禁止使用 require\n改完跑 tsgo 检查"
	files.files["package.json"] = `{"name":"bot","scripts":{"build":"tsc --noEmit"},"dependencies":{"alemonjs":"1.0.0"}}`
	files.files["src/index.ts"] = "export const x = 1"

	prompt := BuildSystemPrompt("/p", files, "基础指令")
	for _, want := range []string{"基础指令", "AGENTS.md", "禁止使用 require", "scripts: build", "dependencies: 1 个包", "src/"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt 缺少 %q：\n%s", want, prompt)
		}
	}
}

func TestBuildSystemPromptPrefersAgentsOverClaude(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["AGENTS.md"] = "agents 内容"
	files.files["CLAUDE.md"] = "claude 内容"

	prompt := BuildSystemPrompt("/p", files, "")
	if !strings.Contains(prompt, "agents 内容") {
		t.Errorf("应优先使用 AGENTS.md")
	}
	if strings.Contains(prompt, "claude 内容") {
		t.Errorf("AGENTS.md 存在时不应再读 CLAUDE.md")
	}
}

func TestBuildSystemPromptWithoutManifest(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/index.ts"] = "x"
	prompt := BuildSystemPrompt("/p", files, "基础")
	if !strings.Contains(prompt, "src/") {
		t.Errorf("无 package.json 时仍应包含目录结构")
	}
	if strings.Contains(prompt, "manifest") {
		t.Errorf("无 package.json 时不应有 manifest 段落")
	}
}

func TestSummarizeTree(t *testing.T) {
	files := []string{"src/a.ts", "src/b.ts", "lib/c.js", "package.json", "tsconfig.json"}
	tree := summarizeTree(files)
	joined := strings.Join(tree, "\n")
	for _, want := range []string{"src/（2 个文件）", "lib/（1 个文件）", "package.json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("目录树缺少 %q：\n%s", want, joined)
		}
	}
}

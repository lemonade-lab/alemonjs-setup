package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BuildSystemPrompt composes a project-aware system prompt from the files in
// the target project: an existing AGENTS.md/CLAUDE.md, the package.json
// scripts and dependency counts, and the top-level file structure. The result
// grounds the agent in the project's conventions before it plans any change.
func BuildSystemPrompt(root string, files FileService, base string) string {
	parts := make([]string, 0, 4)
	if base != "" {
		parts = append(parts, base)
	}

	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		content, err := files.ReadFile(root, name)
		if err == nil && strings.TrimSpace(content) != "" {
			parts = append(parts, fmt.Sprintf("## 项目约定（%s）\n%s", name, content))
			break
		}
	}

	if raw, err := files.ReadFile(root, "package.json"); err == nil {
		if section := manifestSummary(raw); section != "" {
			parts = append(parts, "## 项目 manifest\n"+section)
		}
	}

	if list, err := files.ListFiles(root); err == nil {
		if tree := summarizeTree(list); len(tree) > 0 {
			parts = append(parts, fmt.Sprintf("## 项目结构（共 %d 个文件）\n%s", len(list), strings.Join(tree, "\n")))
		}
	}

	return strings.Join(parts, "\n\n")
}

// manifestSummary extracts scripts and dependency counts from package.json so
// the agent knows what verification commands exist.
func manifestSummary(raw string) string {
	var manifest struct {
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return ""
	}
	lines := make([]string, 0, 3)
	if len(manifest.Scripts) > 0 {
		names := make([]string, 0, len(manifest.Scripts))
		for name := range manifest.Scripts {
			names = append(names, name)
		}
		sort.Strings(names)
		lines = append(lines, "scripts: "+strings.Join(names, ", "))
	}
	if len(manifest.Dependencies) > 0 {
		lines = append(lines, fmt.Sprintf("dependencies: %d 个包", len(manifest.Dependencies)))
	}
	if len(manifest.DevDependencies) > 0 {
		lines = append(lines, fmt.Sprintf("devDependencies: %d 个包", len(manifest.DevDependencies)))
	}
	return strings.Join(lines, "\n")
}

// summarizeTree collapses a file list into top-level directories with file
// counts plus root-level files, so the prompt shows structure without noise.
func summarizeTree(files []string) []string {
	dirs := map[string]int{}
	roots := make([]string, 0)
	for _, path := range files {
		if index := strings.Index(path, "/"); index >= 0 {
			dirs[path[:index]]++
		} else {
			roots = append(roots, path)
		}
	}
	out := make([]string, 0, len(dirs)+len(roots)+2)
	for _, name := range roots {
		out = append(out, name)
	}
	dirNames := make([]string, 0, len(dirs))
	for name := range dirs {
		dirNames = append(dirNames, name)
	}
	sort.Strings(dirNames)
	for _, name := range dirNames {
		out = append(out, fmt.Sprintf("%s/（%d 个文件）", name, dirs[name]))
	}
	return out
}

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FileService abstracts project file access so the tool layer is testable
// without a real filesystem and the web layer can reuse the robot manager's
// path and sensitivity checks.
type FileService interface {
	ReadFile(root, path string) (string, error)
	WriteFile(root, path, content string) error
	ListFiles(root string) ([]string, error)
}

// ProjectTools registers the built-in tools for managing a robot project.
// Read-only tools plus a precise edit tool and a whitelisted command runner.
func ProjectTools(root string, files FileService, commands CommandRunner) *Registry {
	registry := NewRegistry()

	registry.Add(Tool{
		Name:        "read_project_file",
		Description: "读取机器人项目内的源码或配置文件（不超过 1 MiB，不能是密钥、依赖目录或符号链接）。",
		Parameters:  stringParamSchema("path", "相对于项目根目录的文件路径，例如 src/index.ts"),
	}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct{ Path string `json:"path"` }
		if err := json.Unmarshal(arguments, &in); err != nil {
			return "", fmt.Errorf("参数无效：%v", err)
		}
		return files.ReadFile(root, in.Path)
	})

	registry.Add(Tool{
		Name:        "list_project_files",
		Description: "列出机器人项目内可由 AI 管理的源码和配置文件，排除密钥、Git 元数据、依赖目录和符号链接。",
		Parameters:  objectSchema(nil),
	}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		list, err := files.ListFiles(root)
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(list)
		return string(data), nil
	})

	registry.Add(Tool{
		Name:        "agent_search",
		Description: "在项目源码中按正则表达式搜索内容，返回 路径:行号:匹配行。用于定位实现、调用点与报错出处。",
		Parameters: objectSchema(map[string]any{
			"pattern": stringParam("要搜索的正则表达式，例如 createBot|alemonjs.onEvent"),
			"glob":    stringParam("可选。只搜索匹配该 glob 的文件，例如 src/**/*.ts"),
		}, "pattern"),
	}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct {
			Pattern string `json:"pattern"`
			Glob    string `json:"glob"`
		}
		if err := json.Unmarshal(arguments, &in); err != nil {
			return "", fmt.Errorf("参数无效：%v", err)
		}
		expression, err := regexp.Compile(in.Pattern)
		if err != nil {
			return "", fmt.Errorf("正则表达式无效：%v", err)
		}
		return searchProject(files, root, in.Pattern, in.Glob, expression)
	})

	registry.AddWrite(Tool{
		Name:        "agent_edit_file",
		Description: "精确替换项目文件中的一段文本。old 必须是文件中唯一匹配的文本；不匹配或匹配多次时不会修改文件，请调整后重试。用于针对性修改而非整文件覆盖。",
		Parameters: objectSchema(map[string]any{
			"path": stringParam("相对于项目根目录的文件路径。"),
			"old":  stringParam("文件中要替换的现有文本，必须唯一匹配。"),
			"new":  stringParam("替换后的新文本。"),
		}, "path", "old", "new"),
	}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct {
			Path string `json:"path"`
			Old  string `json:"old"`
			New  string `json:"new"`
		}
		if err := json.Unmarshal(arguments, &in); err != nil {
			return "", fmt.Errorf("参数无效：%v", err)
		}
		content, err := files.ReadFile(root, in.Path)
		if err != nil {
			return "", err
		}
		occurrences := strings.Count(content, in.Old)
		switch {
		case in.Old == "":
			return "", fmt.Errorf("old 不能为空")
		case occurrences == 0:
			return "", fmt.Errorf("文件中没有匹配 old 的文本；请提供准确上下文")
		case occurrences > 1:
			return "", fmt.Errorf("old 在文件中出现 %d 次；请包含更多周围代码使匹配唯一", occurrences)
		}
		updated := strings.Replace(content, in.Old, in.New, 1)
		if err := files.WriteFile(root, in.Path, updated); err != nil {
			return "", err
		}
		return fmt.Sprintf("已更新 %s", in.Path), nil
	})

	registry.Add(Tool{
		Name:        "agent_run_command",
		Description: "在项目根目录运行白名单命令：验证工具（tsgo、tsc、eslint、node --check）或包管理器的标准生命周期子命令与 package.json 中声明的脚本（install/build/test/dev）。不支持任意 shell 命令。",
		Parameters: objectSchema(map[string]any{
			"command": stringParam("可执行文件名：yarn/npm/pnpm/node/tsgo/tsc/eslint。"),
			"args":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "传给命令的参数。"},
		}, "command"),
	}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(arguments, &in); err != nil {
			return "", fmt.Errorf("参数无效：%v", err)
		}
		return commands.Run(ctx, root, in.Command, in.Args)
	})

	verifyTool, verifyHandler := VerifyTool(root, files, commands, CommandSpec{})
	registry.Add(verifyTool, verifyHandler)

	return registry
}

// searchProject scans eligible project files for a regex and returns up to 50
// matches as 路径:行号:内容.
func searchProject(files FileService, root, pattern, glob string, expression *regexp.Regexp) (string, error) {
	list, err := files.ListFiles(root)
	if err != nil {
		return "", err
	}
	var matched func(string) bool
	if glob != "" {
		matched = func(path string) bool {
			ok, _ := filepath.Match(glob, path)
			return ok
		}
	}
	hits := make([]string, 0, 50)
	for _, path := range list {
		if matched != nil && !matched(path) {
			continue
		}
		content, err := files.ReadFile(root, path)
		if err != nil {
			continue
		}
		lines := strings.Split(content, "\n")
		for index, line := range lines {
			if expression.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d:%s", path, index+1, strings.TrimSpace(line)))
				if len(hits) >= 50 {
					return strings.Join(hits, "\n") + "\n…（结果超过 50 条，已截断）", nil
				}
			}
		}
	}
	if len(hits) == 0 {
		return "没有匹配结果。", nil
	}
	sort.Strings(hits)
	return strings.Join(hits, "\n"), nil
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringParamSchema(name, description string) map[string]any {
	return objectSchema(map[string]any{name: stringParam(description)}, name)
}

func stringParam(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

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
	CreateFile(root, path, content string) error
	DeleteFile(root, path string) error
	ListFiles(root string) ([]string, error)
}

// ProjectTools registers the built-in tools for managing a robot project.
// Read-only tools plus a precise edit tool and a whitelisted command runner.
func ProjectTools(root string, files FileService, commands CommandRunner) *Registry {
	registry := NewRegistry()
	var cachedIndex RepoIndex
	indexReady := false
	getIndex := func() (RepoIndex, error) {
		if indexReady {
			return cachedIndex, nil
		}
		index, err := BuildRepoIndex(root, files)
		if err != nil {
			return RepoIndex{}, err
		}
		cachedIndex, indexReady = index, true
		return index, nil
	}

	registry.Add(Tool{
		Name:        "read_project_file",
		Description: "读取机器人项目内的源码或配置文件（不超过 1 MiB，不能是密钥、依赖目录或符号链接）。",
		Parameters:  stringParamSchema("path", "相对于项目根目录的文件路径，例如 src/index.ts"),
	}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct {
			Path string `json:"path"`
		}
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
			// 大项目超过列表上限时，不返回硬错误，而是引导模型改用搜索。
			return "项目文件过多，无法一次性列出；请用 agent_search 按关键词定位需要的文件。", nil
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

	registry.Add(Tool{Name: "agent_repo_map", Description: "建立并返回项目代码地图，包括文件、符号、引用和路由/事件注册点。优先用于理解大型项目。", Parameters: objectSchema(nil)}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		index, err := getIndex()
		if err != nil {
			return "", err
		}
		return index.Summary(), nil
	})
	registry.Add(Tool{Name: "agent_find_symbol", Description: "按名称和可选类型查找项目中的函数、类、接口、类型和变量定义。", Parameters: objectSchema(map[string]any{"query": stringParam("符号名称或片段"), "kind": stringParam("可选类型，例如 function/class/interface/type")}, "query")}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct{ Query, Kind string }
		if err := json.Unmarshal(arguments, &in); err != nil {
			return "", err
		}
		index, err := getIndex()
		if err != nil {
			return "", err
		}
		out, _ := json.Marshal(FindSymbols(index, in.Query, in.Kind))
		return string(out), nil
	})
	registry.Add(Tool{Name: "agent_find_references", Description: "查找符号在项目中的引用位置，用于追踪调用关系。", Parameters: stringParamSchema("symbol", "符号名称")}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct {
			Symbol string `json:"symbol"`
		}
		if err := json.Unmarshal(arguments, &in); err != nil {
			return "", err
		}
		index, err := getIndex()
		if err != nil {
			return "", err
		}
		out, _ := json.Marshal(FindReferences(index, in.Symbol))
		return string(out), nil
	})
	registry.Add(Tool{Name: "agent_find_route", Description: "定位 AlemonJS Router、HTTP 路由和事件注册点。", Parameters: objectSchema(nil)}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		index, err := getIndex()
		if err != nil {
			return "", err
		}
		out, _ := json.Marshal(limitReferences(index.Routes))
		return string(out), nil
	})
	registry.Add(Tool{Name: "agent_find_event_handler", Description: "定位 useEvent、onEvent、onMessage 等事件处理器。", Parameters: objectSchema(nil)}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		index, err := getIndex()
		if err != nil {
			return "", err
		}
		var out []RepoReference
		for _, route := range index.Routes {
			if strings.Contains(route.Symbol, "Event") || strings.Contains(route.Symbol, "Message") {
				out = append(out, route)
			}
		}
		raw, _ := json.Marshal(limitReferences(out))
		return string(raw), nil
	})
	registry.Add(Tool{Name: "agent_find_config", Description: "定位 alemon.config、配置读取和 package.json 配置相关代码。", Parameters: objectSchema(nil)}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		return searchProject(files, root, `alemon\.config|config|package\.json`, "", regexp.MustCompile(`alemon\.config|config|package\.json`))
	})

	registry.AddWrite(Tool{
		Name: "agent_edit_file",
		Description: "结构化修改项目文件，三种模式：edit（多 hunk 精确替换）、create（新建文件）、delete（删除文件）。" +
			"edit 模式：edits 数组每项 {old,new}，old 必须是文件中唯一匹配的文本；全部 hunk 应用后才写入，任一失败则不修改。",
		Parameters: objectSchema(map[string]any{
			"path":    stringParam("相对于项目根目录的文件路径。"),
			"mode":    stringParam("edit（默认，替换）/ create（新建）/ delete（删除）。"),
			"content": stringParam("create 模式的新文件内容。"),
			"edits": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "properties": map[string]any{
					"old": stringParam("要替换的现有文本，必须唯一匹配。"),
					"new": stringParam("替换后的新文本。"),
				}, "required": []string{"old", "new"},
			}, "description": "edit 模式的替换列表，按顺序应用。"},
		}, "path"),
	}, func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var in struct {
			Path    string `json:"path"`
			Mode    string `json:"mode"`
			Content string `json:"content"`
			Edits   []struct {
				Old string `json:"old"`
				New string `json:"new"`
			} `json:"edits"`
		}
		if err := json.Unmarshal(arguments, &in); err != nil {
			return "", fmt.Errorf("参数无效：%v", err)
		}
		switch in.Mode {
		case "create":
			if in.Content == "" {
				return "", fmt.Errorf("create 模式需要 content")
			}
			if err := files.CreateFile(root, in.Path, in.Content); err != nil {
				return "", err
			}
			indexReady = false
			return fmt.Sprintf("已创建 %s", in.Path), nil
		case "delete":
			if err := files.DeleteFile(root, in.Path); err != nil {
				return "", err
			}
			indexReady = false
			return fmt.Sprintf("已删除 %s", in.Path), nil
		}
		content, err := files.ReadFile(root, in.Path)
		if err != nil {
			return "", err
		}
		// 先验证所有 hunk 再写入，任一失败则整体回滚。
		work := content
		for index, edit := range in.Edits {
			if edit.Old == "" {
				return "", fmt.Errorf("第 %d 个 hunk 的 old 不能为空", index+1)
			}
			occurrences := strings.Count(work, edit.Old)
			switch {
			case occurrences == 0:
				return "", fmt.Errorf("第 %d 个 hunk 在文件中没有匹配；请提供准确上下文", index+1)
			case occurrences > 1:
				return "", fmt.Errorf("第 %d 个 hunk 在文件中出现 %d 次；请包含更多周围代码使匹配唯一", index+1, occurrences)
			}
			work = strings.Replace(work, edit.Old, edit.New, 1)
		}
		if len(in.Edits) == 0 {
			return "", fmt.Errorf("edit 模式需要至少一个 hunk")
		}
		if err := files.WriteFile(root, in.Path, work); err != nil {
			return "", err
		}
		indexReady = false
		return fmt.Sprintf("已更新 %s（%d 处）", in.Path, len(in.Edits)), nil
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

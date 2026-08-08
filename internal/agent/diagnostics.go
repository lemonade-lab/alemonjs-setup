package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

type DiagnosticContext struct {
	Root         string   `json:"root"`
	Source       string   `json:"source"`
	ErrorText    string   `json:"errorText"`
	ErrorKind    string   `json:"errorKind"`
	File         string   `json:"file,omitempty"`
	Line         int      `json:"line,omitempty"`
	Column       int      `json:"column,omitempty"`
	RelatedFiles []string `json:"relatedFiles,omitempty"`
}

var diagnosticLocation = regexp.MustCompile(`(?m)([^\s()]+\.(?:ts|tsx|js|jsx|mjs|cjs|go)):(\d+)(?::(\d+))?`)

func ParseDiagnostic(root, source string) DiagnosticContext {
	d := DiagnosticContext{Root: root, Source: source, ErrorText: strings.TrimSpace(source), ErrorKind: classifyDiagnostic(source)}
	match := diagnosticLocation.FindStringSubmatch(source)
	if len(match) > 0 {
		d.File = match[1]
		d.Line, _ = strconv.Atoi(match[2])
		if len(match) > 3 && match[3] != "" {
			d.Column, _ = strconv.Atoi(match[3])
		}
	}
	return d
}

func classifyDiagnostic(source string) string {
	lower := strings.ToLower(source)
	switch {
	case strings.Contains(lower, "typescript"), strings.Contains(lower, "error ts"), strings.Contains(lower, "tsc"):
		return "typescript"
	case strings.Contains(lower, "eslint"):
		return "eslint"
	case strings.Contains(lower, "cannot find module"), strings.Contains(lower, "module not found"):
		return "dependency"
	case strings.Contains(lower, "syntaxerror"):
		return "syntax"
	case strings.Contains(lower, "error"):
		return "runtime"
	default:
		return "unknown"
	}
}

func RegisterDiagnosticTools(registry *Registry, root string, files FileService, commands CommandRunner) {
	registry.Add(Tool{Name: "agent_collect_diagnostics", Description: "运行项目安全验证命令并解析 TypeScript、ESLint、依赖和运行时错误。", Parameters: objectSchema(nil)}, func(ctx context.Context, args json.RawMessage) (string, error) {
		spec, ok := DiscoverVerifyCommand(root, files)
		if !ok {
			return "项目没有可用的验证命令。", nil
		}
		output, err := commands.Run(ctx, root, spec.Command, spec.Args)
		d := ParseDiagnostic(root, output)
		raw, _ := json.Marshal(d)
		if err != nil {
			return string(raw), nil
		}
		return string(raw), nil
	})
	registry.Add(Tool{Name: "agent_explain_diagnostic", Description: "解释诊断错误的类型、影响和推荐修复方向。", Parameters: stringParamSchema("error", "错误文本")}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", err
		}
		d := ParseDiagnostic(root, in.Error)
		return diagnosticExplanation(d), nil
	})
	registry.Add(Tool{Name: "agent_locate_diagnostic", Description: "根据错误文件、符号和关键词定位相关源码文件。", Parameters: stringParamSchema("query", "文件名、符号或错误关键词")}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", err
		}
		expression := regexp.QuoteMeta(in.Query)
		return searchProject(files, root, expression, "", regexp.MustCompile(expression))
	})
}

func diagnosticExplanation(d DiagnosticContext) string {
	var advice string
	switch d.ErrorKind {
	case "typescript":
		advice = "检查类型、导入路径和函数参数；修复后运行类型检查。"
	case "eslint":
		advice = "检查代码风格、未使用变量和规则要求；修复后重新运行 lint。"
	case "dependency":
		advice = "检查 package.json、包管理器锁文件和依赖版本。"
	case "syntax":
		advice = "检查错误位置附近的括号、逗号、引号和语法结构。"
	default:
		advice = "先确认错误来源和最小复现步骤，再定位相关实现。"
	}
	return "错误类型：" + d.ErrorKind + "\n" + advice
}

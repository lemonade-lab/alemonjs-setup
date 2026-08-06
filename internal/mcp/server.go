// Package mcp exposes the guarded AlemonJS project operations through the
// Model Context Protocol (MCP) stdio transport.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"alemonjs-setup/internal/catalog"
	"alemonjs-setup/internal/project"
	"alemonjs-setup/internal/releases"
	"alemonjs-setup/internal/robot"
	"alemonjs-setup/internal/setupplugin"
	"alemonjs-setup/internal/system"
)

const protocolVersion = "2025-06-18"

type Server struct {
	version   string
	templates fs.FS
	robots    robot.Manager
	policy    Policy
	mu        sync.RWMutex
	tasks     map[string]operationTask
	processes map[string]*exec.Cmd
	stopping  map[string]bool
	taskID    atomic.Uint64
}

// Policy limits which local filesystem roots this MCP process may manage.
// An empty list preserves the legacy local-client behaviour.
type Policy struct {
	AllowedRoots []string
}

type operationTask struct {
	ID         string     `json:"id"`
	Root       string     `json:"root"`
	Action     string     `json:"action"`
	Status     string     `json:"status"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// NewServer creates a local-only MCP server. It deliberately has no HTTP
// transport: stdio means an AI client must be explicitly configured on the
// same computer before it can access local robot projects.
func NewServer(version string, templates fs.FS) *Server {
	return NewServerWithPolicy(version, templates, Policy{})
}

func NewServerWithPolicy(version string, templates fs.FS, policy Policy) *Server {
	return &Server{version: version, templates: templates, policy: policy, tasks: map[string]operationTask{}, processes: map[string]*exec.Cmd{}, stopping: map[string]bool{}}
}

// HTTPHandler exposes the Streamable HTTP MCP transport at /mcp. The caller
// must bind it to loopback or place it behind a real authorization gateway.
// A non-empty bearer token is mandatory so another local process cannot call
// project-management tools without the user's explicit configuration.
func (s *Server) HTTPHandler(token string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("MCP-Protocol-Version", protocolVersion)
		if request.URL.Path != "/mcp" {
			writeHTTPError(writer, http.StatusNotFound, errorResponse(nil, -32600, "MCP 端点是 /mcp"))
			return
		}
		if !validLocalOrigin(request.Header.Get("Origin")) {
			writeHTTPError(writer, http.StatusForbidden, errorResponse(nil, -32002, "MCP HTTP Origin 不被允许"))
			return
		}
		if token == "" || request.Header.Get("Authorization") != "Bearer "+token {
			writeHTTPError(writer, http.StatusUnauthorized, errorResponse(nil, -32001, "MCP HTTP 认证失败"))
			return
		}
		switch request.Method {
		case http.MethodGet:
			// Server-to-client notifications are not required for the AlemonJS
			// control plane. Streamable HTTP explicitly permits a server to
			// decline an independent SSE stream with 405.
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
			writeHTTPError(writer, http.StatusMethodNotAllowed, errorResponse(nil, -32600, "此 MCP 端点不提供独立 SSE 流"))
			return
		case http.MethodPost:
		default:
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
			writeHTTPError(writer, http.StatusMethodNotAllowed, errorResponse(nil, -32600, "MCP HTTP 仅支持 POST 或 GET"))
			return
		}
		if version := request.Header.Get("MCP-Protocol-Version"); version != "" && version != protocolVersion {
			writeHTTPError(writer, http.StatusBadRequest, errorResponse(nil, -32600, "不支持的 MCP 协议版本"))
			return
		}
		var message rpcRequest
		if err := json.NewDecoder(io.LimitReader(request.Body, 1024*1024)).Decode(&message); err != nil {
			writeHTTPError(writer, http.StatusBadRequest, errorResponse(nil, -32700, "JSON 请求无法识别"))
			return
		}
		if message.ID == nil {
			writer.WriteHeader(http.StatusAccepted)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(s.handle(message))
	})
}

// validLocalOrigin prevents browser pages on arbitrary sites from using a
// loopback MCP endpoint as a DNS-rebinding target. Native MCP clients normally
// omit Origin; when it is present it must itself be a local HTTP(S) origin.
func validLocalOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func writeHTTPError(writer http.ResponseWriter, status int, response rpcResponse) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(response)
}

// Serve implements the newline-delimited JSON-RPC transport required by MCP
// stdio clients. Diagnostic output must never be written to stdout.
func (s *Server) Serve(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var request rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			if err := encoder.Encode(errorResponse(nil, -32700, "JSON 请求无法识别")); err != nil {
				return err
			}
			continue
		}
		if request.ID == nil { // MCP notifications do not receive a response.
			continue
		}
		response := s.handle(request)
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handle(request rpcRequest) rpcResponse {
	if request.JSONRPC != "2.0" {
		return errorResponse(request.ID, -32600, "仅支持 JSON-RPC 2.0")
	}
	switch request.Method {
	case "initialize":
		return resultResponse(request.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}, "resources": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "alemonjs-setup", "version": s.version},
			"instructions":    "此服务只能管理本机 AlemonJS 项目。项目源码可在受限范围内读写；所有会修改项目或执行命令的工具都必须传 confirm=true。密钥文件、依赖目录、Git 元数据和任意宿主机命令始终不可访问。",
		})
	case "ping":
		return resultResponse(request.ID, map[string]any{})
	case "tools/list":
		return resultResponse(request.ID, map[string]any{"tools": tools()})
	case "tools/call":
		return s.callTool(request.ID, request.Params)
	case "resources/list":
		return resultResponse(request.ID, map[string]any{"resources": []map[string]any{{"uri": "alemonjs://mcp/capabilities", "name": "AlemonJS MCP capabilities", "description": "MCP control boundary and available management capabilities.", "mimeType": "application/json"}}})
	case "resources/read":
		return s.readResource(request.ID, request.Params)
	default:
		return errorResponse(request.ID, -32601, "不支持的 MCP 方法")
	}
}

func tools() []map[string]any {
	return []map[string]any{
		tool("alemonjs_project_status", "项目状态", "读取本机 AlemonJS/Node.js 机器人项目的依赖和包管理器状态，不修改文件。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径，目录必须含 package.json。")}, "root"), true, false),
		tool("alemonjs_check_environment", "检查环境", "检查 Setup 中指定目标所需的本机运行环境，不修改系统。", objectSchema(map[string]any{"goalId": map[string]any{"type": "string", "enum": []string{"install", "develop", "desktop", "mobile", "web", "build"}, "description": "Setup 目标 ID。"}, "variant": stringSchema("web 可为 clean/docker；build 可为 npm/git。")}, "goalId"), true, false),
		toolExternal("alemonjs_list_releases", "列出版本", "从官方 GitHub 仓库读取支持应用的发布版本。", objectSchema(map[string]any{"app": map[string]any{"type": "string", "enum": []string{"alemondesk", "alemonapp", "albs", "alemonjs-setup"}, "description": "应用 ID。"}}, "app")),
		toolExternal("alemonjs_check_setup_update", "检查 Setup 更新", "检查当前 AlemonJS Setup 是否有官方更新。", objectSchema(map[string]any{})),
		toolExternal("alemonjs_list_catalog", "读取生态目录", "读取官方 AlemonJS 应用或环境连接目录。", objectSchema(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"apps", "environment"}, "description": "目录类型。"}}, "kind")),
		toolExternal("alemonjs_get_catalog_document", "读取生态文档", "读取官方生态目录中的 GitHub/Gitee 文档；不接受任意网络地址。", objectSchema(map[string]any{"source": stringSchema("官方目录条目的 URL。")}, "source")),
		toolExternal("alemonjs_get_catalog_package_config", "读取生态配置", "读取官方生态目录中包声明的 AlemonJS 配置字段。", objectSchema(map[string]any{"source": stringSchema("官方目录条目的 URL。")}, "source")),
		tool("alemonjs_list_project_files", "列出项目文件", "列出机器人项目内可由 AI 管理的源码和配置文件。会排除密钥、Git 元数据、依赖目录和符号链接。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。")}, "root"), true, false),
		tool("alemonjs_read_project_file", "读取项目文件", "读取机器人项目内的源码或配置文件。不能读取 .env、.npmrc、密钥、Git 元数据、依赖目录或符号链接。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "path": stringSchema("相对于机器人项目根目录的文件路径，例如 src/index.ts。")}, "root", "path"), true, false),
		tool("alemonjs_write_project_file", "写入项目文件", "创建或更新机器人项目中的源码或配置文件。必须在用户明确确认后调用；不能写入密钥、Git 元数据、依赖目录或符号链接。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "path": stringSchema("相对于机器人项目根目录的文件路径；父目录必须已存在。"), "content": stringSchema("完整的新文本内容。"), "confirm": map[string]any{"type": "boolean", "description": "用户已经明确确认本次文件写入时为 true。"}}, "root", "path", "content", "confirm"), false, true),
		tool("alemonjs_list_local_packages", "列出本地包", "列出机器人项目 packages 目录中已发现的本地 AlemonJS 包。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。")}, "root"), true, false),
		tool("alemonjs_get_package_runtime_config", "读取运行配置", "读取已安装 AlemonJS 包声明的运行配置字段与当前值。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "package": stringSchema("已安装的包名。")}, "root", "package"), true, false),
		tool("alemonjs_save_package_runtime_config", "保存运行配置", "按包声明的字段校验后保存 AlemonJS 运行配置。必须在用户明确确认后调用。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "package": stringSchema("已安装的包名。"), "values": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "字段名到字符串值的映射。"}, "confirm": boolSchema("用户已经确认保存运行配置时为 true。")}, "root", "package", "values", "confirm"), false, true),
		tool("alemonjs_get_package_manifest", "读取发布信息", "读取 package.json 中由 Setup 管理的发布信息。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。")}, "root"), true, false),
		tool("alemonjs_save_package_manifest", "保存发布信息", "校验并保存 package.json 的名称、版本、仓库和发布访问级别。必须在用户明确确认后调用。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "manifest": manifestSchema(), "confirm": boolSchema("用户已经确认保存发布信息时为 true。")}, "root", "manifest", "confirm"), false, true),
		tool("alemonjs_get_npm_publish_status", "检查 NPM 发布", "读取 NPM 发布前检查结果，不会发布。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。")}, "root"), true, false),
		tool("alemonjs_get_npm_pack_preview", "预览 NPM 打包", "读取 npm pack 将包含的文件列表，不会发布。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。")}, "root"), true, false),
		tool("alemonjs_get_git_release_status", "检查 GIT 发布", "读取 GIT 发布与发布前检查结果，不会创建标签或推送。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。")}, "root"), true, false),
		tool("alemonjs_initialize_git", "初始化 Git", "按指定作者、仓库和首个提交初始化项目 Git 仓库。必须在用户明确确认后调用。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "authorName": stringSchema("提交作者姓名。"), "authorEmail": stringSchema("提交作者邮箱。"), "repository": stringSchema("可选 origin 地址。"), "message": stringSchema("可选首个提交说明。"), "confirm": boolSchema("用户已经确认初始化 Git 时为 true。")}, "root", "authorName", "authorEmail", "confirm"), false, true),
		tool("alemonjs_start_project_action", "启动项目操作", "异步执行受限项目操作，并返回可供轮询的任务 ID。包含可停止的 dev 开发模式、PM2 生命周期、构建和发布操作；必须在用户明确同意后传 confirm=true，不支持任意 shell 命令。", actionSchema(), false, true),
		tool("alemonjs_stop_development", "停止开发模式", "停止由 alemonjs_start_project_action 的 dev 操作启动的开发机器人。必须在用户明确确认后调用。", objectSchema(map[string]any{"taskId": stringSchema("dev 操作返回的任务 ID。"), "confirm": boolSchema("用户已经确认停止开发机器人时为 true。")}, "taskId", "confirm"), false, true),
		tool("alemonjs_get_project_task", "查询项目操作", "查询一个 MCP 项目操作任务的实时状态和输出。", objectSchema(map[string]any{"taskId": stringSchema("alemonjs_start_project_action 返回的任务 ID。")}, "taskId"), true, false),
		tool("alemonjs_list_project_tasks", "列出项目操作", "列出当前 MCP 会话创建的项目操作任务。", objectSchema(map[string]any{"root": stringSchema("可选。仅返回该机器人项目的操作任务。")}), true, false),
		tool("alemonjs_list_setup_plugins", "列出 Setup 插件", "列出当前电脑可用的声明式 Setup 插件及其操作，不执行插件代码。", objectSchema(map[string]any{}), true, false),
		tool("alemonjs_run_setup_plugin", "运行 Setup 插件", "运行一个已声明的 Setup 插件操作。此操作始终需要用户明确确认；插件自身也可声明额外确认。", objectSchema(map[string]any{"pluginId": stringSchema("Setup 插件 ID。"), "actionId": stringSchema("插件声明的操作 ID。"), "values": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "按插件动作字段声明提供的值。"}, "confirm": boolSchema("用户已经确认运行该系统插件操作时为 true。")}, "pluginId", "actionId", "confirm"), false, true),
		tool("alemonjs_create_project", "创建项目", "使用内置模板创建 AlemonJS 项目，并安装依赖。会写入磁盘、联网下载依赖，必须在用户明确确认后调用。", objectSchema(map[string]any{"config": map[string]any{"type": "object", "description": "与 AlemonJS Setup 创建向导相同的项目配置。"}, "confirm": boolSchema("用户已经确认创建项目时为 true。")}, "config", "confirm"), false, true),
	}
}

func tool(name, title, description string, schema map[string]any, readOnly, destructive bool) map[string]any {
	return map[string]any{"name": name, "title": title, "description": description, "inputSchema": schema, "annotations": map[string]any{"readOnlyHint": readOnly, "destructiveHint": destructive, "openWorldHint": false}}
}

func toolExternal(name, title, description string, schema map[string]any) map[string]any {
	result := tool(name, title, description, schema, true, false)
	result["annotations"] = map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": true}
	return result
}

func actionSchema() map[string]any {
	return objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "action": map[string]any{"type": "string", "enum": []string{"install", "build", "dev", "pm2", "pm2-stop", "pm2-status", "install-package", "uninstall-package", "commit", "npm-version", "git-release", "npm-publish"}, "description": "要执行的受限操作。"}, "package": stringSchema("仅 install-package/uninstall-package 时需要，且必须是受支持的 AlemonJS 包。"), "message": stringSchema("仅 commit 时需要，作为 Git 提交说明。"), "version": stringSchema("npm-version 或 git-release 时需要，格式为 1.2.3。"), "tag": stringSchema("npm-publish 时使用的 npm 标签，默认 latest。"), "confirm": boolSchema("用户已经明确确认本次本机修改或命令执行时为 true。")}, "root", "action", "confirm")
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	// JSON Schema requires `required`, when present, to be an array. Omitting it
	// is the portable representation for zero required properties; encoding a
	// nil Go slice would otherwise emit `required: null`, which strict MCP
	// clients can reject while validating a tool schema.
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func boolSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func manifestSchema() map[string]any {
	return objectSchema(map[string]any{"name": stringSchema("npm 包名。"), "version": stringSchema("语义化版本，例如 1.2.3。"), "description": stringSchema("单行包说明。"), "homepage": stringSchema("可选主页 URL。"), "repository": stringSchema("可选仓库 URL。"), "license": stringSchema("可选许可证标识。"), "private": boolSchema("是否私有包。"), "access": map[string]any{"type": "string", "enum": []string{"", "public", "restricted"}, "description": "发布访问级别。"}}, "name", "version", "description", "private")
}

func (s *Server) callTool(id json.RawMessage, params json.RawMessage) rpcResponse {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil || call.Name == "" {
		return errorResponse(id, -32602, "tools/call 参数无效")
	}
	text, err := s.execute(call.Name, call.Arguments)
	return resultResponse(id, toolResult(text, err))
}

func (s *Server) execute(name string, arguments json.RawMessage) (string, error) {
	switch name {
	case "alemonjs_project_status":
		var input struct {
			Root string `json:"root"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		result, err := s.robots.Run(input.Root, "dependency-status", "", "", "", "", "", false)
		return result.Output, err
	case "alemonjs_check_environment":
		var input struct {
			GoalID  string `json:"goalId"`
			Variant string `json:"variant"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if !validEnvironmentCheck(input.GoalID, input.Variant) {
			return "", fmt.Errorf("环境检查目标或方式无效")
		}
		return encodeResult(system.NewChecker().CheckGoal(input.GoalID, input.Variant))
	case "alemonjs_list_releases":
		var input struct {
			App string `json:"app"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		items, err := releases.List(input.App)
		if err != nil {
			return "", err
		}
		return encodeResult(items)
	case "alemonjs_check_setup_update":
		update, err := releases.SetupUpdate(s.version)
		if err != nil {
			return "", err
		}
		return encodeResult(update)
	case "alemonjs_list_catalog":
		var input struct {
			Kind string `json:"kind"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		items, err := catalog.Fetch(input.Kind)
		if err != nil {
			return "", err
		}
		return encodeResult(items)
	case "alemonjs_get_catalog_document":
		var input struct {
			Source string `json:"source"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		document, err := catalog.LoadDocument(input.Source)
		if err != nil {
			return "", err
		}
		return encodeResult(document)
	case "alemonjs_get_catalog_package_config":
		var input struct {
			Source string `json:"source"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		config, err := catalog.LoadPackageConfig(input.Source)
		if err != nil {
			return "", err
		}
		return encodeResult(config)
	case "alemonjs_read_project_file":
		var input struct {
			Root string `json:"root"`
			Path string `json:"path"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		result, err := s.robots.ReadProjectFile(input.Root, input.Path)
		return result.Output, err
	case "alemonjs_list_project_files":
		var input struct {
			Root string `json:"root"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		files, err := s.robots.ListProjectFiles(input.Root)
		if err != nil {
			return "", err
		}
		return encodeResult(files)
	case "alemonjs_write_project_file":
		var input struct {
			Root    string `json:"root"`
			Path    string `json:"path"`
			Content string `json:"content"`
			Confirm bool   `json:"confirm"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		if !input.Confirm {
			return "", fmt.Errorf("此操作会写入本机项目；请在用户明确确认后传 confirm=true")
		}
		result, err := s.robots.WriteProjectFile(input.Root, input.Path, input.Content)
		return result.Output, err
	case "alemonjs_list_local_packages":
		var input struct {
			Root string `json:"root"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		packages, err := s.robots.LocalPackages(input.Root)
		if err != nil {
			return "", err
		}
		return encodeResult(packages)
	case "alemonjs_get_package_runtime_config":
		var input struct {
			Root    string `json:"root"`
			Package string `json:"package"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		config, err := s.robots.PackageConfig(input.Root, input.Package)
		if err != nil {
			return "", err
		}
		return encodeResult(config)
	case "alemonjs_save_package_runtime_config":
		var input struct {
			Root    string            `json:"root"`
			Package string            `json:"package"`
			Values  map[string]string `json:"values"`
			Confirm bool              `json:"confirm"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		if !input.Confirm {
			return "", fmt.Errorf("此操作会修改机器人运行配置；请在用户明确确认后传 confirm=true")
		}
		result, err := s.robots.SavePackageConfig(input.Root, input.Package, input.Values)
		return result.Output, err
	case "alemonjs_get_package_manifest":
		var input struct {
			Root string `json:"root"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		manifest, err := s.robots.PackageManifest(input.Root)
		if err != nil {
			return "", err
		}
		return encodeResult(manifest)
	case "alemonjs_save_package_manifest":
		var input struct {
			Root     string                `json:"root"`
			Manifest robot.PackageManifest `json:"manifest"`
			Confirm  bool                  `json:"confirm"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		if !input.Confirm {
			return "", fmt.Errorf("此操作会修改 package.json；请在用户明确确认后传 confirm=true")
		}
		result, err := s.robots.SavePackageManifest(input.Root, input.Manifest)
		return result.Output, err
	case "alemonjs_get_npm_publish_status":
		var input struct {
			Root string `json:"root"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		status, err := s.robots.NPMStatus(input.Root)
		if err != nil {
			return "", err
		}
		return encodeResult(status)
	case "alemonjs_get_npm_pack_preview":
		var input struct {
			Root string `json:"root"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		preview, err := s.robots.NPMPackPreview(input.Root)
		if err != nil {
			return "", err
		}
		return encodeResult(preview)
	case "alemonjs_get_git_release_status":
		var input struct {
			Root string `json:"root"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		status, err := robot.GitReleaseStatus(input.Root)
		if err != nil {
			return "", err
		}
		return encodeResult(status)
	case "alemonjs_initialize_git":
		var input struct {
			Root        string `json:"root"`
			AuthorName  string `json:"authorName"`
			AuthorEmail string `json:"authorEmail"`
			Repository  string `json:"repository"`
			Message     string `json:"message"`
			Confirm     bool   `json:"confirm"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		if !input.Confirm {
			return "", fmt.Errorf("此操作会初始化 Git 仓库；请在用户明确确认后传 confirm=true")
		}
		result, err := robot.InitializeGit(input.Root, robot.GitInitConfig{AuthorName: input.AuthorName, AuthorEmail: input.AuthorEmail, Repository: input.Repository, Message: input.Message})
		return result.Output, err
	case "alemonjs_start_project_action":
		var input struct {
			Root    string `json:"root"`
			Action  string `json:"action"`
			Package string `json:"package"`
			Message string `json:"message"`
			Version string `json:"version"`
			Tag     string `json:"tag"`
			Confirm bool   `json:"confirm"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if err := s.authorizeRoot(input.Root); err != nil {
			return "", err
		}
		if !input.Confirm {
			return "", fmt.Errorf("此操作会修改本机项目或执行项目命令；请在用户明确确认后传 confirm=true")
		}
		if !allowedAction(input.Action) {
			return "", fmt.Errorf("MCP 不允许该操作")
		}
		if input.Action == "dev" {
			return encodeResult(s.startDevelopment(input.Root))
		}
		return encodeResult(s.startTask(input.Root, input.Action, func() (string, error) {
			tag := input.Tag
			if tag == "" {
				tag = "latest"
			}
			result, err := s.robots.Run(input.Root, input.Action, input.Message, input.Package, input.Version, tag, "", true)
			return result.Output, err
		}))
	case "alemonjs_stop_development":
		var input struct {
			TaskID  string `json:"taskId"`
			Confirm bool   `json:"confirm"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if !input.Confirm {
			return "", fmt.Errorf("此操作会停止开发机器人；请在用户明确确认后传 confirm=true")
		}
		result, err := s.stopDevelopment(input.TaskID)
		return result, err
	case "alemonjs_get_project_task":
		var input struct {
			TaskID string `json:"taskId"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		task, ok := s.getTask(input.TaskID)
		if !ok {
			return "", fmt.Errorf("MCP 操作任务不存在")
		}
		if err := s.authorizeRoot(task.Root); err != nil {
			return "", err
		}
		return encodeResult(task)
	case "alemonjs_list_project_tasks":
		var input struct {
			Root string `json:"root"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if input.Root != "" {
			if err := s.authorizeRoot(input.Root); err != nil {
				return "", err
			}
		}
		return encodeResult(s.listTasks(input.Root))
	case "alemonjs_list_setup_plugins":
		return encodeResult(setupplugin.NewRegistry().List())
	case "alemonjs_run_setup_plugin":
		var input struct {
			PluginID string            `json:"pluginId"`
			ActionID string            `json:"actionId"`
			Values   map[string]string `json:"values"`
			Confirm  bool              `json:"confirm"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if !input.Confirm {
			return "", fmt.Errorf("此操作会运行 Setup 插件；请在用户明确确认后传 confirm=true")
		}
		output, err := setupplugin.NewRegistry().Run(input.PluginID, input.ActionID, input.Values, true)
		return output, err
	case "alemonjs_create_project":
		var input struct {
			Config  project.Config `json:"config"`
			Confirm bool           `json:"confirm"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return "", err
		}
		if !input.Confirm {
			return "", fmt.Errorf("创建项目会写入磁盘并安装依赖；请在用户明确确认后传 confirm=true")
		}
		if err := s.authorizeDestination(input.Config.Destination); err != nil {
			return "", err
		}
		if s.templates == nil {
			return "", fmt.Errorf("当前运行包未包含项目模板")
		}
		result, err := project.NewCreator(s.templates).Create(input.Config)
		text, marshalErr := encodeResult(result)
		if marshalErr != nil {
			return "", marshalErr
		}
		return text, err
	default:
		return "", fmt.Errorf("未知 MCP 工具")
	}
}

func (s *Server) authorizeRoot(root string) error {
	if len(s.policy.AllowedRoots) == 0 {
		return nil
	}
	if root == "." {
		current, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("无法读取当前工作目录：%w", err)
		}
		root = current
	}
	candidate, err := canonicalPath(root)
	if err != nil {
		return fmt.Errorf("无法解析项目目录：%w", err)
	}
	for _, allowed := range s.policy.AllowedRoots {
		allowedPath, err := canonicalPath(allowed)
		if err != nil {
			return fmt.Errorf("MCP_ALLOWED_ROOTS 包含无法访问的目录：%w", err)
		}
		relative, err := filepath.Rel(allowedPath, candidate)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("项目目录不在 MCP_ALLOWED_ROOTS 允许范围内")
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func (s *Server) authorizeDestination(destination string) error {
	if len(s.policy.AllowedRoots) == 0 {
		return nil
	}
	return s.authorizeRoot(destination)
}

func decodeArguments(data json.RawMessage, target any) error {
	if len(data) == 0 || string(data) == "null" {
		return fmt.Errorf("缺少工具参数")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("工具参数无效：%w", err)
	}
	return nil
}

func allowedAction(action string) bool {
	return strings.Contains(",install,build,dev,pm2,pm2-stop,pm2-status,install-package,uninstall-package,commit,npm-version,git-release,npm-publish,", ","+action+",")
}

func validEnvironmentCheck(goalID, variant string) bool {
	switch goalID {
	case "install", "develop", "desktop", "mobile":
		return variant == ""
	case "web":
		return variant == "clean" || variant == "docker"
	case "build":
		return variant == "npm" || variant == "git"
	default:
		return false
	}
}

func encodeResult(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (s *Server) startTask(root, action string, run func() (string, error)) operationTask {
	id := fmt.Sprintf("mcp-%d", s.taskID.Add(1))
	task := operationTask{ID: id, Root: root, Action: action, Status: "running", CreatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.tasks[id] = task
	s.mu.Unlock()
	go func() {
		output, err := run()
		finished := time.Now().UTC()
		s.mu.Lock()
		current := s.tasks[id]
		current.FinishedAt = &finished
		current.Output = output
		if s.stopping[id] {
			current.Status = "stopped"
			delete(s.stopping, id)
		} else if err != nil {
			current.Status, current.Error = "failed", err.Error()
		} else {
			current.Status = "completed"
		}
		s.tasks[id] = current
		s.mu.Unlock()
	}()
	return task
}

func (s *Server) startDevelopment(root string) operationTask {
	id := fmt.Sprintf("mcp-%d", s.taskID.Add(1))
	task := operationTask{ID: id, Root: root, Action: "dev", Status: "running", CreatedAt: time.Now().UTC()}
	if _, err := s.robots.Run(root, "dependency-status", "", "", "", "", "", false); err != nil {
		task.Status, task.Error = "failed", err.Error()
		finished := time.Now().UTC()
		task.FinishedAt = &finished
		s.mu.Lock()
		s.tasks[id] = task
		s.mu.Unlock()
		return task
	}
	command, err := s.robots.DevelopmentCommand(root)
	if err != nil {
		task.Status, task.Error = "failed", err.Error()
		finished := time.Now().UTC()
		task.FinishedAt = &finished
		s.mu.Lock()
		s.tasks[id] = task
		s.mu.Unlock()
		return task
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		task.Status, task.Error = "failed", err.Error()
		finished := time.Now().UTC()
		task.FinishedAt = &finished
		s.mu.Lock()
		s.tasks[id] = task
		s.mu.Unlock()
		return task
	}
	s.mu.Lock()
	s.tasks[id], s.processes[id] = task, command
	s.mu.Unlock()
	go func() {
		err := command.Wait()
		finished := time.Now().UTC()
		s.mu.Lock()
		current := s.tasks[id]
		current.FinishedAt, current.Output = &finished, output.String()
		if s.stopping[id] {
			current.Status = "stopped"
			delete(s.stopping, id)
		} else if err != nil {
			current.Status, current.Error = "failed", err.Error()
		} else {
			current.Status = "completed"
		}
		s.tasks[id] = current
		delete(s.processes, id)
		s.mu.Unlock()
	}()
	return task
}

func (s *Server) stopDevelopment(id string) (string, error) {
	s.mu.RLock()
	task, taskOK := s.tasks[id]
	command, processOK := s.processes[id]
	s.mu.RUnlock()
	if !taskOK || task.Action != "dev" {
		return "", fmt.Errorf("开发任务不存在")
	}
	if err := s.authorizeRoot(task.Root); err != nil {
		return "", err
	}
	if !processOK || command.Process == nil {
		return "", fmt.Errorf("开发任务未在运行")
	}
	s.mu.Lock()
	s.stopping[id] = true
	s.mu.Unlock()
	if err := command.Process.Kill(); err != nil {
		s.mu.Lock()
		delete(s.stopping, id)
		s.mu.Unlock()
		return "", fmt.Errorf("停止开发机器人失败：%w", err)
	}
	return "已请求停止开发机器人。请继续查询任务状态确认进程已退出。", nil
}

func (s *Server) getTask(id string) (operationTask, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	return task, ok
}

func (s *Server) listTasks(root string) []operationTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tasks := make([]operationTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		if root == "" || task.Root == root {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func (s *Server) readResource(id json.RawMessage, params json.RawMessage) rpcResponse {
	var input struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &input); err != nil || input.URI != "alemonjs://mcp/capabilities" {
		return errorResponse(id, -32602, "资源不存在")
	}
	text, err := encodeResult(map[string]any{"version": s.version, "transport": "stdio or protected local Streamable HTTP", "scopes": []string{"project-status", "project-files", "local-packages", "confirmed-project-actions"}, "allowedRoots": s.policy.AllowedRoots, "blocked": []string{"arbitrary shell", "secret files", "git metadata", "dependency directories", "symbolic links"}, "confirmation": "任何写入、项目命令或对外发布都需要 confirm=true；NPM 发布和 GIT 发布还应先执行预检。"})
	if err != nil {
		return errorResponse(id, -32603, "资源编码失败")
	}
	return resultResponse(id, map[string]any{"contents": []map[string]string{{"uri": input.URI, "mimeType": "application/json", "text": text}}})
}

func toolResult(text string, err error) map[string]any {
	if err != nil {
		return map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true}
	}
	result := map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
	var structured any
	if json.Unmarshal([]byte(text), &structured) == nil {
		if object, ok := structured.(map[string]any); ok {
			result["structuredContent"] = object
		} else {
			result["structuredContent"] = map[string]any{"data": structured}
		}
	} else {
		result["structuredContent"] = map[string]any{"output": text}
	}
	return result
}

func resultResponse(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}
func errorResponse(id json.RawMessage, code int, message string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

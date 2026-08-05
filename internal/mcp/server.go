// Package mcp exposes the guarded AlemonJS project operations through the
// Model Context Protocol (MCP) stdio transport.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"alemonjs-setup/internal/project"
	"alemonjs-setup/internal/robot"
)

const protocolVersion = "2025-06-18"

type Server struct {
	version   string
	templates fs.FS
	robots    robot.Manager
	policy    Policy
	mu        sync.RWMutex
	tasks     map[string]operationTask
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
	return &Server{version: version, templates: templates, policy: policy, tasks: map[string]operationTask{}}
}

// HTTPHandler exposes the same MCP server to a local HTTP client. The caller
// must bind it to loopback or place it behind a real authorization gateway.
// A non-empty bearer token is mandatory so another local process cannot call
// project-management tools without the user's explicit configuration.
func (s *Server) HTTPHandler(token string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("MCP-Protocol-Version", protocolVersion)
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			writeHTTPError(writer, http.StatusMethodNotAllowed, errorResponse(nil, -32600, "MCP HTTP 仅支持 POST"))
			return
		}
		if token == "" || request.Header.Get("Authorization") != "Bearer "+token {
			writeHTTPError(writer, http.StatusUnauthorized, errorResponse(nil, -32001, "MCP HTTP 认证失败"))
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
		tool("alemonjs_list_project_files", "列出项目文件", "列出机器人项目内可由 AI 管理的源码和配置文件。会排除密钥、Git 元数据、依赖目录和符号链接。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。")}, "root"), true, false),
		tool("alemonjs_read_project_file", "读取项目文件", "读取机器人项目内的源码或配置文件。不能读取 .env、.npmrc、密钥、Git 元数据、依赖目录或符号链接。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "path": stringSchema("相对于机器人项目根目录的文件路径，例如 src/index.ts。")}, "root", "path"), true, false),
		tool("alemonjs_write_project_file", "写入项目文件", "创建或更新机器人项目中的源码或配置文件。必须在用户明确确认后调用；不能写入密钥、Git 元数据、依赖目录或符号链接。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "path": stringSchema("相对于机器人项目根目录的文件路径；父目录必须已存在。"), "content": stringSchema("完整的新文本内容。"), "confirm": map[string]any{"type": "boolean", "description": "用户已经明确确认本次文件写入时为 true。"}}, "root", "path", "content", "confirm"), false, true),
		tool("alemonjs_list_local_packages", "列出本地包", "列出机器人项目 packages 目录中已发现的本地 AlemonJS 包。", objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。")}, "root"), true, false),
		tool("alemonjs_start_project_action", "启动项目操作", "异步执行受限项目操作，并返回可供轮询的任务 ID。必须在用户明确同意后传 confirm=true；不支持任意 shell 命令、后台开发服务或远端发布。", actionSchema(), false, true),
		tool("alemonjs_get_project_task", "查询项目操作", "查询一个 MCP 项目操作任务的实时状态和输出。", objectSchema(map[string]any{"taskId": stringSchema("alemonjs_start_project_action 返回的任务 ID。")}, "taskId"), true, false),
		tool("alemonjs_list_project_tasks", "列出项目操作", "列出当前 MCP 会话创建的项目操作任务。", objectSchema(map[string]any{"root": stringSchema("可选。仅返回该机器人项目的操作任务。")}), true, false),
		{"name": "alemonjs_create_project", "description": "使用内置模板创建 AlemonJS 项目，并安装依赖。会写入磁盘、联网下载依赖，必须在用户明确确认后调用。", "inputSchema": objectSchema(map[string]any{"config": map[string]any{"type": "object", "description": "与 AlemonJS Setup 创建向导相同的项目配置。"}, "confirm": map[string]any{"type": "boolean", "description": "用户已经明确确认创建和安装依赖时为 true。"}}, "config", "confirm")},
	}
}

func tool(name, title, description string, schema map[string]any, readOnly, destructive bool) map[string]any {
	return map[string]any{"name": name, "title": title, "description": description, "inputSchema": schema, "annotations": map[string]any{"readOnlyHint": readOnly, "destructiveHint": destructive, "openWorldHint": false}}
}

func actionSchema() map[string]any {
	return objectSchema(map[string]any{"root": stringSchema("机器人项目的绝对路径。"), "action": map[string]any{"type": "string", "enum": []string{"install", "build", "git-init", "pm2", "install-package", "uninstall-package", "commit", "npm-version"}, "description": "要执行的受限操作。"}, "package": stringSchema("仅 install-package/uninstall-package 时需要，且必须是受支持的 AlemonJS 包。"), "message": stringSchema("仅 commit 时需要，作为 Git 提交说明。"), "version": stringSchema("仅 npm-version 时需要，格式为 1.2.3。"), "confirm": map[string]any{"type": "boolean", "description": "用户已经明确确认本次本机修改或命令执行时为 true。"}}, "root", "action", "confirm")
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
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
	case "alemonjs_start_project_action":
		var input struct {
			Root    string `json:"root"`
			Action  string `json:"action"`
			Package string `json:"package"`
			Message string `json:"message"`
			Version string `json:"version"`
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
		return encodeResult(s.startTask(input.Root, input.Action, func() (string, error) {
			result, err := s.robots.Run(input.Root, input.Action, input.Message, input.Package, input.Version, "", "", true)
			return result.Output, err
		}))
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
	candidate, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("无法解析项目目录：%w", err)
	}
	for _, allowed := range s.policy.AllowedRoots {
		allowedPath, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(allowedPath, candidate)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("项目目录不在 MCP_ALLOWED_ROOTS 允许范围内")
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
	return strings.Contains(",install,build,git-init,pm2,install-package,uninstall-package,commit,npm-version,", ","+action+",")
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
		if err != nil {
			current.Status, current.Error = "failed", err.Error()
		} else {
			current.Status = "completed"
		}
		s.tasks[id] = current
		s.mu.Unlock()
	}()
	return task
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
	text, err := encodeResult(map[string]any{"version": s.version, "transport": "stdio or protected local HTTP", "scopes": []string{"project-status", "project-files", "local-packages", "confirmed-project-actions"}, "allowedRoots": s.policy.AllowedRoots, "blocked": []string{"arbitrary shell", "external publishing", "secret files", "git metadata", "dependency directories", "symbolic links"}, "confirmation": "任何写入或项目命令都需要 confirm=true。"})
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

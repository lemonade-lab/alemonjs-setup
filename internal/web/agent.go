package web

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"alemonx/internal/agent"
	"alemonx/internal/robot"
)

// agentChatHandler runs a bounded agentic conversation against the configured
// provider. Write tools are authorized at the task level: sending this message
// is the user's consent for this task to modify the selected project. Command
// execution remains restricted to the whitelist, and per-step confirmation
// lands with the Phase 1 UI.
//
// With ?stream=1 the handler responds as a Server-Sent Events stream, pushing
// one JSON event per progress step (text/tool/result/done/error) so the UI can
// render tool activity live. Without it, a single JSON result is returned.
func (s *server) agentChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		Provider  string              `json:"provider"`
		Model     string              `json:"model"`
		Root      string              `json:"root"`
		SessionID string              `json:"sessionId"`
		Access    string              `json:"access"`
		Messages  []map[string]string `json:"messages"`
	}
	access := input.Access
	if access == "" {
		access = "full"
	}
	if access != "ask" && access != "auto" && access != "full" {
		writeError(w, http.StatusBadRequest, "权限模式无效（应为 ask/auto/full）。")
		return
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求无法识别。")
		return
	}
	if len(input.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "请填写要发送的消息。")
		return
	}
	if len(input.Messages) > 30 {
		writeError(w, http.StatusBadRequest, "一次对话最多保留 30 条消息。")
		return
	}
	if _, err := (robot.Manager{}).Validate(input.Root); err != nil {
		writeError(w, http.StatusBadRequest, "请先选择一个有效的机器人目录。")
		return
	}
	cfg, err := s.ai.Resolve(input.Provider, input.Model)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Session bookkeeping: persist the user message now and the full transcript
	// after the run. A missing sessionId auto-creates one, using the first user
	// message as the conversation title.
	sessionID := input.SessionID
	if sessionID == "" {
		title := ""
		if len(input.Messages) > 0 {
			title = titleFromMessage(input.Messages[len(input.Messages)-1]["content"])
		}
		session, createErr := s.agentSessions.Create(input.Root, input.Provider, input.Model, title)
		if createErr == nil {
			sessionID = session.ID
		}
	}
	if sessionID != "" {
		if len(input.Messages) > 0 {
			_ = s.agentSessions.Append(sessionID, agent.Message{Role: "user", Content: input.Messages[len(input.Messages)-1]["content"]})
		}
	}

	messages := make([]agent.Message, 0, len(input.Messages))
	for _, raw := range input.Messages {
		messages = append(messages, agent.Message{Role: raw["role"], Content: raw["content"]})
	}
	files := robotFileService{manager: robot.Manager{}}
	registry := agent.ProjectTools(input.Root, files, agent.NewCommandRunner())
	systemPrompt := agent.BuildSystemPrompt(input.Root, files, agentBasePrompt())
	loop := agent.NewLoop(cfg, registry, systemPrompt, 40)
	loop.WithContextBudget(120 * 1024)

	stream := r.URL.Query().Get("stream") == "1"
	var emit func(agent.Event)
	if stream {
		emit = agentObserver(w, sessionID, r.Context())
		loop.WithObserver(emit)
	}

	// Permission model:
	//   ask  — each write tool waits for explicit user approval (streaming only).
	//   auto — 替我审核：文件修改自动批准（白名单命令本就可执行）。
	//   full — 完全访问：本次会话内全部自动授权。
	switch access {
	case "ask":
		if stream {
			loop.WithApprover(askApprover(s.agentConfirms, emit, sessionID))
		} else {
			// Non-streaming cannot surface an interactive prompt; fall back to
			// task-level authorization so one-shot requests keep working.
			loop.WithApprover(taskApprover(input.Root))
		}
	default: // auto（替我审核）与 full（完全访问）
		loop.WithApprover(taskApprover(input.Root))
	}
	loop.WithAutoVerify()

	start := time.Now()
	result, err := loop.Run(r.Context(), messages)
	s.logAgentRun(input.Root, len(messages), time.Since(start), err)
	if err != nil {
		if stream && emit != nil {
			// stream 出错时先向 SSE 写 error 事件再关闭，避免浏览器只看到
			// 连接重置而报 "network error"。
			emit(agent.Event{Type: "error", Text: err.Error()})
			return
		}
		if stream {
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// 只有运行成功时 result 非 nil；出错时 result 为 nil，不能遍历。
	// The user message was already persisted above; skipping it here avoids
	// duplicating the first turn in the transcript.
	if sessionID != "" && result != nil {
		for _, message := range result.Messages {
			if message.Role == "system" || message.Role == "user" {
				continue
			}
			_ = s.agentSessions.Append(sessionID, message)
		}
	}
	if stream {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"answer": result.Answer, "sessionId": sessionID})
}

// agentObserver writes each agent event as a flushed SSE frame. It runs on the
// same goroutine as the loop, so it can write to the response directly. The
// sessionId is attached to the done event so a streaming client learns which
// conversation to resume. A background heartbeat keeps the stream alive during
// long tool/model waits, preventing proxies and browsers from treating a quiet
// stream as a dead connection ("network error").
func agentObserver(w http.ResponseWriter, sessionID string, ctx context.Context) func(agent.Event) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// The loop still runs to completion; events are simply not surfaced.
		return func(agent.Event) {}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// 不要设 Connection: keep-alive：它会与 http-proxy 的 chunked 透传冲突
	// 并触发 "Expected LF after chunk data"。SSE 依赖 Transfer-Encoding:
	// chunked，由 Go 自动处理。
	// 立即发送 headers 和一个 start 事件，让客户端立刻知道连接已建立，
	// 而不是在首次模型响应前一直空等。
	startData, _ := json.Marshal(agent.Event{Type: "start", Text: "Agent 已开始执行"})
	_, _ = w.Write([]byte("data: " + string(startData) + "\n\n"))
	flusher.Flush()
	// http.ResponseWriter 不是并发安全的：心跳 goroutine 与主事件写入必须
	// 串行，否则并发写会破坏 chunked 流（ERR_INVALID_CHUNKED_ENCODING）。
	var writeMu sync.Mutex
	write := func(payload string) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_, _ = w.Write([]byte(payload))
		flusher.Flush()
	}
	// 心跳：每 15 秒写一个 SSE 注释行（客户端忽略），保持连接不被超时断开。
	// 连接关闭（ctx done）即停止。
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				write(": keepalive\n\n")
			}
		}
	}()
	return func(event agent.Event) {
		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		write("data: " + string(data) + "\n\n")
		if event.Type == "done" && sessionID != "" {
			sessionEvent, _ := json.Marshal(agent.Event{Type: "session", Tool: sessionID})
			write("data: " + string(sessionEvent) + "\n\n")
		}
	}
}

// robotFileService adapts the robot manager's path-safe project file access to
// the agent's FileService interface.
type robotFileService struct {
	manager robot.Manager
}

func (f robotFileService) ReadFile(root, path string) (string, error) {
	result, err := f.manager.ReadProjectFile(root, path)
	return result.Output, err
}

func (f robotFileService) WriteFile(root, path, content string) error {
	_, err := f.manager.WriteProjectFile(root, path, content)
	return err
}

func (f robotFileService) CreateFile(root, path, content string) error {
	_, err := f.manager.CreateProjectFile(root, path, content)
	return err
}

func (f robotFileService) DeleteFile(root, path string) error {
	_, err := f.manager.DeleteProjectFile(root, path)
	return err
}

func (f robotFileService) ListFiles(root string) ([]string, error) {
	return f.manager.ListProjectFiles(root)
}

// taskApprover authorizes every write tool for the duration of one task (auto
// 替我审核 / full 完全访问). The loop already routes only write tools here;
// read-only and command tools are governed by their own whitelists.
func taskApprover(root string) agent.Approver {
	return func(ctx context.Context, call agent.ToolCall) error {
		return nil
	}
}

func (s *server) logAgentRun(root string, turns int, elapsed time.Duration, err error) {
	status := "ok"
	if err != nil {
		status = "failed"
	}
	s.agentLog("%s agent run turns=%d elapsed=%s status=%s", root, turns, elapsed.Round(time.Millisecond), status)
}

func (s *server) agentLog(format string, args ...any) {
	log.Printf("agent: "+format, args...)
}

// agentBasePrompt is the fixed portion of the agent's system prompt; project
// grounding (AGENTS.md, manifest, structure) is appended by the agent package.
func agentBasePrompt() string {
	return "你是一个运行在本机机器人管理台中的 AI 助手，正在协助管理 AlemonJS 机器人项目。\n" +
		"工具：读取、搜索、精确编辑项目文件，运行白名单命令（tsgo/tsc/eslint 验证、package.json 中声明的脚本）。\n" +
		"工作方式：\n" +
		"- 目标导向：只做用户要求的事，达成目标后立即停止，不要继续读取或探索无关内容。\n" +
		"- 少即是多：一次只读取完成任务所需的最小文件集；能用搜索定位就别读整个文件。\n" +
		"- 并行工具调用：需要读取多个相关文件时，在同一轮里同时调用 read_project_file，不要一次读一个。\n" +
		"- 修改前先读准目标；一次只改一个文件的一段代码，保持项目现有风格。\n" +
		"- 每次修改后用 agent_run_command 跑验证；失败必须修复，通过即停止，不要重复验证。"
}

// titleFromMessage derives a short session title from the first user message.
// It takes the first 2-8 characters, stripping leading whitespace and markdown.
func titleFromMessage(content string) string {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimLeft(trimmed, "#*-_> ")
	runes := []rune(trimmed)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) > 8 {
		runes = runes[:8]
	}
	if len(runes) < 2 {
		return string(runes)
	}
	return string(runes)
}

// agentSessionsHandler lists agent sessions (GET) or creates one (POST).
func (s *server) agentSessionsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions, err := s.agentSessions.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sessions)
	case http.MethodPost:
		var input struct {
			Root     string `json:"root"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
			Title    string `json:"title"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求无法识别。")
			return
		}
		if _, err := (robot.Manager{}).Validate(input.Root); err != nil {
			writeError(w, http.StatusBadRequest, "请先选择一个有效的机器人目录。")
			return
		}
		session, err := s.agentSessions.Create(input.Root, input.Provider, input.Model, input.Title)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, session)
	default:
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
	}
}

// agentSessionHandler loads (GET) or deletes (DELETE) one session by id.
func (s *server) agentSessionHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/agent/sessions/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "会话 ID 无效。")
		return
	}
	switch r.Method {
	case http.MethodGet:
		session, err := s.agentSessions.Get(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		messages, err := s.agentSessions.Load(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": session, "messages": messages})
	case http.MethodDelete:
		if err := s.agentSessions.Delete(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": "会话已删除。"})
	case http.MethodPatch:
		var input struct {
			Title    string `json:"title"`
			Archived *bool  `json:"archived"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "请求无法识别。")
			return
		}
		var session agent.Session
		var err error
		if input.Archived != nil {
			session, err = s.agentSessions.Archive(id, *input.Archived)
		} else {
			session, err = s.agentSessions.Rename(id, input.Title)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, session)
	default:
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
	}
}

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"alemonx/internal/ai"
)

// Handler executes one tool. It receives the raw JSON arguments and returns
// the text that is fed back to the model. Errors are returned as text to the
// model so it can adjust, not surfaced as loop failures.
type Handler func(ctx context.Context, arguments json.RawMessage) (string, error)

// Registry maps tool names to their declarative description and handler.
type Registry struct {
	tools    map[string]Tool
	handlers map[string]Handler
	writes   map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}, handlers: map[string]Handler{}, writes: map[string]bool{}}
}

// Add registers a tool. A later Add with the same name replaces the earlier one.
func (r *Registry) Add(tool Tool, handler Handler) {
	r.tools[tool.Name] = tool
	r.handlers[tool.Name] = handler
}

// AddWrite registers a tool that modifies the project. After a write tool
// runs, the loop auto-invokes the verify tool so the agent checks its own work.
func (r *Registry) AddWrite(tool Tool, handler Handler) {
	r.Add(tool, handler)
	r.writes[tool.Name] = true
}

// IsWrite reports whether name is a registered write tool.
func (r *Registry) IsWrite(name string) bool {
	return r.writes[name]
}

// List returns the registered tools in an unspecified order.
func (r *Registry) List() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		out = append(out, tool)
	}
	return out
}

// Approver decides whether a tool call may execute. A nil error allows the
// call; a non-nil error rejects it and is fed back to the model as the tool
// result so it can adjust its approach.
type Approver func(ctx context.Context, call ToolCall) error

// Event is a structured progress notification emitted during a run. The type
// is one of "text", "tool", "result", "error" or "done".
type Event struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Output string `json:"output,omitempty"`
	Diff   *Diff  `json:"diff,omitempty"`
}

// Diff describes one file change proposed by a write tool, for rendering a
// before/after preview in the confirmation dialog.
type Diff struct {
	Path string `json:"path"`
	Hunks []struct {
		Old string `json:"old"`
		New string `json:"new"`
	} `json:"hunks,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Content string `json:"content,omitempty"`
}

// verifyToolName is the reserved tool name the loop calls after any write tool.
const verifyToolName = "agent_verify"

// Loop drives the agentic loop against one resolved provider.
type Loop struct {
	cfg      ai.Resolved
	registry *Registry
	system   string
	maxTurns int
	approve  Approver
	observer func(Event)
	verify   bool
	budget   int
}

func NewLoop(cfg ai.Resolved, registry *Registry, system string, maxTurns int) *Loop {
	if maxTurns <= 0 {
		maxTurns = 20
	}
	return &Loop{cfg: cfg, registry: registry, system: system, maxTurns: maxTurns}
}

// WithApprover installs the confirmation policy. The default allows every
// call; the web layer supplies a real policy once per-step confirmation lands.
func (l *Loop) WithApprover(approve Approver) *Loop {
	l.approve = approve
	return l
}

// WithObserver installs a callback that receives progress events as the loop
// executes. It runs on the calling goroutine, so it must not block on the
// result of the run.
func (l *Loop) WithObserver(observer func(Event)) *Loop {
	l.observer = observer
	return l
}

// WithContextBudget sets a soft character budget for the conversation history.
// When the accumulated messages exceed it, the oldest tool-result pairs are
// compressed into a short summary so long runs do not overflow the provider's
// context window. A value of 0 disables compression.
func (l *Loop) WithContextBudget(budget int) *Loop {
	l.budget = budget
	return l
}

// WithAutoVerify enables the verify loop: after every write tool, the loop
// runs the registered agent_verify tool and feeds its output back to the
// model. The registry must contain a handler for agent_verify or the write
// tool's result is left untouched.
func (l *Loop) WithAutoVerify() *Loop {
	l.verify = true
	return l
}

// callVerify invokes the agent_verify handler and returns its text output.
func (l *Loop) callVerify(ctx context.Context) (string, bool) {
	handler, ok := l.registry.handlers[verifyToolName]
	if !ok {
		return "", false
	}
	output, err := handler(ctx, nil)
	if err != nil {
		output = "验证命令失败：" + err.Error()
	}
	return output, true
}

// Result is the final outcome of an agent run. Messages holds the full
// transcript including assistant tool-call turns and tool results.
type Result struct {
	Answer   string
	Messages []Message
}

// Run executes the conversation until the model stops calling tools or the
// turn budget is exhausted. The caller's messages are appended to and
// mutated: they are replayed to the provider with tool results inserted.
func (l *Loop) Run(ctx context.Context, messages []Message) (*Result, error) {
	// Guard against transcripts that already carry orphaned tool messages
	// (e.g. loaded from a persisted session where the assistant tool_calls was
	// filtered out). OpenAI rejects a `tool` role without a preceding
	// `tool_calls`.
	messages = pruneOrphanTools(messages)
	if l.system != "" {
		messages = append([]Message{{Role: "system", Content: l.system}}, messages...)
	}
	tools := l.registry.List()
	for turn := 0; turn < l.maxTurns; turn++ {
		messages = compressIfOverBudget(messages, l.budget)
		response, err := RoundTrip(ctx, l.cfg, messages, tools)
		if err != nil {
			if l.observer != nil {
				l.observer(Event{Type: "error", Text: err.Error()})
			}
			return nil, err
		}
		if len(response.ToolCalls) == 0 {
			if l.observer != nil {
				l.observer(Event{Type: "done", Text: response.Content})
			}
			// 把最终回答追加进 transcript，这样调用方能持久化完整的
			// 对话历史（含最后的 assistant 总结）。
			messages = append(messages, Message{Role: "assistant", Content: response.Content})
			return &Result{Answer: response.Content, Messages: messages}, nil
		}
		if response.Content != "" && l.observer != nil {
			l.observer(Event{Type: "text", Text: response.Content})
		}
		messages = append(messages, Message{Role: "assistant", Content: response.Content, ToolCalls: response.ToolCalls})
		messages = l.executeToolCalls(ctx, messages, response.ToolCalls)
	}
	if l.observer != nil {
		l.observer(Event{Type: "error", Text: "agent 达到最大轮数，未能完成任务"})
	}
	return nil, errors.New("agent 达到最大轮数，未能完成任务")
}

// executeToolCalls runs the model's requested tools and appends their results.
// Write tools execute serially (each may require user approval and triggers
// verification); read-only tools run concurrently to cut round-trip latency.
func (l *Loop) executeToolCalls(ctx context.Context, messages []Message, calls []ToolCall) []Message {
	// First pass: serial write tools (approval gate + auto-verify).
	for _, call := range calls {
		if !l.registry.IsWrite(call.Name) {
			continue
		}
		if l.approve != nil {
			if err := l.approve(ctx, call); err != nil {
				output := "用户拒绝了该操作：" + err.Error()
				if l.observer != nil {
					l.observer(Event{Type: "result", Tool: call.Name, Output: output})
				}
				messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: output})
				continue
			}
		}
		messages = l.runOneTool(ctx, messages, call)
		// After a write tool, auto-run verification so the model sees whether
		// its change broke the project before continuing.
		if l.verify {
			if verifyOutput, ok := l.callVerify(ctx); ok {
				if l.observer != nil {
					l.observer(Event{Type: "tool", Tool: verifyToolName, Text: "写操作后自动验证"})
				}
				verifyID := "verify-" + call.ID
				messages = append(messages, Message{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: verifyID, Name: verifyToolName, Arguments: json.RawMessage("{}")}}})
				if l.observer != nil {
					l.observer(Event{Type: "result", Tool: verifyToolName, Output: verifyOutput})
				}
				messages = append(messages, Message{Role: "tool", ToolCallID: verifyID, Content: verifyOutput})
				if formatted := FormatVerifyErrors(ParseVerifyErrors(verifyOutput)); formatted != "" {
					messages = append(messages, Message{Role: "user", Content: formatted})
				}
			}
		}
	}
	// Second pass: read-only tools, run concurrently.
	var readOnly []ToolCall
	for _, call := range calls {
		if !l.registry.IsWrite(call.Name) {
			readOnly = append(readOnly, call)
		}
	}
	if len(readOnly) > 0 {
		results := make([]toolResult, len(readOnly))
		var group sync.WaitGroup
		for index, call := range readOnly {
			group.Add(1)
			go func(index int, call ToolCall) {
				defer group.Done()
				if l.observer != nil {
					l.observer(Event{Type: "tool", Tool: call.Name, Text: string(call.Arguments)})
				}
				output := l.callHandler(ctx, call)
				results[index] = toolResult{callID: call.ID, output: output}
				if l.observer != nil {
					l.observer(Event{Type: "result", Tool: call.Name, Output: output})
				}
			}(index, call)
		}
		group.Wait()
		// Append in the original tool_calls order so transcripts stay stable.
		for _, result := range results {
			messages = append(messages, Message{Role: "tool", ToolCallID: result.callID, Content: result.output})
		}
	}
	return messages
}

type toolResult struct {
	callID string
	output string
}

// runOneTool executes a single tool handler and appends its tool message.
func (l *Loop) runOneTool(ctx context.Context, messages []Message, call ToolCall) []Message {
	output := l.callHandler(ctx, call)
	messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: output})
	return messages
}

// toolTimeout bounds a single tool execution so one slow tool cannot stall the
// whole agent loop.
const toolTimeout = 30 * time.Second

// callHandler dispatches one tool call, returning "错误：…" on failure. It
// wraps the handler in a timeout so a hung read or command cannot block the
// loop indefinitely.
func (l *Loop) callHandler(ctx context.Context, call ToolCall) string {
	handler, ok := l.registry.handlers[call.Name]
	if !ok {
		return "错误：未知工具 " + call.Name
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	output, err := handler(timeoutCtx, call.Arguments)
	if err != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return "错误：工具执行超时（>30 秒）"
		}
		return "错误：" + err.Error()
	}
	return output
}

// messageSize estimates the character weight of a message (content plus tool
// calls), used for context budgeting.
func messageSize(message Message) int {
	size := len(message.Content) + len(message.ToolCallID)
	for _, call := range message.ToolCalls {
		size += len(call.Name) + len(call.Arguments)
	}
	return size
}

// compressIfOverBudget trims the oldest tool-result pairs once the transcript
// exceeds the budget. It keeps the most recent activity intact and prepends a
// one-line marker so the model still sees the shape of the conversation. A
// budget of 0 disables this.
func compressIfOverBudget(messages []Message, budget int) []Message {
	if budget <= 0 {
		return messages
	}
	total := 0
	for _, message := range messages {
		total += messageSize(message)
	}
	if total <= budget {
		return messages
	}
	out := make([]Message, 0, len(messages))
	keptTail := 0
	// Measure from the back so we always preserve the most recent exchanges.
	for index := len(messages) - 1; index >= 0; index-- {
		keptTail += messageSize(messages[index])
		if keptTail > budget {
			break
		}
		out = append(out, messages[index])
	}
	// Reverse back into chronological order.
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	// Drop orphaned tool messages: OpenAI requires each `tool` response to
	// follow an `assistant` message carrying matching tool_calls. If the fold
	// boundary cut between them, the surviving `tool` has no referent.
	out = pruneOrphanTools(out)
	note := Message{Role: "user", Content: "（较早的工具调用结果已折叠以节省上下文，继续当前任务即可。）"}
	return append([]Message{note}, out...)
}

// pruneOrphanTools removes tool messages whose ToolCallID has no matching
// tool_calls in a preceding assistant message. This keeps transcripts valid
// for OpenAI after context compression.
func pruneOrphanTools(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	// Track the set of tool call IDs that are still "open" (declared by an
	// assistant message and not yet answered by a tool message).
	openCalls := make(map[string]bool)
	for _, message := range messages {
		if len(message.ToolCalls) > 0 {
			for _, call := range message.ToolCalls {
				if call.ID != "" {
					openCalls[call.ID] = true
				}
			}
			out = append(out, message)
			continue
		}
		if message.Role == "tool" {
			if !openCalls[message.ToolCallID] {
				// No preceding declaration — drop it.
				continue
			}
			delete(openCalls, message.ToolCallID)
			out = append(out, message)
			continue
		}
		out = append(out, message)
	}
	return out
}

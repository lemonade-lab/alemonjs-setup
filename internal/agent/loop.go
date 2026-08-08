package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
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
	TaskID string `json:"taskId,omitempty"`
	StepID string `json:"stepId,omitempty"`
	Text   string `json:"text,omitempty"`
	Tool   string `json:"tool,omitempty"`
	CallID string `json:"callId,omitempty"`
	Turn   int    `json:"turn,omitempty"`
	Output string `json:"output,omitempty"`
	Diff   *Diff  `json:"diff,omitempty"`
}
type VerificationResult struct {
	Passed bool
	Output string
	Error  string
}

// Diff describes one file change proposed by a write tool, for rendering a
// before/after preview in the confirmation dialog.
type Diff struct {
	Path  string `json:"path"`
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
	cfg                  ai.Resolved
	registry             *Registry
	system               string
	maxTurns             int
	approve              Approver
	observer             func(Event)
	checkpoint           func(int, []Message)
	verify               bool
	verificationObserver func(VerificationResult)
	budget               int
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

// WithCheckpoint receives a copy of the transcript after each completed
// round. The caller owns persistence and may atomically store it for resume.
func (l *Loop) WithCheckpoint(checkpoint func(int, []Message)) *Loop {
	l.checkpoint = checkpoint
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
func (l *Loop) WithVerificationObserver(observer func(VerificationResult)) *Loop {
	l.verificationObserver = observer
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
		if l.verificationObserver != nil {
			l.verificationObserver(VerificationResult{Output: output, Error: err.Error()})
		}
	} else if l.verificationObserver != nil {
		l.verificationObserver(VerificationResult{Passed: true, Output: output})
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
	messages = PruneOrphanTools(messages)
	if l.system != "" {
		messages = append([]Message{{Role: "system", Content: l.system}}, messages...)
	}
	tools := l.registry.List()
	for turn := 0; turn < l.maxTurns; turn++ {
		if l.observer != nil {
			l.observer(Event{Type: "turn", Turn: turn + 1, Text: "Agent 正在处理第 " + strconv.Itoa(turn+1) + " 轮"})
		}
		messages = compressIfOverBudget(messages, l.budget)
		// compress 折叠旧消息可能切断 assistant tool_calls 与 tool 响应的
		// 配对，发送前必须重新清理，否则 DeepSeek 报 tool_calls 无响应。
		messages = PruneOrphanTools(messages)
		// Use assignment, not :=. A short declaration inside this loop would
		// shadow the function parameter `messages`, dropping the tool transcript
		// before the next model round.
		var response RoundTripResult
		var err error
		response, messages, err = l.roundTripWithRecovery(ctx, messages, tools)
		if err != nil {
			if l.observer != nil {
				l.observer(Event{Type: "error", Text: userSafeModelFailure})
			}
			// Keep the provider's exact response in the server log only. It can
			// contain implementation details that are neither actionable nor safe
			// to expose in a user-facing conversation.
			log.Printf("agent model request paused after recovery attempts: %v", err)
			return nil, &RecoverableError{Public: userSafeModelFailure, Cause: err}
		}
		if len(response.ToolCalls) == 0 {
			if l.observer != nil {
				l.observer(Event{Type: "done", Text: response.Content})
			}
			// 把最终回答追加进 transcript，这样调用方能持久化完整的
			// 对话历史（含最后的 assistant 总结）。reasoning_content 也保存，
			// 供 DeepSeek thinking 模式下一轮回传。
			messages = append(messages, Message{Role: "assistant", Content: response.Content, ReasoningContent: response.ReasoningContent})
			if l.checkpoint != nil {
				l.checkpoint(turn+1, append([]Message(nil), messages...))
			}
			return &Result{Answer: response.Content, Messages: messages}, nil
		}
		if response.Content != "" && l.observer != nil {
			l.observer(Event{Type: "text", Text: response.Content})
		}
		messages = append(messages, Message{Role: "assistant", Content: response.Content, ToolCalls: response.ToolCalls, ReasoningContent: response.ReasoningContent})
		messages = l.executeToolCalls(ctx, messages, response.ToolCalls)
		if l.checkpoint != nil {
			l.checkpoint(turn+1, append([]Message(nil), messages...))
		}
	}
	if l.observer != nil {
		l.observer(Event{Type: "error", Text: "agent 达到最大轮数，未能完成任务"})
	}
	return nil, errors.New("agent 达到最大轮数，未能完成任务")
}

const userSafeModelFailure = "模型服务暂时无法继续处理，已保留当前进度。请稍后继续任务。"

// roundTripWithRecovery retries provider-side protocol and transient failures
// after normalising the transcript again. A retry is intentionally bounded:
// tasks remain responsive and, on persistent failure, can be resumed from the
// durable checkpoint instead of repeatedly consuming requests.
func (l *Loop) roundTripWithRecovery(ctx context.Context, messages []Message, tools []Tool) (RoundTripResult, []Message, error) {
	var lastErr error
	clean := PruneOrphanTools(messages)
	for attempt := 0; attempt < 3; attempt++ {
		response, err := RoundTrip(ctx, l.cfg, clean, tools)
		if err == nil {
			return response, clean, nil
		}
		lastErr = err
		// Thinking-mode providers require the private reasoning_content from an
		// assistant tool-call response to be replayed verbatim. Older checkpoints
		// created before that field was persisted cannot satisfy the protocol. The
		// missing value cannot be reconstructed, so discard that *complete* legacy
		// tool exchange and continue from the remaining durable conversation.
		// Keeping the assistant call and merely retrying would send the same invalid
		// request forever.
		if requiresReasoningReplay(err) {
			if repaired, changed := PruneToolCallsMissingReasoning(clean); changed {
				clean = repaired
				if l.observer != nil {
					l.observer(Event{Type: "text", Text: "检测到旧任务缺少 thinking 上下文，已安全重建后继续执行。"})
				}
				continue
			}
		}
		if ctx.Err() != nil || !shouldRetryModelRequest(err) || attempt == 2 {
			break
		}
		if l.observer != nil {
			l.observer(Event{Type: "text", Text: "模型服务响应异常，正在自动重试…"})
		}
		select {
		case <-ctx.Done():
			return RoundTripResult{}, clean, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}
	return RoundTripResult{}, clean, lastErr
}

func shouldRetryModelRequest(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "tool_calls") ||
		strings.Contains(text, "tool_call_id") ||
		strings.Contains(text, "reasoning_content") ||
		strings.Contains(text, "insufficient tool") ||
		strings.Contains(text, "timeout") ||
		strings.Contains(text, "temporar") ||
		strings.Contains(text, "rate limit") ||
		strings.Contains(text, " 429") ||
		strings.Contains(text, " 5")
}

// requiresReasoningReplay recognises the protocol error emitted by
// DeepSeek-compatible thinking models when a historical assistant tool call
// lost its reasoning_content during an interrupted run.
func requiresReasoningReplay(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "reasoning_content") &&
		(strings.Contains(text, "passed back") || strings.Contains(text, "thinking mode"))
}

// executeToolCalls runs the model's requested tools and appends their results.
// Write tools execute serially (each may require user approval and triggers
// verification); read-only tools run concurrently to cut round-trip latency.
func (l *Loop) executeToolCalls(ctx context.Context, messages []Message, calls []ToolCall) []Message {
	// Every response to one assistant tool_calls message must be contiguous and
	// in the same call order. In particular, never insert our synthetic verify
	// exchange between results from a mixed read/write tool batch.
	results := make([]toolResult, len(calls))
	wrote := make([]ToolCall, 0, len(calls))
	for index, call := range calls {
		if !l.registry.IsWrite(call.Name) {
			continue
		}
		output := ""
		if l.approve != nil {
			if err := l.approve(ctx, call); err != nil {
				output = "用户拒绝了该操作：" + err.Error()
			}
		}
		if output == "" {
			output = l.runTool(ctx, call)
			wrote = append(wrote, call)
		} else if l.observer != nil {
			l.observer(Event{Type: "result", Tool: call.Name, CallID: call.ID, Output: output})
		}
		results[index] = toolResult{callID: call.ID, output: output}
	}
	// Run read-only tools concurrently, but do not append their messages yet.
	var readOnly []ToolCall
	readIndexes := make([]int, 0, len(calls))
	for index, call := range calls {
		if !l.registry.IsWrite(call.Name) {
			readOnly = append(readOnly, call)
			readIndexes = append(readIndexes, index)
		}
	}
	if len(readOnly) > 0 {
		readResults := make([]toolResult, len(readOnly))
		var group sync.WaitGroup
		for index, call := range readOnly {
			group.Add(1)
			go func(index int, call ToolCall) {
				defer group.Done()
				if l.observer != nil {
					l.observer(Event{Type: "tool", Tool: call.Name, CallID: call.ID, Text: string(call.Arguments)})
				}
				output := l.callHandler(ctx, call)
				readResults[index] = toolResult{callID: call.ID, output: output}
				if l.observer != nil {
					l.observer(Event{Type: "result", Tool: call.Name, CallID: call.ID, Output: output})
				}
			}(index, call)
		}
		group.Wait()
		for index, result := range readResults {
			results[readIndexes[index]] = result
		}
	}
	// Append all original call results together, preserving the assistant's
	// declared order. This is required by OpenAI/DeepSeek-compatible APIs.
	for _, result := range results {
		messages = append(messages, Message{Role: "tool", ToolCallID: result.callID, Content: result.output})
	}
	// Auto-verification is a separate, complete exchange after the original
	// group is closed. One verification per successful write is retained so the
	// task planner can associate the result with that write.
	if l.verify {
		for _, call := range wrote {
			if verifyOutput, ok := l.callVerify(ctx); ok {
				verifyID := verifyIDFor(call.ID)
				if l.observer != nil {
					l.observer(Event{Type: "tool", Tool: verifyToolName, CallID: verifyID, Text: "写操作后自动验证"})
				}
				messages = append(messages, Message{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: verifyID, Name: verifyToolName, Arguments: json.RawMessage("{}")}}})
				if l.observer != nil {
					l.observer(Event{Type: "result", Tool: verifyToolName, CallID: verifyID, Output: verifyOutput})
				}
				messages = append(messages, Message{Role: "tool", ToolCallID: verifyID, Content: verifyOutput})
				if formatted := FormatVerifyErrors(ParseVerifyErrors(verifyOutput)); formatted != "" {
					messages = append(messages, Message{Role: "user", Content: formatted})
				}
			}
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
	output := l.runTool(ctx, call)
	messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: output})
	return messages
}

func (l *Loop) runTool(ctx context.Context, call ToolCall) string {
	if l.observer != nil {
		l.observer(Event{Type: "tool", Tool: call.Name, CallID: call.ID, Text: string(call.Arguments)})
	}
	output := l.callHandler(ctx, call)
	if l.observer != nil {
		l.observer(Event{Type: "result", Tool: call.Name, CallID: call.ID, Output: output})
	}
	return output
}

func verifyIDFor(callID string) string { return "verify-" + callID }

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
	// Messages carrying ReasoningContent must survive folding: DeepSeek's
	// thinking mode requires each assistant turn's reasoning_content to be
	// passed back, and dropping it makes the API reject the request. Collect
	// them up front so a large tool result earlier in the transcript cannot
	// hide them behind the budget break.
	reasoning := make([]Message, 0, 2)
	for _, message := range messages {
		if message.ReasoningContent != "" {
			reasoning = append(reasoning, message)
		}
	}
	// Measure from the back so we always preserve the most recent exchanges.
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].ReasoningContent != "" {
			continue
		}
		keptTail += messageSize(messages[index])
		if keptTail > budget {
			break
		}
		out = append(out, messages[index])
	}
	out = append(out, reasoning...)
	// Reverse back into chronological order.
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	// Drop orphaned tool messages: OpenAI requires each `tool` response to
	// follow an `assistant` message carrying matching tool_calls. If the fold
	// boundary cut between them, the surviving `tool` has no referent.
	out = PruneOrphanTools(out)
	// A small budget must never erase the active tool exchange completely. The
	// next provider request still needs the assistant tool_calls and all
	// contiguous results, even when that exchange alone exceeds the budget.
	if !containsToolExchange(out) {
		if start := lastToolExchangeStart(messages); start >= 0 {
			prefix := start
			for index := start - 1; index >= 0; index-- {
				if messages[index].Role == "user" {
					prefix = index
					break
				}
			}
			out = append(append([]Message(nil), messages[prefix:start]...), messages[start:]...)
		}
	}
	note := Message{Role: "user", Content: "（较早的工具调用结果已折叠以节省上下文，继续当前任务即可。）"}
	return append([]Message{note}, out...)
}

func containsToolExchange(messages []Message) bool {
	for _, message := range messages {
		if len(message.ToolCalls) > 0 || message.Role == "tool" {
			return true
		}
	}
	return false
}

func lastToolExchangeStart(messages []Message) int {
	start := -1
	for index, message := range messages {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			expected := make(map[string]struct{}, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				if call.ID != "" {
					expected[call.ID] = struct{}{}
				}
			}
			next := index + 1
			for ; next < len(messages) && messages[next].Role == "tool"; next++ {
				delete(expected, messages[next].ToolCallID)
			}
			// Only retain an exchange that is still at the active end of the
			// transcript. Earlier exchanges can be folded; retaining them would
			// reintroduce a huge tool result and defeat the context budget.
			if len(expected) == 0 && next == len(messages) {
				start = index
			}
		}
	}
	return start
}

// PruneOrphanTools keeps only complete, contiguous assistant-tool exchanges.
// OpenAI-compatible providers require every assistant tool_calls message to be
// followed immediately by tool messages for *all* of its call IDs; finding a
// matching ID elsewhere in history is not enough after compression/recovery.
func PruneOrphanTools(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for index := 0; index < len(messages); {
		message := messages[index]
		if len(message.ToolCalls) > 0 {
			expected := make(map[string]bool, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				if call.ID != "" {
					expected[call.ID] = true
				}
			}
			end := index + 1
			responses := make([]Message, 0, len(expected))
			for end < len(messages) && messages[end].Role == "tool" {
				response := messages[end]
				if expected[response.ToolCallID] {
					responses = append(responses, response)
					delete(expected, response.ToolCallID)
				}
				end++
			}
			if len(expected) != 0 {
				if message.ReasoningContent != "" {
					message.ToolCalls = nil
					out = append(out, message)
				}
				// The tool messages immediately after an incomplete call are part
				// of that invalid group, so drop them with the assistant message.
				index = end
				continue
			}
			out = append(out, message)
			out = append(out, responses...)
			index = end
			continue
		}
		if message.Role == "tool" {
			// A bare tool result can never be legal: it must have been consumed
			// as part of the contiguous group above.
			index++
			continue
		}
		out = append(out, message)
		index++
	}
	return out
}

// PruneToolCallsMissingReasoning removes complete assistant/tool exchanges
// that cannot be replayed to thinking-mode providers. reasoning_content is
// generated by the provider and is deliberately not guessed or synthesized;
// a checkpoint that lost it must resume from the surrounding user context.
//
// The function only removes assistant messages that actually carry tool calls.
// Normal assistant answers from non-thinking providers are retained.
func PruneToolCallsMissingReasoning(messages []Message) ([]Message, bool) {
	out := make([]Message, 0, len(messages))
	changed := false
	for index := 0; index < len(messages); {
		message := messages[index]
		if message.Role != "assistant" || len(message.ToolCalls) == 0 || message.ReasoningContent != "" {
			out = append(out, message)
			index++
			continue
		}

		// Prune the assistant call and only its immediately following tool
		// responses. This preserves later user decisions and final answers.
		changed = true
		index++
		for index < len(messages) && messages[index].Role == "tool" {
			index++
		}
	}
	return out, changed
}

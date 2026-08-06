package agent

import (
	"context"
	"encoding/json"
	"errors"

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
	if l.system != "" {
		messages = append([]Message{{Role: "system", Content: l.system}}, messages...)
	}
	tools := l.registry.List()
	for turn := 0; turn < l.maxTurns; turn++ {
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
			return &Result{Answer: response.Content, Messages: messages}, nil
		}
		if response.Content != "" && l.observer != nil {
			l.observer(Event{Type: "text", Text: response.Content})
		}
		messages = append(messages, Message{Role: "assistant", Content: response.Content, ToolCalls: response.ToolCalls})
		for _, call := range response.ToolCalls {
			// Only write tools go through the approval gate. Read-only and
			// command tools are governed by their own whitelists and must never
			// be blocked by a confirmation policy.
			if l.approve != nil && l.registry.IsWrite(call.Name) {
				if err := l.approve(ctx, call); err != nil {
					output := "用户拒绝了该操作：" + err.Error()
					if l.observer != nil {
						l.observer(Event{Type: "result", Tool: call.Name, Output: output})
					}
					messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: output})
					continue
				}
			}
			handler, ok := l.registry.handlers[call.Name]
			if !ok {
				output := "错误：未知工具 " + call.Name
				if l.observer != nil {
					l.observer(Event{Type: "result", Tool: call.Name, Output: output})
				}
				messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: output})
				continue
			}
			if l.observer != nil {
				l.observer(Event{Type: "tool", Tool: call.Name, Text: string(call.Arguments)})
			}
			output, err := handler(ctx, call.Arguments)
			if err != nil {
				output = "错误：" + err.Error()
			}
			if l.observer != nil {
				l.observer(Event{Type: "result", Tool: call.Name, Output: output})
			}
			messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: output})
			// After a write tool, auto-run verification so the model sees
			// whether its change broke the project before continuing.
			if l.verify && l.registry.IsWrite(call.Name) {
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
				}
			}
		}
	}
	if l.observer != nil {
		l.observer(Event{Type: "error", Text: "agent 达到最大轮数，未能完成任务"})
	}
	return nil, errors.New("agent 达到最大轮数，未能完成任务")
}

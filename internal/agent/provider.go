// Package agent implements an in-process agentic loop for ALemonX's built-in
// AI assistant. It converts each provider's tool-use protocol into one unified
// message model and drives a bounded number of tool round-trips.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alemonx/internal/ai"
)

// Tool describes a function the model may call. Parameters is a JSON Schema
// object describing the arguments; the provider adapters translate it into
// their own wire format.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ToolCall is a request from the model to invoke one tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Message is one unified turn in a conversation. Role is one of system, user,
// assistant or tool. An assistant message carries ToolCalls when the model
// asked to invoke tools; a tool message reports the result of one ToolCallID.
// ReasoningContent preserves DeepSeek's thinking-mode output, which must be
// passed back to the API on the next request.
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

// RoundTripResult is the model's response to one request.
type RoundTripResult struct {
	Content          string
	ToolCalls        []ToolCall
	StopReason       string
	ReasoningContent string
}

// RoundTrip sends one request with tools to the resolved provider and parses
// the model's text and/or tool calls.
func RoundTrip(ctx context.Context, cfg ai.Resolved, messages []Message, tools []Tool) (RoundTripResult, error) {
	if cfg.Anthropic {
		return anthropicRoundTrip(ctx, cfg, messages, tools)
	}
	return openAIRoundTrip(ctx, cfg, messages, tools)
}

func openAIRoundTrip(ctx context.Context, cfg ai.Resolved, messages []Message, tools []Tool) (RoundTripResult, error) {
	wire := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{"role": message.Role}
		switch message.Role {
		case "tool":
			item["content"] = message.Content
			item["tool_call_id"] = message.ToolCallID
		default:
			if len(message.ToolCalls) > 0 {
				item["content"] = nil
				calls := make([]map[string]any, 0, len(message.ToolCalls))
				for _, call := range message.ToolCalls {
					calls = append(calls, map[string]any{
						"id":       call.ID,
						"type":     "function",
						"function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)},
					})
				}
				item["tool_calls"] = calls
			} else {
				item["content"] = message.Content
			}
			// DeepSeek thinking mode: the reasoning_content from a prior turn
			// must be passed back to the API on the next request.
			if message.ReasoningContent != "" {
				item["reasoning_content"] = message.ReasoningContent
			}
		}
		wire = append(wire, item)
	}
	body := map[string]any{"model": cfg.Model, "messages": wire, "stream": false}
	if len(tools) > 0 {
		list := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			list = append(list, map[string]any{
				"type":     "function",
				"function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters},
			})
		}
		body["tools"] = list
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return RoundTripResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return RoundTripResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	var data struct {
		Choices []struct {
			Message struct {
				Content          *string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := doJSON(req, &data); err != nil {
		return RoundTripResult{}, err
	}
	if len(data.Choices) == 0 {
		return RoundTripResult{}, errors.New(data.Error.Message)
	}
	message := data.Choices[0].Message
	result := RoundTripResult{
		StopReason:       data.Choices[0].FinishReason,
		ReasoningContent: message.ReasoningContent,
	}
	if content := message.Content; content != nil {
		result.Content = *content
	}
	for _, call := range message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: json.RawMessage(call.Function.Arguments),
		})
	}
	return result, nil
}

func anthropicRoundTrip(ctx context.Context, cfg ai.Resolved, messages []Message, tools []Tool) (RoundTripResult, error) {
	systemParts := make([]string, 0)
	wire := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "system":
			systemParts = append(systemParts, message.Content)
		case "tool":
			block := map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content}
			if len(wire) > 0 && wire[len(wire)-1]["role"] == "user" {
				content, _ := wire[len(wire)-1]["content"].([]any)
				wire[len(wire)-1]["content"] = append(content, block)
			} else {
				wire = append(wire, map[string]any{"role": "user", "content": []any{block}})
			}
		default:
			item := map[string]any{"role": message.Role}
			if len(message.ToolCalls) > 0 {
				blocks := make([]any, 0, len(message.ToolCalls)+1)
				if message.Content != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": message.Content})
				}
				for _, call := range message.ToolCalls {
					blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": rawInput(call.Arguments)})
				}
				item["content"] = blocks
			} else {
				item["content"] = message.Content
			}
			wire = append(wire, item)
		}
	}
	body := map[string]any{"model": cfg.Model, "max_tokens": 4096, "messages": wire}
	if len(systemParts) > 0 {
		body["system"] = strings.Join(systemParts, "\n\n")
	}
	if len(tools) > 0 {
		list := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			list = append(list, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.Parameters})
		}
		body["tools"] = list
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return RoundTripResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return RoundTripResult{}, err
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	var data struct {
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Error      struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := doJSON(req, &data); err != nil {
		return RoundTripResult{}, err
	}
	result := RoundTripResult{StopReason: data.StopReason}
	for _, block := range data.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: block.ID, Name: block.Name, Arguments: block.Input})
		}
	}
	return result, nil
}

// rawInput converts tool arguments for Anthropic's input field, which is an
// object, not a JSON string. An empty argument set becomes an empty object.
func rawInput(raw json.RawMessage) any {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}
	}
	return json.RawMessage(raw)
}

// httpClient is a package-level variable so tests can inject a fake transport
// without binding a network port.
var httpClient = &http.Client{Timeout: 120 * time.Second}

func doJSON(req *http.Request, target any) error {
	response, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerError(raw, response.Status)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("AI 响应无法解析：%w", err)
	}
	return nil
}

func providerError(raw []byte, status string) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Error.Message != "" {
		return fmt.Errorf("AI 请求失败：%s", payload.Error.Message)
	}
	message := strings.TrimSpace(string(raw))
	if len(message) > 500 {
		message = message[:500] + "…"
	}
	if message != "" {
		return fmt.Errorf("AI 请求失败：%s", message)
	}
	return fmt.Errorf("AI 请求失败：%s", status)
}

package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"alemonx/internal/agent"
)

// confirmTimeout bounds how long an agent run waits for a user decision before
// treating the call as rejected.
const confirmTimeout = 5 * time.Minute

// confirmDecision is the user's verdict for one pending tool call.
type confirmDecision struct {
	approved bool
}

// agentConfirmManager tracks tool calls waiting for the user's explicit
// approval. An agent run registers a channel under a confirm ID; the browser
// resolves it through POST /api/v1/agent/approve.
type agentConfirmManager struct {
	mu   sync.Mutex
	pend map[string]chan confirmDecision
}

func newAgentConfirmManager() *agentConfirmManager {
	return &agentConfirmManager{pend: map[string]chan confirmDecision{}}
}

// register opens a pending confirmation and returns the channel the approver
// should block on, plus a cleanup func that removes the entry.
func (m *agentConfirmManager) register(id string) (chan confirmDecision, func()) {
	ch := make(chan confirmDecision, 1)
	m.mu.Lock()
	m.pend[id] = ch
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.pend, id)
		m.mu.Unlock()
	}
}

// resolve delivers a decision for a pending confirm ID. It returns false when
// no such confirmation is pending.
func (m *agentConfirmManager) resolve(id string, approved bool) bool {
	m.mu.Lock()
	ch, ok := m.pend[id]
	delete(m.pend, id)
	m.mu.Unlock()
	if !ok {
		return false
	}
	ch <- confirmDecision{approved: approved}
	return true
}

// agentConfirmHandler resolves a pending confirmation (POST) for the streaming
// chat flow.
func (s *server) agentConfirmHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "该操作暂不支持。")
		return
	}
	var input struct {
		ConfirmID string `json:"confirmId"`
		Approve   bool   `json:"approve"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求无法识别。")
		return
	}
	if input.ConfirmID == "" {
		writeError(w, http.StatusBadRequest, "缺少确认 ID。")
		return
	}
	if !s.agentConfirms.resolve(input.ConfirmID, input.Approve) {
		writeError(w, http.StatusNotFound, "该确认已过期或不存在。")
		return
	}
	verb := "已拒绝"
	if input.Approve {
		verb = "已批准"
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": verb + "该操作。"})
}

// askApprover blocks each write tool on the user's explicit approval. It emits
// a confirm event on the SSE stream, then waits for the browser's verdict or a
// timeout. Any rejection or timeout is fed back to the model as a refusal.
func askApprover(confirms *agentConfirmManager, emit func(agent.Event), confirmID string) agent.Approver {
	return func(ctx context.Context, call agent.ToolCall) error {
		id := confirmID + ":" + call.ID
		ch, cleanup := confirms.register(id)
		defer cleanup()
		if emit != nil {
			emit(agent.Event{Type: "confirm", Tool: id, Text: call.Name, Output: string(call.Arguments)})
		}
		select {
		case decision := <-ch:
			if decision.approved {
				return nil
			}
			return errors.New("用户拒绝了该操作")
		case <-ctx.Done():
			return errors.New("请求已取消")
		case <-time.After(confirmTimeout):
			return errors.New("等待确认超时，操作未执行")
		}
	}
}

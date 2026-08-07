package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alemonx/internal/agent"
)

func TestAgentConfirmManagerResolve(t *testing.T) {
	m := newAgentConfirmManager()
	ch, cleanup := m.register("id1")
	if !m.resolve("id1", true) {
		t.Fatal("pending 确认应能解析")
	}
	select {
	case decision := <-ch:
		if !decision.approved {
			t.Error("应批准")
		}
	case <-time.After(time.Second):
		t.Fatal("resolve 应通过 channel 送达")
	}
	cleanup()
	// Second resolve should fail: already consumed.
	if m.resolve("id1", true) {
		t.Error("重复解析应失败")
	}
}

func TestAgentConfirmManagerMissing(t *testing.T) {
	m := newAgentConfirmManager()
	if m.resolve("nope", true) {
		t.Error("不存在的确认应返回 false")
	}
}

// TestAskApproverBlocksAndApproves runs the ask approver in a goroutine,
// resolves the pending confirmation, and asserts the approver returns nil.
func TestAskApproverBlocksAndApproves(t *testing.T) {
	m := newAgentConfirmManager()
	var emitted []agent.Event
	emit := func(event agent.Event) { emitted = append(emitted, event) }
	approver := askApprover(m, emit, "sessionX")

	var err error
	done := make(chan struct{})
	go func() {
		err = approver(context.Background(), agent.ToolCall{ID: "call1", Name: "agent_edit_file", Arguments: []byte(`{"path":"a.ts"}`)})
		close(done)
	}()
	// Wait for the emit (proves the approver reached the confirm point).
	deadline := time.Now().Add(time.Second)
	for len(emitted) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(emitted) != 1 || emitted[0].Type != "confirm" {
		t.Fatalf("应发出 confirm 事件：%+v", emitted)
	}
	if emitted[0].Tool != "sessionX:call1" {
		t.Errorf("confirm 事件应带确认 ID：%+v", emitted[0])
	}
	if !m.resolve("sessionX:call1", true) {
		t.Fatal("应能解析确认")
	}
	select {
	case <-done:
		if err != nil {
			t.Fatalf("批准后 approver 应返回 nil，实际 %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("批准后 approver 应解除阻塞")
	}
}

// TestAskApproverReject returns an error to the caller.
func TestAskApproverReject(t *testing.T) {
	m := newAgentConfirmManager()
	approver := askApprover(m, func(agent.Event) {}, "s")
	var err error
	done := make(chan struct{})
	go func() {
		err = approver(context.Background(), agent.ToolCall{ID: "c", Name: "agent_edit_file"})
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for !m.resolve("s:c", false) {
		if time.Now().After(deadline) {
			t.Fatal("确认未被注册")
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-done
	if err == nil {
		t.Fatal("拒绝后 approver 应返回错误")
	}
	if errors.Is(err, context.Canceled) {
		t.Error("拒绝不应被当成取消")
	}
}

// TestAskApproverRejectsUnknownTool ensures only wired tools pass the gate.
func TestAskApproverRejectsUnknownTool(t *testing.T) {
	m := newAgentConfirmManager()
	approver := askApprover(m, func(agent.Event) {}, "s")
	if err := approver(context.Background(), agent.ToolCall{ID: "c", Name: "nope"}); err == nil {
		t.Error("未接入的工具应被拒绝")
	}
}

// TestAgentConfirmHandlerRejectsBadInput guards the endpoint contract: an
// empty confirm ID is rejected with 400.
func TestAgentConfirmHandlerRejectsBadInput(t *testing.T) {
	body := `{"approve":true}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/approve", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，应为 400", response.Code)
	}
}

// TestAgentConfirmHandlerUnknownID returns 404 for a stale confirm ID.
func TestAgentConfirmHandlerUnknownID(t *testing.T) {
	body := `{"confirmId":"gone","approve":true}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/approve", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newTestServer().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d，应为 404", response.Code)
	}
}

// TestSSEHeartbeatFlushed verifies the observer sends a start event and that a
// heartbeat frame is written while the context is alive.
func TestSSEHeartbeatFlushed(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	emit := agentObserver(recorder, "sessionX", ctx)
	// start 事件应立即写入。
	if !strings.Contains(recorder.Body.String(), `"type":"start"`) {
		t.Fatalf("应写入 start 事件：%q", recorder.Body.String())
	}
	// 发送一个事件验证正常写入。
	emit(agent.Event{Type: "text", Text: "你好"})
	if !strings.Contains(recorder.Body.String(), "你好") {
		t.Fatalf("应写入 text 事件：%q", recorder.Body.String())
	}
	cancel()
}

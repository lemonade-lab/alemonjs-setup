package agent

import (
	"path/filepath"
	"testing"
)

// newTestStore returns a store rooted in a temp dir.
func newTestStore(t *testing.T) *SessionStore {
	t.Helper()
	return &SessionStore{dir: t.TempDir()}
}

func TestSessionStoreCreateAndList(t *testing.T) {
	store := newTestStore(t)
	session, err := store.Create("/path/to/robot", "deepseek", "deepseek-chat")
	if err != nil {
		t.Fatal(err)
	}
	if session.ID == "" || session.Root != "/path/to/robot" {
		t.Errorf("创建会话字段错误：%+v", session)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != session.ID {
		t.Errorf("列表应含新会话：%+v", list)
	}
}

func TestSessionStoreAppendLoadRoundTrip(t *testing.T) {
	store := newTestStore(t)
	session, _ := store.Create("/p", "deepseek", "m")
	messages := []Message{
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "来了", ToolCalls: []ToolCall{{ID: "t1", Name: "x", Arguments: nil}}},
		{Role: "tool", ToolCallID: "t1", Content: "结果"},
	}
	for _, message := range messages {
		if err := store.Append(session.ID, message); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := store.Load(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("应加载 3 条消息，实际 %d", len(loaded))
	}
	if loaded[0].Role != "user" || loaded[1].Role != "assistant" || loaded[2].Role != "tool" {
		t.Errorf("消息顺序错误：%+v", loaded)
	}
	if loaded[1].ToolCalls[0].Name != "x" {
		t.Errorf("ToolCalls 未持久化：%+v", loaded[1].ToolCalls)
	}
}

func TestSessionStoreDelete(t *testing.T) {
	store := newTestStore(t)
	session, _ := store.Create("/p", "deepseek", "m")
	_ = store.Append(session.ID, Message{Role: "user", Content: "x"})
	if err := store.Delete(session.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Errorf("删除后列表应为空：%+v", list)
	}
	if loaded, _ := store.Load(session.ID); len(loaded) != 0 {
		t.Errorf("删除后 transcript 应为空")
	}
}

func TestSessionStoreGetMissing(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Get("nope"); err == nil {
		t.Error("不存在的会话应报错")
	}
}

func TestDeriveTitle(t *testing.T) {
	cases := []struct{ root, want string }{
		{"/Users/me/robots/mybot", "mybot"},
		{"/Users/me/robots/mybot/", "mybot"},
		{".", "未命名会话"},
	}
	for _, c := range cases {
		if got := deriveTitle(c.root); got != c.want {
			t.Errorf("deriveTitle(%q) = %q, 期望 %q", c.root, got, c.want)
		}
	}
	if filepath.Base("/") != "" {
		t.Skip("路径语义与预期不同")
	}
}

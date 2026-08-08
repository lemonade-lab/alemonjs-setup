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
	session, err := store.Create("/path/to/robot", "deepseek", "deepseek-chat", "")
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

func TestSessionStoreLoadEmptyTranscriptReturnsEmptySlice(t *testing.T) {
	store := newTestStore(t)
	session, err := store.Create("/path/to/robot", "deepseek", "deepseek-chat", "")
	if err != nil {
		t.Fatal(err)
	}
	messages, err := store.Load(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if messages == nil || len(messages) != 0 {
		t.Fatalf("空会话应返回非 nil 空切片，实际：%#v", messages)
	}
}

func TestSessionStoreUpdateProgress(t *testing.T) {
	store := newTestStore(t)
	session, _ := store.Create("/p", "deepseek", "m", "")
	updated, err := store.UpdateProgress(session.ID, "running", 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "running" || updated.Turn != 3 || updated.LastError != "" {
		t.Fatalf("运行进度未保存：%+v", updated)
	}
	failed, err := store.UpdateProgress(session.ID, "failed", 4, "验证失败")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || failed.Turn != 4 || failed.LastError != "验证失败" {
		t.Fatalf("失败状态未保存：%+v", failed)
	}
}

func TestSessionStoreAppendLoadRoundTrip(t *testing.T) {
	store := newTestStore(t)
	session, _ := store.Create("/p", "deepseek", "m", "")
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
	session, _ := store.Create("/p", "deepseek", "m", "")
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

func TestSessionStoreCreateWithCustomTitle(t *testing.T) {
	store := newTestStore(t)
	session, err := store.Create("/p", "deepseek", "m", "我的新对话")
	if err != nil {
		t.Fatal(err)
	}
	if session.Title != "我的新对话" {
		t.Errorf("应使用自定义标题，实际 %q", session.Title)
	}
}

func TestSessionStoreCreateTitleLengthRule(t *testing.T) {
	store := newTestStore(t)
	// 1 字与 9 字都应回退到默认。
	short, _ := store.Create("/robot/alpha", "deepseek", "m", "短")
	if short.Title != "alpha" {
		t.Errorf("1 字标题应回退到目录名，实际 %q", short.Title)
	}
	long, _ := store.Create("/robot/beta", "deepseek", "m", "这是一个超过八个字的标题")
	if long.Title != "beta" {
		t.Errorf("超长标题应回退到目录名，实际 %q", long.Title)
	}
	ok, _ := store.Create("/robot/gamma", "deepseek", "m", "正好八个字")
	if ok.Title != "正好八个字" {
		t.Errorf("8 字标题应保留，实际 %q", ok.Title)
	}
}

func TestSessionStoreRename(t *testing.T) {
	store := newTestStore(t)
	session, _ := store.Create("/p", "deepseek", "m", "旧名")
	renamed, err := store.Rename(session.ID, "新名字")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Title != "新名字" {
		t.Errorf("重命名失败：%q", renamed.Title)
	}
	if _, err := store.Rename(session.ID, "  "); err == nil {
		t.Error("空标题应被拒绝")
	}
	if _, err := store.Rename("nope", "名字"); err == nil {
		t.Error("不存在的会话重命名应报错")
	}
}

func TestSessionStoreArchive(t *testing.T) {
	store := newTestStore(t)
	session, _ := store.Create("/p", "deepseek", "m", "对话")
	archived, err := store.Archive(session.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived {
		t.Error("应标记为已归档")
	}
	// 归档后 List 不再返回它。
	list, _ := store.List()
	for _, item := range list {
		if item.ID == session.ID {
			t.Error("归档会话不应出现在默认列表")
		}
	}
	// 但 Get/Load 仍可访问。
	if _, err := store.Get(session.ID); err != nil {
		t.Error("归档会话应仍可通过 Get 访问")
	}
	// 取消归档后恢复。
	if _, err := store.Archive(session.ID, false); err != nil {
		t.Fatal(err)
	}
	list, _ = store.List()
	found := false
	for _, item := range list {
		if item.ID == session.ID {
			found = true
		}
	}
	if !found {
		t.Error("取消归档后应重新出现在列表")
	}
}

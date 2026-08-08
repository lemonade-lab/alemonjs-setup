package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStoreCheckpointAndEventReplay(t *testing.T) {
	store := NewTaskStoreAt(t.TempDir())
	task := AgentTask{ID: "t1", Status: TaskQueued, Created: time.Now(), Updated: time.Now()}
	if err := store.SaveTask(task); err != nil {
		t.Fatal(err)
	}
	cp := AgentCheckpoint{TaskID: "t1", Version: 1, Messages: []Message{{Role: "user", Content: "继续"}}, Status: TaskRunning}
	if err := store.SaveCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadCheckpoint("t1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Messages[0].Content != "继续" {
		t.Fatalf("checkpoint 内容错误：%+v", loaded)
	}
	if err := store.AppendEvent("t1", TaskEvent{ID: 1, TaskID: "t1", Event: Event{Type: "turn", Turn: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent("t1", TaskEvent{ID: 2, TaskID: "t1", Event: Event{Type: "done"}}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadEvents("t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || string(events[0]) == "" {
		t.Fatalf("事件重放错误：%s", events)
	}
	if _, err := os.Stat(filepath.Join(store.TasksDir(), "t1", "checkpoint.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotRollbackDetectsExternalChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewSnapshotStoreAt(filepath.Join(t.TempDir(), "snapshots"))
	snap, err := store.Capture("t1", root, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("agent"), 0600); err != nil {
		t.Fatal(err)
	}
	snap.AfterHash = HashBytes([]byte("agent"))
	if err := store.Save("t1", []FileSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("human"), 0600); err != nil {
		t.Fatal(err)
	}
	if conflicts, err := store.Rollback("t1", root, false); err == nil || len(conflicts) != 1 {
		t.Fatalf("应检测外部修改，conflicts=%v err=%v", conflicts, err)
	}
	if _, err := store.Rollback("t1", root, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "before" {
		t.Fatalf("强制回滚内容错误：%q", data)
	}
}

func TestTaskManagerCancel(t *testing.T) {
	manager := NewTaskManager(NewTaskStoreAt(t.TempDir()))
	task, err := manager.Create(AgentTask{ID: "t1"}, func(ctx context.Context, task AgentTask, emit func(Event)) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, _ := manager.Get(task.ID)
		if current.Status == TaskCancelled {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("任务未进入 cancelled 状态")
}

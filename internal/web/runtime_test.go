package web

import (
	"context"
	"testing"

	"alemonx/internal/agent"
)

func TestServerRuntimeShutdownIsIdempotent(t *testing.T) {
	store := agent.NewTaskStoreAt(t.TempDir())
	tasks := agent.NewTaskManager(store)
	runtime := &ServerRuntime{server: &server{agentTasks: tasks, goalSchedulerStop: make(chan struct{})}}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

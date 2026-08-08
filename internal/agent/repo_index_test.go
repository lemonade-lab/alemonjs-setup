package agent

import "testing"

func TestBuildRepoIndexFindsSymbolsAndRoutes(t *testing.T) {
	files := newFakeFiles("/p")
	files.files["src/index.ts"] = "export function createBot() {}\nrouter.get('/health', handler)\nalemonjs.onEvent('message', handler)"
	index, err := BuildRepoIndex("/p", files)
	if err != nil {
		t.Fatal(err)
	}
	if len(FindSymbols(index, "createBot", "function")) != 1 {
		t.Fatalf("符号索引错误：%+v", index.Symbols)
	}
	if len(index.Routes) != 2 {
		t.Fatalf("路由/事件索引错误：%+v", index.Routes)
	}
}

func TestProjectWriteLockRejectsSecondOwner(t *testing.T) {
	release, err := AcquireProjectWriteLock("/tmp/project", "a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := AcquireProjectWriteLock("/tmp/project", "b"); err == nil {
		t.Fatal("第二个写任务应被拒绝")
	}
}

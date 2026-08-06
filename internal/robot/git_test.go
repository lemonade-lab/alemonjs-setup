package robot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitReleaseStatusDoesNotBlockLocalChangesOrMissingBuildOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"example","version":"1.0.0","scripts":{"build":"echo build"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test User"},
		{"config", "user.email", "test@example.com"},
		{"add", "package.json"},
		{"commit", "-m", "initial"},
	} {
		if _, err := gitRun(root, command...); err != nil {
			t.Skipf("git is unavailable for release-status test: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "local-note.txt"), []byte("not committed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	status, err := GitReleaseStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range status.Issues {
		if issue == "工作区有未提交修改；请先提交或暂存后再打包。" || issue == "尚未发现 lib 构建产物；发布时会先执行 build。" {
			t.Fatalf("local working files must not block a selected-commit release: %#v", status.Issues)
		}
	}
}

func TestGitWorkspaceReportsSourceControlWithoutReleaseChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test User"}, {"config", "user.email", "test@example.com"}, {"add", "note.txt"}, {"commit", "-m", "initial"}} {
		if _, err := gitRun(root, command...); err != nil {
			t.Skipf("git is unavailable for workspace test: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "changed.txt"), []byte("pending\n"), 0644); err != nil {
		t.Fatal(err)
	}
	status, err := GitWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Repository || status.Branch != "main" || len(status.Commits) != 1 {
		t.Fatalf("workspace = %#v", status)
	}
	if len(status.Changes) != 1 || status.Changes[0].Path != "changed.txt" {
		t.Fatalf("changes = %#v", status.Changes)
	}
}

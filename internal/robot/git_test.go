package robot

import (
	"os"
	"path/filepath"
	"strings"
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
	commitView, err := GitWorkspaceView(root, "commit")
	if err != nil || len(commitView.Changes) != 1 || len(commitView.Commits) != 0 || len(commitView.Tags) != 0 || len(commitView.Branches) != 0 {
		t.Fatalf("commit view must only load working changes: %#v, %v", commitView, err)
	}
	historyView, err := GitWorkspaceView(root, "history")
	if err != nil || len(historyView.Commits) != 1 || len(historyView.Changes) != 0 || len(historyView.Tags) != 0 {
		t.Fatalf("history view must only load commits: %#v, %v", historyView, err)
	}
	if _, err := GitWorkspaceAction(root, "tag-create", "v1.0.0", "release: v1.0.0"); err != nil {
		t.Fatal(err)
	}
	status, err = GitWorkspace(root)
	if err != nil || len(status.Tags) != 1 || status.Tags[0].Name != "v1.0.0" || !strings.Contains(status.Tags[0].Subject, "release") {
		t.Fatalf("tags = %#v, %v", status.Tags, err)
	}
	if len(status.Branches) != 1 || !status.Branches[0].Current || status.Branches[0].Name != "main" {
		t.Fatalf("branches = %#v", status.Branches)
	}
	if _, err := GitWorkspaceAction(root, "branch-create", "bad..branch", ""); err == nil {
		t.Fatal("invalid branch name should be rejected")
	}
}

func TestParseRemoteRefsUsesDefaultBranchAndOrdersTags(t *testing.T) {
	heads, tags, branch := parseRemoteRefs("ref: refs/heads/trunk\tHEAD\nabc\tHEAD\nabc\trefs/heads/trunk\ndef\trefs/heads/release\n1\trefs/tags/v0.0.9\n2\trefs/tags/v0.0.10\n")
	if branch != "trunk" || heads["trunk"] != "abc" || heads["release"] != "def" {
		t.Fatalf("remote heads = %#v, branch = %q", heads, branch)
	}
	if strings.Join(tags, ",") != "v0.0.10,v0.0.9" {
		t.Fatalf("tags = %#v", tags)
	}
}

func TestRemoteAdviceExplainsCommonFailures(t *testing.T) {
	if got := remoteAdvice("git@github.com: Permission denied (publickey)."); !strings.Contains(got, "SSH") {
		t.Fatalf("ssh advice = %q", got)
	}
	if got := remoteAdvice("fatal: repository not found"); !strings.Contains(got, "不存在") {
		t.Fatalf("not-found advice = %q", got)
	}
}

func TestGitWorkspaceListsFetchedRemoteBranches(t *testing.T) {
	root := t.TempDir()
	remote := t.TempDir()
	if _, err := gitRun(remote, "init", "--bare"); err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test User"}, {"config", "user.email", "test@example.com"}, {"add", "."}, {"commit", "-m", "initial"}, {"remote", "add", "origin", remote}, {"push", "-u", "origin", "main"}, {"switch", "-c", "feature/remote"}, {"push", "-u", "origin", "feature/remote"}, {"switch", "main"}} {
		if _, err := gitRun(root, command...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := GitWorkspaceAction(root, "fetch", "", ""); err != nil {
		t.Fatal(err)
	}
	status, err := GitWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, branch := range status.RemoteBranches {
		if branch.Name == "origin/feature/remote" && branch.Remote == "origin" && branch.Branch == "feature/remote" {
			found = true
		}
	}
	if !found {
		t.Fatalf("remote branches = %#v", status.RemoteBranches)
	}
}

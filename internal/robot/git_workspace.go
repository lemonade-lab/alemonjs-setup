package robot

import (
	"errors"
	"fmt"
	"strings"
)

// GitWorkspaceStatus describes source-control state only. It intentionally has
// no package-build or release-branch checks: Git management and publishing are
// separate user workflows.
type GitWorkspaceStatus struct {
	Root       string      `json:"root"`
	Repository bool        `json:"repository"`
	GitRoot    string      `json:"gitRoot,omitempty"`
	Remote     string      `json:"remote,omitempty"`
	Branch     string      `json:"branch,omitempty"`
	Upstream   string      `json:"upstream,omitempty"`
	Ahead      int         `json:"ahead"`
	Behind     int         `json:"behind"`
	Changes    []GitChange `json:"changes"`
	Branches   []string    `json:"branches"`
	Commits    []GitCommit `json:"commits"`
	Tags       []string    `json:"tags"`
}

type GitChange struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

func GitWorkspace(root string) (GitWorkspaceStatus, error) {
	path, err := workspacePath(root)
	if err != nil {
		return GitWorkspaceStatus{}, err
	}
	status := GitWorkspaceStatus{Root: path, Changes: []GitChange{}, Branches: []string{}, Commits: []GitCommit{}, Tags: []string{}}
	inside, err := gitRun(path, "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		return status, nil
	}
	status.Repository = true
	status.GitRoot, _ = gitRun(path, "rev-parse", "--show-toplevel")
	status.Remote, _ = gitRun(path, "remote", "get-url", "origin")
	status.Branch, _ = gitRun(path, "branch", "--show-current")
	status.Upstream, _ = gitRun(path, "rev-parse", "--abbrev-ref", "@{upstream}")
	if status.Upstream != "" {
		if counts, err := gitRun(path, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
			_, _ = fmt.Sscanf(counts, "%d\t%d", &status.Ahead, &status.Behind)
		}
	}
	if output, err := gitRun(path, "status", "--porcelain=v1"); err == nil {
		for _, line := range strings.Split(output, "\n") {
			if len(line) < 4 {
				continue
			}
			file := strings.TrimSpace(line[3:])
			if renamed := strings.LastIndex(file, " -> "); renamed >= 0 {
				file = strings.TrimSpace(file[renamed+4:])
			}
			status.Changes = append(status.Changes, GitChange{Status: strings.TrimSpace(line[:2]), Path: file})
		}
	}
	status.Branches = gitLines(path, "branch", "--format=%(refname:short)")
	status.Commits = sourceCommits(path)
	status.Tags = gitLines(path, "tag", "--list", "--sort=-v:refname")
	return status, nil
}

// GitWorkspaceAction only exposes named Git operations. It never executes a
// browser-provided shell command.
func GitWorkspaceAction(root, action, message string) (Result, error) {
	path, err := workspacePath(root)
	if err != nil {
		return Result{}, err
	}
	inside, err := gitRun(path, "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		return Result{}, errors.New("当前机器人目录尚未初始化 Git")
	}
	switch action {
	case "fetch":
		output, err := gitRun(path, "fetch", "--prune", "origin")
		return Result{Path: path, Output: output}, err
	case "pull":
		output, err := gitRun(path, "pull", "--ff-only")
		return Result{Path: path, Output: output}, err
	case "commit":
		message = strings.TrimSpace(message)
		if message == "" || strings.ContainsAny(message, "\r\n") {
			return Result{}, errors.New("请填写单行提交说明")
		}
		if output, err := gitRun(path, "add", "-A"); err != nil {
			return Result{Path: path, Output: output}, err
		}
		output, err := gitRun(path, "commit", "-m", message)
		return Result{Path: path, Output: output}, err
	default:
		return Result{}, errors.New("不支持的 Git 操作")
	}
}

package robot

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// GitWorkspaceStatus describes source-control state only. It intentionally has
// no package-build or release-branch checks: Git management and publishing are
// separate user workflows.
type GitWorkspaceStatus struct {
	Root           string            `json:"root"`
	Repository     bool              `json:"repository"`
	GitRoot        string            `json:"gitRoot,omitempty"`
	Remote         string            `json:"remote,omitempty"`
	Branch         string            `json:"branch,omitempty"`
	Upstream       string            `json:"upstream,omitempty"`
	Ahead          int               `json:"ahead"`
	Behind         int               `json:"behind"`
	Changes        []GitChange       `json:"changes"`
	Branches       []GitBranch       `json:"branches"`
	RemoteBranches []GitRemoteBranch `json:"remoteBranches"`
	Commits        []GitCommit       `json:"commits"`
	Tags           []GitTag          `json:"tags"`
	Remotes        []GitRemote       `json:"remotes"`
}

type GitBranch struct {
	Name     string `json:"name"`
	Current  bool   `json:"current"`
	Upstream string `json:"upstream,omitempty"`
}

// GitRemoteBranch is a branch advertised by a remote. It is read from the
// local remote-tracking refs after `fetch`; selecting it creates a normal
// local tracking branch, never a detached HEAD.
type GitRemoteBranch struct {
	Name   string `json:"name"`
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

type GitTag struct {
	Name      string `json:"name"`
	Subject   string `json:"subject,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type GitRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type GitChange struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

func GitWorkspace(root string) (GitWorkspaceStatus, error) { return GitWorkspaceView(root, "all") }

// GitWorkspaceView loads only the data needed by one tab. Large histories,
// tag lists and remote branch lists are therefore not read when opening the
// default "提交" tab.
func GitWorkspaceView(root, view string) (GitWorkspaceStatus, error) {
	path, err := workspacePath(root)
	if err != nil {
		return GitWorkspaceStatus{}, err
	}
	status := GitWorkspaceStatus{Root: path, Changes: []GitChange{}, Branches: []GitBranch{}, RemoteBranches: []GitRemoteBranch{}, Commits: []GitCommit{}, Tags: []GitTag{}, Remotes: []GitRemote{}}
	inside, err := gitRun(path, "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		return status, nil
	}
	status.Repository = true
	status.GitRoot, _ = gitRun(path, "rev-parse", "--show-toplevel")
	status.Branch, _ = gitRun(path, "branch", "--show-current")
	loadChanges := func() {
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
	}
	loadBranches := func() {
		for _, line := range gitLines(path, "for-each-ref", "--format=%(refname:short)|%(upstream:short)", "refs/heads") {
			parts := strings.SplitN(line, "|", 2)
			branch := GitBranch{Name: parts[0], Current: parts[0] == status.Branch}
			if len(parts) == 2 {
				branch.Upstream = parts[1]
			}
			status.Branches = append(status.Branches, branch)
		}
		for _, name := range gitLines(path, "for-each-ref", "--format=%(refname:short)", "refs/remotes") {
			parts := strings.SplitN(name, "/", 2)
			if len(parts) == 2 && !strings.HasSuffix(parts[1], "/HEAD") {
				status.RemoteBranches = append(status.RemoteBranches, GitRemoteBranch{Name: name, Remote: parts[0], Branch: parts[1]})
			}
		}
	}
	loadTags := func() {
		for _, name := range gitLines(path, "tag", "--list", "--sort=-v:refname") {
			tag := GitTag{Name: name}
			if detail, err := gitRun(path, "for-each-ref", "--format=%(contents:subject)|%(creatordate:iso-strict)", "refs/tags/"+name); err == nil {
				parts := strings.SplitN(detail, "|", 2)
				tag.Subject = parts[0]
				if len(parts) == 2 {
					tag.CreatedAt = parts[1]
				}
			}
			status.Tags = append(status.Tags, tag)
		}
	}
	loadRemotes := func() {
		status.Remote, _ = gitRun(path, "remote", "get-url", "origin")
		status.Upstream, _ = gitRun(path, "rev-parse", "--abbrev-ref", "@{upstream}")
		if status.Upstream != "" {
			if counts, err := gitRun(path, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
				_, _ = fmt.Sscanf(counts, "%d\t%d", &status.Ahead, &status.Behind)
			}
		}
		for _, name := range gitLines(path, "remote") {
			url, _ := gitRun(path, "remote", "get-url", name)
			status.Remotes = append(status.Remotes, GitRemote{Name: name, URL: url})
		}
	}
	switch view {
	case "commit":
		loadChanges()
	case "history":
		status.Commits = sourceCommits(path)
	case "tag":
		loadTags()
	case "branch":
		loadBranches()
		loadRemotes()
	case "remote":
		loadRemotes()
	case "all":
		loadChanges()
		loadBranches()
		status.Commits = sourceCommits(path)
		loadTags()
		loadRemotes()
	default:
		return GitWorkspaceStatus{}, errors.New("不支持的 Git 视图")
	}
	return status, nil
}

// GitWorkspaceAction only exposes named Git operations. It never executes a
// browser-provided shell command.
func GitWorkspaceAction(root, action, value, message string) (Result, error) {
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
		output, err := gitRun(path, "fetch", "--prune", "--tags", "origin")
		return Result{Path: path, Output: output}, err
	case "pull":
		output, err := gitRun(path, "pull", "--ff-only")
		return Result{Path: path, Output: output}, err
	case "push":
		if upstream, err := gitRun(path, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil || upstream == "" {
			branch := currentBranch(path)
			if branch == "" {
				return Result{}, errors.New("当前处于分离 HEAD 状态，请先在“分支”中切换到一个本地分支")
			}
			output, pushErr := gitRun(path, "push", "-u", "origin", branch)
			return Result{Path: path, Output: output}, pushErr
		}
		output, err := gitRun(path, "push")
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
	case "branch-create":
		if !validGitRef(value) {
			return Result{}, errors.New("分支名称无效")
		}
		output, err := gitRun(path, "switch", "-c", value)
		return Result{Path: path, Output: output}, err
	case "branch-switch":
		if !validGitRef(value) {
			return Result{}, errors.New("分支名称无效")
		}
		output, err := gitRun(path, "switch", value)
		return Result{Path: path, Output: output}, err
	case "branch-track":
		if !validRemoteBranch(value) {
			return Result{}, errors.New("远程分支名称无效")
		}
		parts := strings.SplitN(value, "/", 2)
		output, err := gitRun(path, "switch", "--track", "-c", parts[1], value)
		return Result{Path: path, Output: output}, err
	case "branch-delete":
		if !validGitRef(value) || value == currentBranch(path) {
			return Result{}, errors.New("不能删除当前分支，或分支名称无效")
		}
		output, err := gitRun(path, "branch", "-d", value)
		return Result{Path: path, Output: output}, err
	case "tag-create":
		if !validGitRef(value) {
			return Result{}, errors.New("标签名称无效")
		}
		message = strings.TrimSpace(message)
		if message == "" || strings.ContainsAny(message, "\r\n") {
			return Result{}, errors.New("请填写单行标签说明")
		}
		output, err := gitRun(path, "tag", "-a", value, "-m", message)
		return Result{Path: path, Output: output}, err
	case "tag-push":
		if !validGitRef(value) {
			return Result{}, errors.New("标签名称无效")
		}
		output, err := gitRun(path, "push", "origin", value)
		return Result{Path: path, Output: output}, err
	case "tag-delete":
		if !validGitRef(value) {
			return Result{}, errors.New("标签名称无效")
		}
		output, err := gitRun(path, "tag", "-d", value)
		return Result{Path: path, Output: output}, err
	case "remote-add", "remote-set-url":
		if !validRemoteName(value) || !validRemoteURL(message) {
			return Result{}, errors.New("远程名称或仓库地址无效")
		}
		args := []string{"remote", "add", value, message}
		if action == "remote-set-url" {
			args = []string{"remote", "set-url", value, message}
		}
		output, err := gitRun(path, args...)
		return Result{Path: path, Output: output}, err
	case "remote-remove":
		if !validRemoteName(value) {
			return Result{}, errors.New("远程名称无效")
		}
		output, err := gitRun(path, "remote", "remove", value)
		return Result{Path: path, Output: output}, err
	default:
		return Result{}, errors.New("不支持的 Git 操作")
	}
}

var gitRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var gitRemotePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validGitRef(value string) bool {
	value = strings.TrimSpace(value)
	return gitRefPattern.MatchString(value) && !strings.Contains(value, "..") && !strings.HasSuffix(value, ".") && !strings.HasSuffix(value, "/")
}

func validRemoteName(value string) bool {
	return gitRemotePattern.MatchString(strings.TrimSpace(value))
}

func validRemoteURL(value string) bool {
	value = strings.TrimSpace(value)
	return (strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "ssh://") || strings.Contains(value, "@")) && !strings.ContainsAny(value, "\r\n \t")
}

func currentBranch(root string) string {
	branch, _ := gitRun(root, "branch", "--show-current")
	return branch
}

func validRemoteBranch(value string) bool {
	parts := strings.SplitN(strings.TrimSpace(value), "/", 2)
	return len(parts) == 2 && validRemoteName(parts[0]) && validGitRef(parts[1])
}

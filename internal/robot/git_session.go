package robot

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GitBuildSession struct {
	ID        string    `json:"sessionId"`
	Branch    string    `json:"branch"`
	Commit    string    `json:"commit"`
	Target    string    `json:"target"`
	Files     []string  `json:"files"`
	Logs      string    `json:"logs"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type gitBuildState struct {
	GitBuildSession
	root           string
	worktree       string
	publishing     bool
	branchPushed   bool
	releaseCommit  string
	releaseVersion string
}

var gitBuildSessions = struct {
	sync.Mutex
	items map[string]gitBuildState
}{items: map[string]gitBuildState{}}

func PrepareGitBuild(root, branch, commit string) (GitBuildSession, error) {
	path, err := workspacePath(root)
	if err != nil {
		return GitBuildSession{}, err
	}
	status, err := GitReleaseStatus(path)
	if err != nil || len(status.Issues) > 0 {
		if err != nil {
			return GitBuildSession{}, err
		}
		return GitBuildSession{}, errors.New("发布前检查未通过：" + strings.Join(status.Issues, "；"))
	}
	if branch == "" {
		branch = status.Branch
	}
	if !validGitRef(branch) {
		return GitBuildSession{}, errors.New("请选择有效的源码分支")
	}
	branchRef, err := sourceBranchRef(path, branch)
	if err != nil {
		return GitBuildSession{}, err
	}
	commit, err = gitRun(path, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return GitBuildSession{}, errors.New("所选提交不存在")
	}
	if _, err = gitRun(path, "merge-base", "--is-ancestor", commit, branchRef); err != nil {
		return GitBuildSession{}, errors.New("所选提交不属于源码分支")
	}
	worktree, err := os.MkdirTemp("", "albs-git-build-")
	if err != nil {
		return GitBuildSession{}, err
	}
	cleanup := func() { _, _ = gitRun(path, "worktree", "remove", "--force", worktree); _ = os.RemoveAll(worktree) }
	output, err := gitRun(path, "worktree", "add", "--detach", worktree, commit)
	if err != nil {
		cleanup()
		return GitBuildSession{}, err
	}
	install, err := runPackageManager(worktree, "install")
	output += "\n" + install
	if err != nil {
		cleanup()
		return GitBuildSession{}, buildDependencyError("安装构建依赖失败", output)
	}
	build, err := runPackageManager(worktree, "run", "build")
	output += "\n" + build
	if err != nil {
		cleanup()
		return GitBuildSession{}, buildDependencyError("构建失败", output)
	}
	files := scanPublishFiles(worktree, "")
	if len(files) == 0 {
		cleanup()
		return GitBuildSession{}, errors.New("构建后没有可发布的产物")
	}
	bytes := make([]byte, 12)
	_, _ = rand.Read(bytes)
	id := hex.EncodeToString(bytes)
	createdAt := time.Now()
	session := GitBuildSession{ID: id, Branch: branch, Commit: commit, Target: releaseBranchName(branch, status.RemoteBranch), Files: files, Logs: strings.TrimSpace(output), CreatedAt: createdAt, ExpiresAt: createdAt.Add(30 * time.Minute)}
	gitBuildSessions.Lock()
	cleanupGitBuildSessions()
	gitBuildSessions.items[id] = gitBuildState{GitBuildSession: session, root: path, worktree: worktree}
	gitBuildSessions.Unlock()
	return session, nil
}

func buildDependencyError(action, output string) error {
	lower := strings.ToLower(output)
	if strings.Contains(lower, `the engine "node" is incompatible`) || strings.Contains(lower, "engine \"node\" is incompatible") {
		got := "当前 Node.js 版本"
		if marker := strings.Index(output, "Got \""); marker >= 0 {
			if rest := output[marker+len("Got \""):]; strings.Index(rest, "\"") >= 0 {
				got = "当前 Node.js " + rest[:strings.Index(rest, "\"")]
			}
		}
		return errors.New(action + "：" + got + " 不被项目依赖支持。请安装并切换到 Node.js 24 LTS（推荐），或切换到错误信息中要求的版本后重新构建；Node.js 25 等非 LTS 版本常会被依赖明确拒绝。")
	}
	return errors.New(action + "：" + strings.TrimSpace(output))
}
func scanPublishFiles(root, prefix string) []string {
	entries, _ := os.ReadDir(root)
	items := []string{}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") || n == "node_modules" || (prefix == "" && n == "package.json") {
			continue
		}
		rel := filepath.Join(prefix, n)
		if e.IsDir() {
			if children := scanPublishFiles(filepath.Join(root, n), rel); len(children) > 0 {
				items = append(items, rel)
				items = append(items, children...)
			}
		} else {
			items = append(items, rel)
		}
	}
	return items
}
func cleanupGitBuildSessions() {
	now := time.Now()
	for id, s := range gitBuildSessions.items {
		if now.Sub(s.CreatedAt) > 30*time.Minute {
			_, _ = gitRun(s.root, "worktree", "remove", "--force", s.worktree)
			_ = os.RemoveAll(s.worktree)
			delete(gitBuildSessions.items, id)
		}
	}
}

// CleanupGitBuildSessions releases detached worktrees when the setup process
// exits. Expired sessions are also cleaned before every session operation.
func CleanupGitBuildSessions() {
	gitBuildSessions.Lock()
	defer gitBuildSessions.Unlock()
	for id, state := range gitBuildSessions.items {
		_, _ = gitRun(state.root, "worktree", "remove", "--force", state.worktree)
		_ = os.RemoveAll(state.worktree)
		delete(gitBuildSessions.items, id)
	}
}

func PublishPreparedGitBuild(id, version string, artifacts []string, confirmed bool) (Result, error) {
	gitBuildSessions.Lock()
	cleanupGitBuildSessions()
	state, ok := gitBuildSessions.items[id]
	if ok && state.publishing {
		gitBuildSessions.Unlock()
		return Result{}, errors.New("该构建会话正在发布，请等待当前操作完成")
	}
	if ok {
		state.publishing = true
		gitBuildSessions.items[id] = state
	}
	gitBuildSessions.Unlock()
	if !ok {
		return Result{}, errors.New("构建会话已过期，请重新构建")
	}
	if len(artifacts) == 0 {
		return Result{}, errors.New("请至少选择一个最终产物")
	}
	allowed, seen := map[string]bool{}, map[string]bool{}
	for _, item := range state.Files {
		allowed[item] = true
	}
	selected := []string{}
	for _, item := range artifacts {
		item = filepath.Clean(item)
		if !validReleaseArtifact(item) || !allowed[item] || seen[item] {
			return Result{}, errors.New("所选产物无效或不属于本次构建：" + item)
		}
		seen[item] = true
		selected = append(selected, item)
	}
	result, err := publishPreparedWorktree(&state, version, selected, confirmed)
	gitBuildSessions.Lock()
	if err == nil {
		delete(gitBuildSessions.items, id)
	} else if _, exists := gitBuildSessions.items[id]; exists {
		state.publishing = false
		gitBuildSessions.items[id] = state
	}
	gitBuildSessions.Unlock()
	if err == nil {
		_, _ = gitRun(state.root, "worktree", "remove", "--force", state.worktree)
		_ = os.RemoveAll(state.worktree)
	}
	return result, err
}

// RetryPreparedGitTag is available only after the release branch commit was
// accepted but the tag push failed. It never rebuilds or pushes the branch.
func RetryPreparedGitTag(id string) (Result, error) {
	gitBuildSessions.Lock()
	cleanupGitBuildSessions()
	state, ok := gitBuildSessions.items[id]
	if !ok {
		gitBuildSessions.Unlock()
		return Result{}, errors.New("构建会话已过期，请重新构建")
	}
	if state.publishing {
		gitBuildSessions.Unlock()
		return Result{}, errors.New("该构建会话正在发布，请等待当前操作完成")
	}
	if !state.branchPushed || state.releaseCommit == "" || state.releaseVersion == "" {
		gitBuildSessions.Unlock()
		return Result{}, errors.New("当前会话没有可重试的标签推送")
	}
	state.publishing = true
	gitBuildSessions.items[id] = state
	gitBuildSessions.Unlock()

	result, err := retryPreparedGitTag(state)
	gitBuildSessions.Lock()
	if err == nil {
		delete(gitBuildSessions.items, id)
	} else if _, exists := gitBuildSessions.items[id]; exists {
		state.publishing = false
		gitBuildSessions.items[id] = state
	}
	gitBuildSessions.Unlock()
	if err == nil {
		_, _ = gitRun(state.root, "worktree", "remove", "--force", state.worktree)
		_ = os.RemoveAll(state.worktree)
	}
	return result, err
}

// publishPreparedWorktree deliberately consumes the exact worktree inspected by
// the user. Rebuilding here would make the selected artifact list misleading.
func publishPreparedWorktree(state *gitBuildState, version string, artifacts []string, confirmed bool) (Result, error) {
	status, err := GitReleaseStatus(state.root)
	if err != nil {
		return Result{}, err
	}
	if len(status.Issues) > 0 {
		return Result{}, errors.New("发布前检查未通过：" + strings.Join(status.Issues, "；"))
	}
	if version == "" {
		version = status.SuggestedVersion
	} else {
		version = "v" + strings.TrimPrefix(version, "v")
	}
	if !gitVersionPattern.MatchString(version) {
		return Result{}, errors.New("版本号应为 v1.2.3 或 1.2.3")
	}
	if _, err := gitRun(state.root, "rev-parse", "-q", "--verify", "refs/tags/"+version); err == nil {
		return Result{}, errors.New("版本标签 " + version + " 已存在，已发布版本不可覆盖")
	}
	if !confirmed {
		return Result{}, errors.New("请确认后再开始 GIT 发布")
	}
	logs := []string{"使用已完成的构建 " + shortGitSHA(state.Commit)}
	worktree, err := os.MkdirTemp("", "albs-release-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(worktree)
	start := "HEAD"
	if _, err := gitRun(state.root, "ls-remote", "--exit-code", "--heads", "origin", "refs/heads/"+state.Target); err == nil {
		output, err := gitRun(state.root, "fetch", "origin", state.Target)
		if err != nil {
			return Result{Path: state.root, Output: strings.Join(append(logs, output), "\n")}, errors.New("无法同步远程 " + state.Target + " 分支：" + output)
		}
		start = "origin/" + state.Target
	}
	output, err := gitRun(state.root, "worktree", "add", "--detach", worktree, start)
	if err != nil {
		return Result{Path: state.root, Output: strings.Join(append(logs, output), "\n")}, errors.New("无法创建安全的临时发布目录：" + output)
	}
	defer gitRun(state.root, "worktree", "remove", "--force", worktree)
	if output, err = gitRun(worktree, "rm", "-rf", "."); err != nil {
		return Result{}, errors.New("无法准备发布目录：" + output)
	}
	if output, err = gitRun(worktree, "clean", "-fdx"); err != nil {
		return Result{}, errors.New("无法清理发布目录：" + output)
	}
	if err := copyReleaseFiles(state.worktree, worktree, strings.TrimPrefix(version, "v"), artifacts); err != nil {
		return Result{}, err
	}
	mappingData, err := json.MarshalIndent(ReleaseMapping{Version: version, SourceBranch: state.Branch, SourceCommit: state.Commit}, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(worktree, ".albs-release.json"), append(mappingData, '\n'), 0644); err != nil {
		return Result{}, err
	}
	if _, err := gitRun(worktree, "add", "-A"); err != nil {
		return Result{}, err
	}
	commitMessage := "release: " + version + " (" + state.Branch + "@" + shortGitSHA(state.Commit) + ")"
	if output, err = gitRun(worktree, "commit", "-m", commitMessage); err != nil {
		return Result{}, errors.New("无法创建 release 提交（请先配置 Git 用户名和邮箱）：" + output)
	}
	if output, err = gitRun(worktree, "push", "origin", "HEAD:refs/heads/"+state.Target); err != nil {
		return Result{Path: state.root, Output: strings.Join(append(logs, output), "\n")}, errors.New(state.Target + " 分支推送失败：" + output)
	}
	state.releaseCommit, _ = gitRun(worktree, "rev-parse", "HEAD")
	state.releaseVersion = version
	state.branchPushed = true
	if output, err = gitRun(worktree, "tag", "-a", version, "-m", "Release "+version+"\n\nSource: "+state.Branch+"@"+state.Commit); err != nil {
		return Result{}, errors.New("release 分支已推送，但无法创建标签：" + output)
	}
	if output, err = gitRun(worktree, "push", "origin", version); err != nil {
		return Result{Path: state.root, Output: strings.Join(append(logs, output), "\n")}, errors.New("release 分支已推送，但标签未推送；请修复权限或网络后重试标签推送：" + output)
	}
	logs = append(logs, "已发布 "+version+"："+state.Target+" 分支、标签及源码映射已推送（"+state.Branch+"@"+shortGitSHA(state.Commit)+"）。")
	return Result{Path: state.root, Output: strings.Join(logs, "\n")}, nil
}

func retryPreparedGitTag(state gitBuildState) (Result, error) {
	worktree, err := os.MkdirTemp("", "albs-tag-retry-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(worktree)
	if output, err := gitRun(state.root, "worktree", "add", "--detach", worktree, state.releaseCommit); err != nil {
		return Result{}, errors.New("无法准备标签重试目录：" + output)
	}
	defer gitRun(state.root, "worktree", "remove", "--force", worktree)
	if _, err := gitRun(worktree, "tag", "-a", state.releaseVersion, "-m", "Release "+state.releaseVersion+"\n\nSource: "+state.Branch+"@"+state.Commit); err != nil {
		return Result{}, errors.New("无法创建重试标签：" + err.Error())
	}
	if output, err := gitRun(worktree, "push", "origin", state.releaseVersion); err != nil {
		_, _ = gitRun(worktree, "tag", "-d", state.releaseVersion)
		return Result{Path: state.root, Output: output}, errors.New("标签仍未推送；请检查网络或仓库写入权限后重试：" + output)
	}
	return Result{Path: state.root, Output: "已重试并推送标签 " + state.releaseVersion + "。release 分支没有重复提交。"}, nil
}

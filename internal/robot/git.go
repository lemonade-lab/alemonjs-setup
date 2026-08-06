package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var gitVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// GitStatus describes the package-release workflow.  A Git release here means
// publishing the built Node package to the project's release branch, not
// building alemonjs-setup itself.
type GitStatus struct {
	Root               string            `json:"root,omitempty"`
	Repository         string            `json:"repository,omitempty"`
	Branch             string            `json:"branch,omitempty"`
	RemoteBranch       string            `json:"remoteBranch,omitempty"`
	RemoteReachable    bool              `json:"remoteReachable"`
	RemoteAdvice       string            `json:"remoteAdvice,omitempty"`
	PackageName        string            `json:"packageName,omitempty"`
	PackageVersion     string            `json:"packageVersion,omitempty"`
	PackageManager     string            `json:"packageManager,omitempty"`
	GitHubActionsURL   string            `json:"gitHubActionsUrl,omitempty"`
	WorkflowConfigured bool              `json:"workflowConfigured"`
	GitReady           bool              `json:"gitReady"`
	ReleaseBranch      bool              `json:"releaseBranch"`
	LatestVersion      string            `json:"latestVersion,omitempty"`
	SuggestedVersion   string            `json:"suggestedVersion,omitempty"`
	Tags               []string          `json:"tags"`
	Commits            []string          `json:"commits"`
	SourceCommits      []GitCommit       `json:"sourceCommits"`
	SourceBranches     []GitSourceBranch `json:"sourceBranches"`
	ReleaseMappings    []ReleaseMapping  `json:"releaseMappings"`
	Checks             []string          `json:"checks"`
	Issues             []string          `json:"issues"`
}

// GitCommit is a source revision the user can deliberately package.  Releases
// are always built from one of these committed revisions, never from files
// currently lying in the working directory.
type GitCommit struct {
	SHA       string `json:"sha"`
	ShortSHA  string `json:"shortSha"`
	Subject   string `json:"subject"`
	CreatedAt string `json:"createdAt"`
}

type GitSourceBranch struct {
	Name    string      `json:"name"`
	Commits []GitCommit `json:"commits"`
}

// ReleaseMapping is stored inside every release commit as .albs-release.json.
// It makes it possible to answer exactly which source revision produced a
// published package even after the source branch has moved on.
type ReleaseMapping struct {
	Version       string `json:"version"`
	SourceBranch  string `json:"sourceBranch"`
	SourceCommit  string `json:"sourceCommit"`
	ReleaseCommit string `json:"releaseCommit,omitempty"`
}

type GitInitConfig struct {
	AuthorName  string `json:"authorName"`
	AuthorEmail string `json:"authorEmail"`
	Repository  string `json:"repository"`
	Message     string `json:"message"`
}

func GitReleaseStatus(root string) (GitStatus, error) {
	path, err := workspacePath(root)
	if err != nil {
		return GitStatus{}, err
	}
	status := GitStatus{Root: path, Checks: []string{}, Issues: []string{}, Tags: []string{}, Commits: []string{}, SourceCommits: []GitCommit{}, ReleaseMappings: []ReleaseMapping{}}
	pkg, err := readPackage(path)
	if err != nil {
		status.Issues = append(status.Issues, "当前目录缺少可用的 package.json，无法按应用包流程发布。")
		return status, nil
	}
	status.PackageName, _ = pkg["name"].(string)
	status.PackageVersion, _ = pkg["version"].(string)
	status.PackageManager = projectPackageManager(path)
	if _, ok := pkg["scripts"].(map[string]any); !ok {
		status.Issues = append(status.Issues, "package.json 没有 scripts，无法确认构建命令。")
	} else if scripts := pkg["scripts"].(map[string]any); scripts["build"] == nil {
		status.Issues = append(status.Issues, "package.json 没有 build 脚本，无法生成发布文件。")
	} else {
		status.Checks = append(status.Checks, "已找到构建脚本")
	}
	if output, err := gitRun(path, "rev-parse", "--is-inside-work-tree"); err != nil || output != "true" {
		status.Issues = append(status.Issues, "当前项目尚未初始化 Git。")
		return status, nil
	}
	// A parent repository must not silently become this project's release
	// repository. The UI can explicitly initialise an independent repository.
	status.GitReady = true
	gitRoot, err := gitRun(path, "rev-parse", "--show-toplevel")
	if err != nil || !sameWorkspacePath(path, gitRoot) {
		status.Issues = append(status.Issues, "所选目录不是 Git 仓库根目录；可在此目录初始化独立 Git 仓库。")
		return status, nil
	}
	if status.Repository, err = gitRun(path, "remote", "get-url", "origin"); err != nil {
		status.Issues = append(status.Issues, "未找到 origin 远程仓库。")
	} else {
		status.Checks = append(status.Checks, "已连接远程仓库")
		status.GitHubActionsURL = githubActionsURL(status.Repository)
	}
	if entries, err := os.ReadDir(filepath.Join(path, ".github", "workflows")); err == nil && len(entries) > 0 {
		status.WorkflowConfigured = true
		status.Checks = append(status.Checks, "已发现 GitHub 工作流")
	}
	status.Branch, _ = gitRun(path, "branch", "--show-current")
	// GitPublish creates detached worktrees from the selected commit.  The
	// caller's working tree is intentionally never read, cleaned, or switched,
	// so local edits (including generated lib files) must not block a release.
	status.Checks = append(status.Checks, "将从所选提交独立构建")
	if status.Repository != "" {
		inspectRemoteRelease(path, &status)
	}
	status.SourceBranches = sourceBranches(path)
	status.SourceCommits = sourceCommits(path)
	if len(status.SourceCommits) == 0 && status.GitReady {
		status.Issues = append(status.Issues, "当前分支还没有可选择的提交。")
	}
	if len(status.Tags) == 0 {
		status.Tags = gitLines(path, "tag", "--list", "v*", "--sort=-v:refname")
	}
	status.LatestVersion = latestGitVersionFromTags(status.Tags)
	status.SuggestedVersion = "v0.0.1"
	if status.LatestVersion != "" {
		status.SuggestedVersion = "v" + nextPatch(strings.TrimPrefix(status.LatestVersion, "v"))
	}
	return status, nil
}

func sourceBranches(root string) []GitSourceBranch {
	items := []GitSourceBranch{}
	seen := map[string]bool{}
	for _, name := range gitLines(root, "for-each-ref", "--format=%(refname:short)", "refs/heads") {
		commits := sourceCommitsAt(root, name)
		if len(commits) > 0 {
			items = append(items, GitSourceBranch{Name: name, Commits: commits})
			seen[name] = true
		}
	}
	for _, ref := range gitLines(root, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin") {
		name := strings.TrimPrefix(ref, "origin/")
		if name == "HEAD" || name == "" || seen[name] {
			continue
		}
		commits := sourceCommitsAt(root, ref)
		if len(commits) > 0 {
			items = append(items, GitSourceBranch{Name: name, Commits: commits})
			seen[name] = true
		}
	}
	return items
}

func sourceBranchRef(root, branch string) (string, error) {
	if _, err := gitRun(root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return "refs/heads/" + branch, nil
	}
	if _, err := gitRun(root, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
		return "refs/remotes/origin/" + branch, nil
	}
	return "", errors.New("所选源码分支不存在")
}

// RefreshGitSourceBranches updates origin tracking refs only. It never
// switches the caller's branch or merges code into the working directory.
func RefreshGitSourceBranches(root string) (GitStatus, error) {
	path, err := workspacePath(root)
	if err != nil {
		return GitStatus{}, err
	}
	if _, err := gitRun(path, "remote", "get-url", "origin"); err != nil {
		return GitStatus{}, errors.New("未找到 origin 远程仓库")
	}
	if output, err := gitRun(path, "fetch", "--prune", "origin"); err != nil {
		return GitStatus{}, fmt.Errorf("无法刷新远程分支：%s", output)
	}
	return GitReleaseStatus(path)
}

// inspectRemoteRelease reads the remote without updating local refs.  Status
// checks must not be able to fail merely because a local tracking ref is
// locked, stale, or absent.
func inspectRemoteRelease(path string, status *GitStatus) {
	output, err := gitRun(path, "ls-remote", "--symref", "origin", "HEAD", "refs/heads/main", "refs/heads/master", "refs/heads/release", "refs/tags/v*")
	if err != nil {
		status.RemoteAdvice = remoteAdvice(output)
		status.Issues = append(status.Issues, status.RemoteAdvice)
		return
	}
	status.RemoteReachable = true
	status.Checks = append(status.Checks, "已读取远程分支与版本标签")
	remoteHeads, remoteTags, defaultBranch := parseRemoteRefs(output)
	if defaultBranch == "" {
		// Some Git servers omit the symbolic HEAD response.  Prefer the common
		// defaults, then use the current branch when it is advertised remotely.
		for _, candidate := range []string{"main", "master", status.Branch} {
			if candidate != "" && remoteHeads[candidate] != "" {
				defaultBranch = candidate
				break
			}
		}
	}
	status.RemoteBranch = defaultBranch
	status.Tags = remoteTags
	if defaultBranch == "" {
		status.Issues = append(status.Issues, "远程仓库尚未设置默认分支；请先推送一个分支，并在仓库设置中设为默认分支。")
		return
	}
	if status.Branch == "" {
		status.Issues = append(status.Issues, "当前处于分离提交状态；请切换到远程默认分支 "+defaultBranch+"。")
	} else if status.Branch != defaultBranch {
		status.Checks = append(status.Checks, "当前分支 "+status.Branch+" 将发布到独立的 "+releaseBranchName(status.Branch, defaultBranch))
	} else {
		status.Checks = append(status.Checks, "当前处于远程默认分支 "+defaultBranch)
		if head, err := gitRun(path, "rev-parse", "HEAD"); err == nil && head == remoteHeads[defaultBranch] {
			status.Checks = append(status.Checks, defaultBranch+" 已与远程同步")
		} else {
			status.Issues = append(status.Issues, "本地 "+defaultBranch+" 与远程不同步；请先在 Git 管理中拉取或推送。")
		}
	}
	if remoteHeads["release"] != "" {
		status.ReleaseBranch = true
		status.Checks = append(status.Checks, "已找到远程 release 分支")
	}
}

func parseRemoteRefs(output string) (map[string]string, []string, string) {
	heads, tags, defaultBranch := map[string]string{}, []string{}, ""
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "ref:" && fields[len(fields)-1] == "HEAD" {
			defaultBranch = strings.TrimPrefix(fields[1], "refs/heads/")
			continue
		}
		ref := fields[1]
		if strings.HasPrefix(ref, "refs/heads/") {
			heads[strings.TrimPrefix(ref, "refs/heads/")] = fields[0]
		}
		if strings.HasPrefix(ref, "refs/tags/") {
			tag := strings.TrimPrefix(ref, "refs/tags/")
			if gitVersionPattern.MatchString(tag) {
				tags = append(tags, tag)
			}
		}
	}
	return heads, sortGitVersions(tags), defaultBranch
}

func sortGitVersions(tags []string) []string {
	sort.Slice(tags, func(left, right int) bool { return compareGitVersion(tags[left], tags[right]) > 0 })
	return tags
}

func compareGitVersion(left, right string) int {
	var l1, l2, l3, r1, r2, r3 int
	if _, err := fmt.Sscanf(strings.TrimPrefix(left, "v"), "%d.%d.%d", &l1, &l2, &l3); err != nil {
		return 0
	}
	if _, err := fmt.Sscanf(strings.TrimPrefix(right, "v"), "%d.%d.%d", &r1, &r2, &r3); err != nil {
		return 0
	}
	for _, pair := range [][2]int{{l1, r1}, {l2, r2}, {l3, r3}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

func remoteAdvice(output string) string {
	message := strings.ToLower(output)
	switch {
	case strings.Contains(message, "permission denied (publickey)"):
		return "无法通过 SSH 认证到 origin；请检查 SSH Key、ssh-agent 与仓库写入权限。"
	case strings.Contains(message, "authentication failed"), strings.Contains(message, "could not read username"), strings.Contains(message, "terminal prompts disabled"):
		return "origin 需要登录或访问令牌；请在 Git 管理中更新远程地址或完成 GitHub 登录。"
	case strings.Contains(message, "repository not found"):
		return "origin 仓库不存在，或当前账号没有访问权限；请检查远程地址与仓库权限。"
	case strings.Contains(message, "could not resolve host"), strings.Contains(message, "network is unreachable"), strings.Contains(message, "connection timed out"):
		return "无法连接远程仓库；请检查网络、代理或 DNS 后重新检查。"
	default:
		return "无法读取 origin 远程仓库；请在 Git 管理中检查远程地址和访问权限后重试。"
	}
}

// InitializeGit creates an independent repository in the selected project.
// It only changes identity in this repository, never the user's global Git
// configuration.
func InitializeGit(root string, config GitInitConfig) (Result, error) {
	path, err := workspacePath(root)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(config.AuthorName) == "" || strings.TrimSpace(config.AuthorEmail) == "" {
		return Result{}, errors.New("请填写 Git 提交姓名和邮箱")
	}
	if strings.ContainsAny(config.AuthorName+config.AuthorEmail+config.Repository+config.Message, "\r\n") {
		return Result{}, errors.New("Git 初始化信息不能包含换行")
	}
	if config.Repository != "" && !(strings.HasPrefix(config.Repository, "https://") || strings.HasPrefix(config.Repository, "git@") || strings.HasPrefix(config.Repository, "ssh://")) {
		return Result{}, errors.New("origin 地址应为 HTTPS、SSH 或 Git 地址")
	}
	if gitRoot, err := gitRun(path, "rev-parse", "--show-toplevel"); err == nil && sameWorkspacePath(path, gitRoot) {
		return Result{}, errors.New("当前目录已经是 Git 仓库根目录")
	}
	message := strings.TrimSpace(config.Message)
	if message == "" {
		message = "chore: initialize project"
	}
	logs := []string{}
	for _, command := range [][]string{{"init"}, {"branch", "-M", "main"}, {"config", "user.name", config.AuthorName}, {"config", "user.email", config.AuthorEmail}, {"add", "."}, {"commit", "-m", message}} {
		output, err := gitRun(path, command...)
		if output != "" {
			logs = append(logs, output)
		}
		if err != nil {
			return Result{Path: path, Output: strings.Join(logs, "\n")}, fmt.Errorf("Git 初始化失败：%w", err)
		}
	}
	if config.Repository != "" {
		output, err := gitRun(path, "remote", "add", "origin", config.Repository)
		if output != "" {
			logs = append(logs, output)
		}
		if err != nil {
			return Result{Path: path, Output: strings.Join(logs, "\n")}, fmt.Errorf("设置 origin 失败：%w", err)
		}
	}
	logs = append(logs, "Git 仓库已初始化。")
	return Result{Path: path, Output: strings.Join(logs, "\n")}, nil
}

func sameWorkspacePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if resolved, err := filepath.EvalSymlinks(left); err == nil {
		left = resolved
	}
	if resolved, err := filepath.EvalSymlinks(right); err == nil {
		right = resolved
	}
	return left == right
}

// GitPublish builds a selected committed source revision, puts only
// distributable files on release, and tags that release commit. It never cleans
// or switches the user's current worktree and never overwrites a remote branch
// or tag.
func GitPublish(root, version, sourceCommit string, confirmed bool) (Result, error) {
	return GitPublishWithOptions(root, version, "", sourceCommit, nil, confirmed)
}

func GitPublishWithOptions(root, version, sourceBranch, sourceCommit string, artifacts []string, confirmed bool) (Result, error) {
	path, err := workspacePath(root)
	if err != nil {
		return Result{}, err
	}
	status, err := GitReleaseStatus(path)
	if err != nil {
		return Result{}, err
	}
	if len(status.Issues) > 0 {
		return Result{}, errors.New("发布前检查未通过：" + strings.Join(status.Issues, "；"))
	}
	if sourceBranch == "" {
		sourceBranch = status.Branch
	}
	if !validGitRef(sourceBranch) {
		return Result{}, errors.New("请选择有效的源码分支")
	}
	if _, err := gitRun(path, "show-ref", "--verify", "--quiet", "refs/heads/"+sourceBranch); err != nil {
		return Result{}, errors.New("所选源码分支不存在，请刷新后重试")
	}
	if !regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`).MatchString(sourceCommit) {
		return Result{}, errors.New("请选择一个已提交的源码版本")
	}
	sourceCommit, err = gitRun(path, "rev-parse", "--verify", sourceCommit+"^{commit}")
	if err != nil {
		return Result{}, errors.New("所选源码提交不存在，请刷新后重新选择")
	}
	if _, err := gitRun(path, "merge-base", "--is-ancestor", sourceCommit, "refs/heads/"+sourceBranch); err != nil {
		return Result{}, errors.New("所选提交不属于所选源码分支，请刷新后重新选择")
	}
	if version == "" {
		version = status.SuggestedVersion
	} else {
		version = "v" + strings.TrimPrefix(version, "v")
	}
	if !gitVersionPattern.MatchString(version) {
		return Result{}, errors.New("版本号应为 v1.2.3 或 1.2.3")
	}
	if _, err := gitRun(path, "rev-parse", "-q", "--verify", "refs/tags/"+version); err == nil {
		return Result{}, fmt.Errorf("版本标签 %s 已存在，已发布版本不可覆盖", version)
	}
	if !confirmed {
		return Result{Path: path, Output: "检查通过：将从 " + shortGitSHA(sourceCommit) + " 构建 " + status.PackageName + "，把发布文件提交至 release，并创建标签 " + version}, errors.New("请确认后再开始 GIT 发布")
	}
	logs := []string{"已选择源码提交 " + shortGitSHA(sourceCommit), "准备独立构建目录"}
	sourceWorktree, err := os.MkdirTemp("", "albs-source-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(sourceWorktree)
	output, err := gitRun(path, "worktree", "add", "--detach", sourceWorktree, sourceCommit)
	if err != nil {
		return Result{Path: path, Output: strings.Join(append(logs, output), "\n")}, fmt.Errorf("无法创建源码构建目录：%w", err)
	}
	defer gitRun(path, "worktree", "remove", "--force", sourceWorktree)
	manager := projectPackageManager(sourceWorktree)
	logs = append(logs, "安装 "+manager+" 依赖")
	output, err = runPackageManager(sourceWorktree, "install")
	logs = append(logs, output)
	if err != nil {
		return Result{Path: path, Output: strings.Join(logs, "\n")}, buildDependencyError("安装构建依赖失败", strings.Join(logs, "\n"))
	}
	logs = append(logs, "开始构建 "+status.PackageName)
	output, err = runPackageManager(sourceWorktree, "run", "build")
	logs = append(logs, output)
	if err != nil {
		return Result{Path: path, Output: strings.Join(logs, "\n")}, buildDependencyError("构建失败", strings.Join(logs, "\n"))
	}
	if _, err := os.Stat(filepath.Join(sourceWorktree, "lib")); err != nil {
		return Result{Path: path, Output: strings.Join(logs, "\n")}, errors.New("构建结束后仍未找到 lib 目录，无法创建 Git 发布包")
	}
	worktree, err := os.MkdirTemp("", "albs-release-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(worktree)
	start := "HEAD"
	if status.ReleaseBranch {
		if output, err = gitRun(path, "fetch", "origin", "release"); err != nil {
			return Result{Path: path, Output: strings.Join(append(logs, output), "\n")}, fmt.Errorf("无法同步远程 release 分支：%w", err)
		}
		start = "origin/release"
	}
	if output, err = gitRun(path, "worktree", "add", "--detach", worktree, start); err != nil {
		return Result{Path: path, Output: strings.Join(append(logs, output), "\n")}, fmt.Errorf("无法创建安全的临时发布目录：%w", err)
	}
	defer gitRun(path, "worktree", "remove", "--force", worktree)
	if output, err = gitRun(worktree, "rm", "-rf", "."); err != nil {
		return Result{}, fmt.Errorf("无法准备 release 内容：%w", err)
	}
	if output, err = gitRun(worktree, "clean", "-fdx"); err != nil {
		return Result{}, fmt.Errorf("无法清理临时发布目录：%w", err)
	}
	if err := copyReleaseFiles(sourceWorktree, worktree, strings.TrimPrefix(version, "v"), artifacts); err != nil {
		return Result{}, err
	}
	mapping := ReleaseMapping{Version: version, SourceBranch: sourceBranch, SourceCommit: sourceCommit}
	mappingData, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(worktree, ".albs-release.json"), append(mappingData, '\n'), 0644); err != nil {
		return Result{}, err
	}
	if _, err := gitRun(worktree, "add", "-A"); err != nil {
		return Result{}, err
	}
	targetBranch := releaseBranchName(sourceBranch, status.RemoteBranch)
	commitMessage := "release: " + version + " (" + sourceBranch + "@" + shortGitSHA(sourceCommit) + ")"
	if output, err = gitRun(worktree, "commit", "-m", commitMessage); err != nil {
		return Result{}, fmt.Errorf("无法创建 release 提交（请先配置 Git 用户名和邮箱）：%w", err)
	}
	if output, err = gitRun(worktree, "push", "origin", "HEAD:refs/heads/"+targetBranch); err != nil {
		return Result{Path: path, Output: strings.Join(append(logs, output), "\n")}, fmt.Errorf("%s 分支推送失败：%w", targetBranch, err)
	}
	if output, err = gitRun(worktree, "tag", "-a", version, "-m", "Release "+version+"\n\nSource: "+status.Branch+"@"+sourceCommit); err != nil {
		return Result{}, fmt.Errorf("无法创建标签：%w", err)
	}
	if output, err = gitRun(worktree, "push", "origin", version); err != nil {
		_, _ = gitRun(worktree, "tag", "-d", version)
		return Result{Path: path, Output: strings.Join(append(logs, output), "\n")}, fmt.Errorf("标签推送失败：%w", err)
	}
	return Result{Path: path, Output: strings.Join(append(logs, "已发布 "+version+"："+targetBranch+" 分支、标签及源码映射已推送（"+sourceBranch+"@"+shortGitSHA(sourceCommit)+"）。"), "\n")}, nil
}

func shortGitSHA(value string) string {
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

func sourceCommits(root string) []GitCommit { return sourceCommitsAt(root, "HEAD") }
func sourceCommitsAt(root, ref string) []GitCommit {
	output, err := gitRun(root, "log", ref, "--format=%H%x1f%h%x1f%s%x1f%cs", "-20")
	if err != nil || output == "" {
		return []GitCommit{}
	}
	items := []GitCommit{}
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Split(line, "\x1f")
		if len(parts) != 4 {
			continue
		}
		items = append(items, GitCommit{SHA: parts[0], ShortSHA: parts[1], Subject: parts[2], CreatedAt: parts[3]})
	}
	return items
}

func releaseMappings(root, ref string) []ReleaseMapping {
	commits := gitLines(root, "log", ref, "--format=%H", "-8")
	mappings := []ReleaseMapping{}
	for _, commit := range commits {
		data, err := gitRun(root, "show", commit+":.albs-release.json")
		if err != nil {
			continue
		}
		var mapping ReleaseMapping
		if json.Unmarshal([]byte(data), &mapping) == nil && mapping.SourceCommit != "" {
			mapping.ReleaseCommit = commit
			mappings = append(mappings, mapping)
		}
	}
	return mappings
}

func copyReleaseFiles(source, destination, version string, artifacts []string) error {
	if len(artifacts) == 0 {
		artifacts = []string{"dist"}
	}
	for _, name := range artifacts {
		if !validReleaseArtifact(name) {
			return errors.New("发布产物路径无效：" + name)
		}
		if _, err := os.Stat(filepath.Join(source, name)); err == nil {
			if err := copyPath(filepath.Join(source, name), filepath.Join(destination, name)); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("构建后未找到所选发布产物：%s", name)
		}
	}
	pkg, err := readPackage(source)
	if err != nil {
		return err
	}
	for _, key := range []string{"devDependencies", "workspaces", "private", "scripts"} {
		delete(pkg, key)
	}
	pkg["version"] = version
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, "package.json"), append(data, '\n'), 0644)
}

func validReleaseArtifact(value string) bool {
	return value != "" && !filepath.IsAbs(value) && !strings.Contains(value, "..") && filepath.Clean(value) == value
}
func releaseBranchName(source, defaultBranch string) string {
	if source == defaultBranch {
		return "release"
	}
	return strings.ReplaceAll(strings.ReplaceAll(source, "/", "-"), " ", "-") + "-release"
}
func copyPath(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		return os.WriteFile(destination, data, info.Mode())
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		target := filepath.Join(destination, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
func readPackage(root string) (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil, err
	}
	var pkg map[string]any
	err = json.Unmarshal(data, &pkg)
	return pkg, err
}
func projectPackageManager(root string) string {
	var manifest struct {
		PackageManager string `json:"packageManager"`
	}
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil && json.Unmarshal(data, &manifest) == nil {
		manager := strings.ToLower(strings.Split(strings.TrimSpace(manifest.PackageManager), "@")[0])
		if manager == "npm" || manager == "yarn" || manager == "pnpm" {
			return manager
		}
	}
	if _, err := os.Stat(filepath.Join(root, "yarn.lock")); err == nil {
		return "yarn"
	}
	if _, err := os.Stat(filepath.Join(root, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	return "npm"
}

func githubActionsURL(remote string) string {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	remote = strings.TrimPrefix(remote, "git@github.com:")
	remote = strings.TrimPrefix(remote, "ssh://git@github.com/")
	remote = strings.TrimPrefix(remote, "https://github.com/")
	remote = strings.TrimPrefix(remote, "http://github.com/")
	if strings.Count(remote, "/") != 1 || strings.Contains(remote, "://") {
		return ""
	}
	return "https://github.com/" + remote + "/actions"
}
func workspacePath(root string) (string, error) {
	if root == "." {
		return os.Getwd()
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("请选择完整的项目目录")
	}
	info, err := os.Stat(root)
	if err != nil {
		if permissionError(err) {
			return "", fmt.Errorf("无法访问项目目录：%w", err)
		}
		return "", errors.New("项目目录不存在")
	}
	if !info.IsDir() {
		return "", errors.New("项目目录不存在")
	}
	return root, nil
}
func gitRun(root string, args ...string) (string, error) { return run(root, "git", args...) }
func nextGitVersion(root string) string {
	latest := latestGitVersion(root)
	if latest == "" {
		return "v0.0.1"
	}
	return "v" + nextPatch(strings.TrimPrefix(latest, "v"))
}
func latestGitVersion(root string) string {
	tags, err := gitRun(root, "tag", "--list", "v*", "--sort=-v:refname")
	if err != nil {
		return ""
	}
	return latestGitVersionFromTags(strings.Split(tags, "\n"))
}

func latestGitVersionFromTags(tags []string) string {
	for _, value := range tags {
		if gitVersionPattern.MatchString(value) {
			return value
		}
	}
	return ""
}
func gitLines(root string, args ...string) []string {
	output, err := gitRun(root, args...)
	if err != nil || output == "" {
		return []string{}
	}
	return strings.Split(output, "\n")
}

package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var gitVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// GitStatus describes the package-release workflow.  A Git release here means
// publishing the built Node package to the project's release branch, not
// building alemonjs-setup itself.
type GitStatus struct {
	Repository         string   `json:"repository,omitempty"`
	Branch             string   `json:"branch,omitempty"`
	PackageName        string   `json:"packageName,omitempty"`
	PackageVersion     string   `json:"packageVersion,omitempty"`
	PackageManager     string   `json:"packageManager,omitempty"`
	GitHubActionsURL   string   `json:"gitHubActionsUrl,omitempty"`
	WorkflowConfigured bool     `json:"workflowConfigured"`
	GitReady           bool     `json:"gitReady"`
	ReleaseBranch      bool     `json:"releaseBranch"`
	LatestVersion      string   `json:"latestVersion,omitempty"`
	SuggestedVersion   string   `json:"suggestedVersion,omitempty"`
	Tags               []string `json:"tags"`
	Commits            []string `json:"commits"`
	Artifacts          []string `json:"artifacts"`
	Checks             []string `json:"checks"`
	Issues             []string `json:"issues"`
}

func GitReleaseStatus(root string) (GitStatus, error) {
	path, err := workspacePath(root)
	if err != nil {
		return GitStatus{}, err
	}
	status := GitStatus{Checks: []string{}, Issues: []string{}, Tags: []string{}, Commits: []string{}, Artifacts: []string{}}
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
	status.GitReady = true
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
	if status.Branch != "main" {
		status.Issues = append(status.Issues, "当前分支是 "+status.Branch+"，请切换到 main。")
	} else {
		status.Checks = append(status.Checks, "当前处于 main 分支")
	}
	if dirty, _ := gitRun(path, "status", "--porcelain"); dirty != "" {
		status.Issues = append(status.Issues, "工作区有未提交修改。")
	} else {
		status.Checks = append(status.Checks, "工作区干净")
	}
	if status.Repository != "" {
		if _, err := gitRun(path, "fetch", "origin", "main", "--tags"); err != nil {
			status.Issues = append(status.Issues, "无法读取远程 main 与版本标签。")
		} else if head, _ := gitRun(path, "rev-parse", "HEAD"); func() bool {
			remote, err := gitRun(path, "rev-parse", "origin/main")
			return err == nil && head == remote
		}() {
			status.Checks = append(status.Checks, "main 已与远程同步")
		} else {
			status.Issues = append(status.Issues, "本地 main 与 origin/main 不同步。")
		}
		if _, err := gitRun(path, "ls-remote", "--exit-code", "--heads", "origin", "release"); err == nil {
			status.ReleaseBranch = true
			status.Checks = append(status.Checks, "已找到 release 分支")
		}
	}
	for _, item := range []struct{ path, label string }{{"lib", "lib（构建产物）"}, {"README.md", "README.md"}, {".puppeteerrc.cjs", ".puppeteerrc.cjs"}} {
		if _, err := os.Stat(filepath.Join(path, item.path)); err == nil {
			status.Artifacts = append(status.Artifacts, item.label)
		}
	}
	if _, err := os.Stat(filepath.Join(path, "lib")); err != nil {
		status.Issues = append(status.Issues, "尚未发现 lib 构建产物；发布时会先执行 build。")
	}
	status.Tags, status.Commits = gitLines(path, "tag", "--list", "v*", "--sort=-v:refname"), gitLines(path, "log", "--oneline", "-8")
	status.LatestVersion = latestGitVersion(path)
	status.SuggestedVersion = "v0.0.1"
	if status.LatestVersion != "" {
		status.SuggestedVersion = "v" + nextPatch(strings.TrimPrefix(status.LatestVersion, "v"))
	}
	return status, nil
}

// GitPublish builds the current Node project, puts only distributable files on
// release, and tags that release commit. It never cleans or switches the user's
// current worktree and never overwrites a remote branch or tag.
func GitPublish(root, version string, confirmed bool) (Result, error) {
	path, err := workspacePath(root)
	if err != nil {
		return Result{}, err
	}
	status, err := GitReleaseStatus(path)
	if err != nil {
		return Result{}, err
	}
	issues := make([]string, 0, len(status.Issues))
	for _, issue := range status.Issues {
		if !strings.HasPrefix(issue, "尚未发现 lib") {
			issues = append(issues, issue)
		}
	}
	if len(issues) > 0 {
		return Result{}, errors.New("发布前检查未通过：" + strings.Join(issues, "；"))
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
		return Result{Path: path, Output: "检查通过：将构建 " + status.PackageName + "，把发布文件提交至 release，并创建标签 " + version}, errors.New("请确认后再开始 Git 打包")
	}
	manager := projectPackageManager(path)
	logs := []string{"开始构建 " + status.PackageName}
	output, err := run(path, manager, "run", "build")
	logs = append(logs, output)
	if err != nil {
		return Result{Path: path, Output: strings.Join(logs, "\n")}, fmt.Errorf("构建失败：%w", err)
	}
	if _, err := os.Stat(filepath.Join(path, "lib")); err != nil {
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
	if err := copyReleaseFiles(path, worktree, strings.TrimPrefix(version, "v")); err != nil {
		return Result{}, err
	}
	if _, err := gitRun(worktree, "add", "-A"); err != nil {
		return Result{}, err
	}
	if output, err = gitRun(worktree, "commit", "-m", version); err != nil {
		return Result{}, fmt.Errorf("无法创建 release 提交（请先配置 Git 用户名和邮箱）：%w", err)
	}
	if output, err = gitRun(worktree, "push", "origin", "HEAD:refs/heads/release"); err != nil {
		return Result{Path: path, Output: strings.Join(append(logs, output), "\n")}, fmt.Errorf("release 分支推送失败：%w", err)
	}
	if output, err = gitRun(worktree, "tag", "-a", version, "-m", "Release "+version); err != nil {
		return Result{}, fmt.Errorf("无法创建标签：%w", err)
	}
	if output, err = gitRun(worktree, "push", "origin", version); err != nil {
		_, _ = gitRun(worktree, "tag", "-d", version)
		return Result{Path: path, Output: strings.Join(append(logs, output), "\n")}, fmt.Errorf("标签推送失败：%w", err)
	}
	return Result{Path: path, Output: strings.Join(append(logs, "已发布 "+version+"：release 分支已更新，Git 标签已推送。"), "\n")}, nil
}

func copyReleaseFiles(source, destination, version string) error {
	for _, name := range []string{"lib", "README.md", ".puppeteerrc.cjs"} {
		if _, err := os.Stat(filepath.Join(source, name)); err == nil {
			if err := copyPath(filepath.Join(source, name), filepath.Join(destination, name)); err != nil {
				return err
			}
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
	if err != nil || !info.IsDir() {
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
	for _, value := range strings.Split(tags, "\n") {
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

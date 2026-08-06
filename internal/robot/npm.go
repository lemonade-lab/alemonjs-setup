package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const npmRegistry = "https://registry.npmjs.org"

// NPMStatus is the information needed to safely guide a package publication.
// It never contains credentials or raw npm command output.
type NPMStatus struct {
	Name             string      `json:"name"`
	LocalVersion     string      `json:"localVersion"`
	LatestVersion    string      `json:"latestVersion,omitempty"`
	LatestPublished  string      `json:"latestPublished,omitempty"`
	Published        bool        `json:"published"`
	Private          bool        `json:"private"`
	LoggedIn         bool        `json:"loggedIn"`
	Username         string      `json:"username,omitempty"`
	SuggestedVersion string      `json:"suggestedVersion,omitempty"`
	Scripts          []string    `json:"scripts"`
	Branch           string      `json:"branch,omitempty"`
	GitReady         bool        `json:"gitReady"`
	SourceCommits    []GitCommit `json:"sourceCommits"`
	Issues           []string    `json:"issues"`
}

// NPMPackPreview is produced by npm itself, so the user sees the exact files
// that would be uploaded before credentials are used for publishing.
type NPMPackPreview struct {
	Name         string   `json:"name,omitempty"`
	Version      string   `json:"version,omitempty"`
	Filename     string   `json:"filename,omitempty"`
	FileCount    int      `json:"fileCount"`
	UnpackedSize int64    `json:"unpackedSize"`
	Files        []string `json:"files"`
}

type packageManifest struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Private bool              `json:"private"`
	Scripts map[string]string `json:"scripts"`
}

func (Manager) NPMStatus(root string) (NPMStatus, error) {
	path, err := projectPath(root)
	if err != nil {
		return NPMStatus{}, err
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return NPMStatus{}, fmt.Errorf("无法读取 package.json：%w", err)
	}
	var manifest packageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return NPMStatus{}, errors.New("package.json 格式无法识别，请先修正后再发布")
	}
	status := NPMStatus{Name: manifest.Name, LocalVersion: manifest.Version, Private: manifest.Private, Scripts: []string{}, SourceCommits: []GitCommit{}, Issues: []string{}}
	for _, name := range []string{"prepublishOnly", "prepack", "prepare", "build"} {
		if command := strings.TrimSpace(manifest.Scripts[name]); command != "" {
			status.Scripts = append(status.Scripts, name+": "+command)
		}
	}
	if status.Name == "" {
		status.Issues = append(status.Issues, "package.json 缺少包名，无法发布到 npm。")
	}
	if !isNpmVersion(status.LocalVersion) {
		status.Issues = append(status.Issues, "本地版本号应为 1.2.3 这样的格式。")
	}
	if status.Private {
		status.Issues = append(status.Issues, "该项目标记为 private，npm 不允许发布。")
	}
	if status.Name != "" {
		latest, publishedAt, found, err := npmPackage(status.Name)
		if err != nil {
			status.Issues = append(status.Issues, "暂时无法连接 npm 官方仓库，发布前请重新检查。")
		} else if found {
			status.Published, status.LatestVersion, status.LatestPublished = true, latest, publishedAt
			if isNpmVersion(status.LocalVersion) && compareSemver(status.LocalVersion, latest) <= 0 {
				status.Issues = append(status.Issues, "本地版本必须高于 npm 当前最新版 "+latest+"。")
			}
			status.SuggestedVersion = nextPatch(latest)
		} else {
			status.SuggestedVersion = status.LocalVersion
		}
	}
	if username := npmWhoami(path); username != "" {
		status.LoggedIn, status.Username = true, username
	} else {
		status.Issues = append(status.Issues, "尚未登录 npm；请先完成登录或配置发布令牌。")
	}
	if output, err := gitRun(path, "rev-parse", "--is-inside-work-tree"); err != nil || output != "true" {
		status.Issues = append(status.Issues, "当前项目尚未初始化 Git，无法确认要发布的源码提交。")
		return status, nil
	}
	status.GitReady = true
	gitRoot, err := gitRun(path, "rev-parse", "--show-toplevel")
	if err != nil || !sameWorkspacePath(path, gitRoot) {
		status.Issues = append(status.Issues, "所选目录不是 Git 仓库根目录，无法确认要发布的源码提交。")
		return status, nil
	}
	status.Branch, _ = gitRun(path, "branch", "--show-current")
	if dirty, _ := gitRun(path, "status", "--porcelain"); dirty != "" {
		status.Issues = append(status.Issues, "工作区有未提交修改；请先提交或暂存后再发布。")
	}
	status.SourceCommits = sourceCommits(path)
	if len(status.SourceCommits) == 0 {
		status.Issues = append(status.Issues, "当前分支还没有可选择的提交。")
	}
	return status, nil
}

func (Manager) NPMPackPreview(root string) (NPMPackPreview, error) {
	path, err := projectPath(root)
	if err != nil {
		return NPMPackPreview{}, err
	}
	return npmPackPreview(path)
}

// NPMPackPreviewAtCommit previews the same committed files that will be sent
// to npm, without running package lifecycle scripts or changing the project.
func (Manager) NPMPackPreviewAtCommit(root, sourceCommit string) (NPMPackPreview, error) {
	path, err := projectPath(root)
	if err != nil {
		return NPMPackPreview{}, err
	}
	if !regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`).MatchString(sourceCommit) {
		return NPMPackPreview{}, errors.New("请选择一个已提交的源码版本")
	}
	sourceCommit, err = gitRun(path, "rev-parse", "--verify", sourceCommit+"^{commit}")
	if err != nil {
		return NPMPackPreview{}, errors.New("所选源码提交不存在，请刷新后重新选择")
	}
	worktree, err := os.MkdirTemp("", "alx-npm-preview-")
	if err != nil {
		return NPMPackPreview{}, err
	}
	defer os.RemoveAll(worktree)
	if _, err := gitRun(path, "worktree", "add", "--detach", worktree, sourceCommit); err != nil {
		return NPMPackPreview{}, fmt.Errorf("无法创建源码预览目录：%w", err)
	}
	defer gitRun(path, "worktree", "remove", "--force", worktree)
	return npmPackPreview(worktree)
}

func npmPackPreview(path string) (NPMPackPreview, error) {
	// --ignore-scripts keeps this preview side-effect free. The actual npm
	// publish still runs the package lifecycle scripts and is shown separately.
	output, err := run(path, "npm", "pack", "--dry-run", "--json", "--ignore-scripts")
	if err != nil {
		return NPMPackPreview{}, fmt.Errorf("无法生成 npm 打包预览：%w", err)
	}
	var items []struct {
		Name         string `json:"name"`
		Version      string `json:"version"`
		Filename     string `json:"filename"`
		UnpackedSize int64  `json:"unpackedSize"`
		Files        []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(output), &items); err != nil || len(items) == 0 {
		return NPMPackPreview{}, errors.New("npm 未返回可识别的打包预览")
	}
	preview := NPMPackPreview{Name: items[0].Name, Version: items[0].Version, Filename: items[0].Filename, UnpackedSize: items[0].UnpackedSize, Files: []string{}}
	for _, item := range items[0].Files {
		preview.Files = append(preview.Files, item.Path)
	}
	preview.FileCount = len(preview.Files)
	return preview, nil
}

// NPMPublish publishes an archive produced from the selected committed source
// revision. This prevents a local uncommitted file from accidentally becoming
// part of a public npm package.
func (Manager) NPMPublish(root, sourceCommit, tag, token string) (Result, error) {
	path, err := projectPath(root)
	if err != nil {
		return Result{}, err
	}
	status, err := (Manager{}).NPMStatus(path)
	if err != nil {
		return Result{}, err
	}
	issues := make([]string, 0, len(status.Issues))
	for _, issue := range status.Issues {
		if token != "" && strings.HasPrefix(issue, "尚未登录 npm") {
			continue
		}
		issues = append(issues, issue)
	}
	if len(issues) > 0 {
		return Result{}, errors.New("发布前检查未通过：" + strings.Join(issues, "；"))
	}
	if !regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`).MatchString(sourceCommit) {
		return Result{}, errors.New("请选择一个已提交的源码版本")
	}
	sourceCommit, err = gitRun(path, "rev-parse", "--verify", sourceCommit+"^{commit}")
	if err != nil {
		return Result{}, errors.New("所选源码提交不存在，请刷新后重新选择")
	}
	if _, err := gitRun(path, "merge-base", "--is-ancestor", sourceCommit, "HEAD"); err != nil {
		return Result{}, errors.New("所选提交不属于当前源码分支，请刷新后重新选择")
	}
	worktree, err := os.MkdirTemp("", "alx-npm-source-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(worktree)
	output, err := gitRun(path, "worktree", "add", "--detach", worktree, sourceCommit)
	if err != nil {
		return Result{Path: path, Output: output}, fmt.Errorf("无法创建源码打包目录：%w", err)
	}
	defer gitRun(path, "worktree", "remove", "--force", worktree)
	output, err = run(worktree, "npm", "pack", "--json")
	if err != nil {
		return Result{Path: path, Output: output}, fmt.Errorf("从所选提交打包失败：%w", err)
	}
	var items []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal([]byte(output), &items); err != nil || len(items) == 0 || items[0].Filename == "" {
		return Result{Path: path, Output: output}, errors.New("npm 未返回可发布的打包文件")
	}
	archive := filepath.Join(worktree, items[0].Filename)
	if token != "" {
		output, err = publishWithToken(worktree, archive, tag, token)
	} else {
		output, err = run(worktree, "npm", "publish", archive, "--tag", tag, "--registry="+npmRegistry)
	}
	if err != nil {
		return Result{Path: path, Output: output}, fmt.Errorf("npm 发布失败：%w", err)
	}
	return Result{Path: path, Output: "已从 " + status.Branch + "@" + shortGitSHA(sourceCommit) + " 打包并发布到 npm。\n" + output}, nil
}

func npmPackage(name string) (latest, publishedAt string, found bool, err error) {
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(npmRegistry + "/" + url.PathEscape(name))
	if err != nil {
		return "", "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", "", false, nil
	}
	if response.StatusCode != http.StatusOK {
		return "", "", false, fmt.Errorf("npm registry returned %s", response.Status)
	}
	var payload struct {
		DistTags map[string]string `json:"dist-tags"`
		Time     map[string]string `json:"time"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", "", false, err
	}
	latest = payload.DistTags["latest"]
	if latest == "" {
		return "", "", false, nil
	}
	return latest, payload.Time[latest], true, nil
}

func npmWhoami(root string) string {
	cmd := exec.Command("npm", "whoami", "--registry="+npmRegistry)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func publishWithToken(root, target, tag, token string) (string, error) {
	if strings.ContainsAny(token, "\r\n") {
		return "", errors.New("npm 令牌格式无效")
	}
	directory, err := os.MkdirTemp("", "alemonx-npm-")
	if err != nil {
		return "", fmt.Errorf("无法创建临时发布配置：%w", err)
	}
	defer os.RemoveAll(directory)
	config := filepath.Join(directory, ".npmrc")
	content := "registry=" + npmRegistry + "/\n//registry.npmjs.org/:_authToken=" + token + "\n"
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("无法准备临时发布配置：%w", err)
	}
	args := []string{"publish"}
	if target != "" {
		args = append(args, target)
	}
	args = append(args, "--tag", tag, "--registry="+npmRegistry)
	return runWithEnv(root, map[string]string{"NPM_CONFIG_USERCONFIG": config}, "npm", args...)
}

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func isNpmVersion(version string) bool { return versionPattern.MatchString(version) }

func nextPatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return ""
	}
	var major, minor, patch int
	if _, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

func compareSemver(left, right string) int {
	// Inputs are bare x.y.z npm versions; semver.Compare requires the v prefix.
	return semver.Compare("v"+left, "v"+right)
}

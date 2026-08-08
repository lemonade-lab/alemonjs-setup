package robot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"alemonx/internal/system"
)

var gitBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var cloneDirectoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var gitCloneProgressPattern = regexp.MustCompile(`(?i)(compressing objects|receiving objects|resolving deltas):\s*(\d+)%`)

type CloneTarget struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// CloneProgress is a progress update reported by Git itself while cloning.
type CloneProgress struct {
	Percent int
	Detail  string
}

func cloneRepositoryURL(repository string) (*url.URL, string, error) {
	value := strings.TrimSpace(repository)
	if match := regexp.MustCompile(`^git@(github\.com|gitee\.com):([^/]+)/([^/]+?)(?:\.git)?$`).FindStringSubmatch(value); len(match) == 4 {
		name := strings.TrimSuffix(match[3], ".git")
		return &url.URL{Scheme: "ssh", Host: match[1], Path: "/" + match[2] + "/" + name}, name, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "ssh") || (parsed.Host != "github.com" && parsed.Host != "gitee.com") {
		return nil, "", errors.New("请填写完整的 GitHub 或 Gitee 仓库地址")
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, "", errors.New("仓库地址应为 https://github.com/组织/仓库")
	}
	return parsed, parts[1], nil
}

// ValidateCloneRepository checks the URL format and local SSH prerequisite
// before a clone task is queued.
func ValidateCloneRepository(repository string) error {
	parsed, _, err := cloneRepositoryURL(repository)
	if err != nil {
		return err
	}
	return requireSSHKey(parsed)
}

// CloneBranches silently reads remote heads for a completed repository URL.
// It is read-only and does not create any local directory.
func CloneBranches(repository string) ([]string, string, error) {
	parsed, _, err := cloneRepositoryURL(repository)
	if err != nil {
		return nil, "", err
	}
	if err := requireSSHKey(parsed); err != nil {
		return nil, "", err
	}
	remote := strings.TrimSpace(repository)
	if parsed.Scheme == "ssh" && strings.HasPrefix(remote, "git@") {
		// Keep the scp-style SSH URL exactly as entered.
	} else if parsed.Scheme == "ssh" {
		remote = parsed.String()
	}
	output, err := run(os.TempDir(), "git", "ls-remote", "--heads", remote)
	if err != nil {
		return nil, "", fmt.Errorf("无法读取远程分支：%w", err)
	}
	branches, defaultBranch := []string{}, ""
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[1], "refs/heads/") {
			branches = append(branches, strings.TrimPrefix(fields[1], "refs/heads/"))
		}
	}
	sort.Strings(branches)
	if head, err := run(os.TempDir(), "git", "ls-remote", "--symref", remote, "HEAD"); err == nil {
		for _, line := range strings.Split(head, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "ref:" && fields[len(fields)-1] == "HEAD" {
				defaultBranch = strings.TrimPrefix(fields[1], "refs/heads/")
				break
			}
		}
	}
	if defaultBranch == "" && len(branches) > 0 {
		defaultBranch = branches[0]
	}
	return branches, defaultBranch, nil
}

func CloneDestination(destination, repository, name string) (CloneTarget, error) {
	_, defaultName, err := cloneRepositoryURL(repository)
	if err != nil {
		return CloneTarget{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultName
	}
	if !cloneDirectoryPattern.MatchString(name) {
		return CloneTarget{}, errors.New("最终目录名只能包含字母、数字、点、下划线或短横线")
	}
	target := filepath.Join(destination, name)
	if _, err := os.Stat(target); err == nil {
		return CloneTarget{Path: target, Exists: true}, nil
	} else if !os.IsNotExist(err) {
		return CloneTarget{}, fmt.Errorf("无法检查目标目录：%w", err)
	}
	return CloneTarget{Path: target}, nil
}

// CloneRepository clones a remote robot repository into an existing parent
// directory. The destination name and mirror are validated from fixed choices.
// depth limits how much history is downloaded; 0 means the full repository.
func CloneRepository(destination, repository, branch, name, mirror string, depth int) (Result, error) {
	return CloneRepositoryWithProgress(destination, repository, branch, name, mirror, depth, nil)
}

// CloneRepositoryWithProgress clones a repository and forwards Git's live
// --progress output. The displayed total is weighted across Git's own phases:
// object compression (0–5%), receiving objects (5–90%), and resolving deltas
// (90–99%). 100% is only emitted after Git exits successfully.
func CloneRepositoryWithProgress(destination, repository, branch, name, mirror string, depth int, onProgress func(CloneProgress)) (Result, error) {
	parsed, _, err := cloneRepositoryURL(repository)
	if err != nil {
		return Result{}, err
	}
	if err := requireSSHKey(parsed); err != nil {
		return Result{}, err
	}
	branch = strings.TrimSpace(branch)
	if branch != "" && (!gitBranchPattern.MatchString(branch) || strings.Contains(branch, "..") || strings.HasPrefix(branch, "-")) {
		return Result{}, errors.New("Git 分支或 tag 无效")
	}
	target, err := CloneDestination(destination, repository, name)
	if err != nil {
		return Result{}, err
	}
	if target.Exists {
		return Result{}, fmt.Errorf("目标目录 %s 已存在", filepath.Base(target.Path))
	}
	remote := strings.TrimSpace(repository)
	if parsed.Scheme == "https" {
		remote = parsed.String()
	}
	switch mirror {
	case "", "official":
	case "gh-proxy":
		if parsed.Host != "github.com" {
			return Result{}, errors.New("该镜像仅支持 GitHub 仓库")
		}
		remote = "https://gh-proxy.com/" + remote
	case "ghproxy-net":
		if parsed.Host != "github.com" {
			return Result{}, errors.New("该镜像仅支持 GitHub 仓库")
		}
		remote = "https://ghproxy.net/" + remote
	default:
		return Result{}, errors.New("不支持的 Git 镜像")
	}
	args := []string{}
	if parsed.Scheme == "https" && (mirror == "" || mirror == "official") {
		// A user-level url.*.insteadOf rule can silently redirect even an
		// explicitly selected “official” source to an unavailable mirror. Give
		// this exact repository URL a higher-precedence no-op rewrite for this
		// command only. It preserves credential helpers and other Git settings.
		args = append(args, "-c", "url."+remote+".insteadOf="+remote)
	}
	args = append(args, "clone", "--progress")
	if depth > 0 {
		args = append(args, "--depth", strconv.Itoa(depth))
	}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, remote, target.Path)
	output, err := runCloneWithProgress(destination, onProgress, args...)
	if err != nil {
		return Result{Path: target.Path, Output: output}, fmt.Errorf("克隆仓库失败：%w", err)
	}
	warnings := []string{}
	if parsed.Scheme == "https" && (mirror == "" || mirror == "official") {
		// Keep later fetch/pull operations on the selected official source too;
		// this repository-local setting overrides a less-specific global mirror.
		if _, err := gitRun(target.Path, "config", "url."+remote+".insteadOf", remote); err != nil {
			warnings = append(warnings, "未能保存官网来源设置："+err.Error())
		}
	}
	// git clone --branch narrows remote.origin.fetch to that single branch, so
	// later fetches would never learn about the other remote branches. Restore
	// the full refspec so the whole repository becomes visible after fetch. This
	// is a follow-up convenience only: a successful clone must remain usable if
	// Git cannot update this optional configuration (for example, a filesystem
	// lock held by a security tool on Windows).
	if _, err := gitRun(target.Path, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		warnings = append(warnings, "未能恢复全部远程分支跟踪；稍后可在 Git 管理中执行拉取："+err.Error())
	}
	if len(warnings) > 0 {
		output = strings.TrimSpace(output + "\n提示：仓库已克隆。\n" + strings.Join(warnings, "\n"))
	}
	return Result{Path: target.Path, Output: "已克隆到 " + target.Path + "。\n" + output}, nil
}

func cloneProgressFromGitOutput(value string) (CloneProgress, bool) {
	match := gitCloneProgressPattern.FindStringSubmatch(value)
	if len(match) != 3 {
		return CloneProgress{}, false
	}
	phase := strings.ToLower(match[1])
	percent, err := strconv.Atoi(match[2])
	if err != nil {
		return CloneProgress{}, false
	}
	percent = max(0, min(100, percent))
	switch phase {
	case "compressing objects":
		return CloneProgress{Percent: percent * 5 / 100, Detail: "正在压缩对象…"}, true
	case "receiving objects":
		return CloneProgress{Percent: 5 + percent*85/100, Detail: fmt.Sprintf("正在接收对象（%d%%）…", percent)}, true
	case "resolving deltas":
		return CloneProgress{Percent: 90 + percent*9/100, Detail: fmt.Sprintf("正在解析增量（%d%%）…", percent)}, true
	default:
		return CloneProgress{}, false
	}
}

func runCloneWithProgress(root string, onProgress func(CloneProgress), args ...string) (string, error) {
	// Clone-specific temporary Git config is placed before the clone subcommand,
	// so infer the timeout from the operation rather than args[0].
	timeout := commandTimeout("git", "clone")
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = root
	HideWindow(command)
	var output bytes.Buffer
	progressOutput := &cloneProgressWriter{output: &output, onProgress: onProgress}
	command.Stdout = progressOutput
	command.Stderr = progressOutput
	err := command.Run()
	text := strings.TrimSpace(output.String())
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return text, fmt.Errorf("操作超时（%s）；请检查网络、登录状态或代理后重试", timeout.Round(time.Second))
	}
	if err != nil {
		if permissionError(err) || permissionError(errors.New(text)) {
			return text, permissionAdvice("执行 git")
		}
		if commandNotFound(err, text) {
			return text, missingCommandAdvice("git")
		}
		if text != "" {
			return text, fmt.Errorf("%s：%w", text, err)
		}
		return text, fmt.Errorf("执行 git 失败：%w", err)
	}
	return text, nil
}

type cloneProgressWriter struct {
	output     *bytes.Buffer
	onProgress func(CloneProgress)
	last       CloneProgress
	pending    string
	mu         sync.Mutex
}

func (w *cloneProgressWriter) Write(chunk []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.output.Write(chunk)
	if w.onProgress == nil {
		return len(chunk), nil
	}
	w.pending += string(chunk)
	lastBreak := strings.LastIndexAny(w.pending, "\r\n")
	if lastBreak < 0 {
		return len(chunk), nil
	}
	values, rest := w.pending[:lastBreak], w.pending[lastBreak+1:]
	w.pending = rest
	for _, value := range strings.FieldsFunc(values, func(r rune) bool { return r == '\r' || r == '\n' }) {
		progress, ok := cloneProgressFromGitOutput(value)
		if ok && progress != w.last {
			w.last = progress
			w.onProgress(progress)
		}
	}
	return len(chunk), nil
}

func requireSSHKey(repository *url.URL) error {
	if repository.Scheme != "ssh" {
		return nil
	}
	keys, err := system.SSHKeys()
	if err != nil {
		return fmt.Errorf("无法检查 SSH 配置：%w", err)
	}
	if len(keys) == 0 {
		return errors.New("未配置 SSH 公钥，无法使用 SSH 地址克隆。请在顶部“SSH 管理”中生成密钥、将公钥添加到代码平台，或改用 HTTPS 地址")
	}
	return nil
}

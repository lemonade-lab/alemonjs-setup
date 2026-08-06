package robot

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"alemonjs-setup/internal/system"
)

var gitBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var cloneDirectoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type CloneTarget struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
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
func CloneRepository(destination, repository, branch, name, mirror string) (Result, error) {
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
	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, remote, target.Path)
	output, err := run(destination, "git", args...)
	if err != nil {
		return Result{Path: target.Path, Output: output}, fmt.Errorf("克隆仓库失败：%w", err)
	}
	return Result{Path: target.Path, Output: "已克隆到 " + target.Path + "。\n" + output}, nil
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

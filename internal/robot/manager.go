// Package robot provides guarded local project management operations.
package robot

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Manager struct{}
type Result struct {
	Output string `json:"output"`
	Path   string `json:"path"`
}

func (Manager) Read(root, name string) (Result, error) {
	path, err := file(root, name)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("读取失败：%w", err)
	}
	return Result{Path: path, Output: string(data)}, nil
}
func (Manager) Write(root, name, content string) (Result, error) {
	path, err := file(root, name)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Output: "已保存。"}, nil
}
func (Manager) Run(root, action, message, packageName, version, tag, token string, confirmed bool) (Result, error) {
	if action == "git-release" {
		return GitPublish(root, version, confirmed)
	}
	if err := project(root); err != nil {
		return Result{}, err
	}
	manager := "npm"
	if _, err := os.Stat(filepath.Join(root, "yarn.lock")); err == nil {
		manager = "yarn"
	}
	var name string
	var args []string
	switch action {
	case "dependency-status":
		checks := []string{}
		if _, err := os.Stat(filepath.Join(root, "node_modules")); err == nil {
			checks = append(checks, "已发现 node_modules，依赖目录已就绪。")
		} else {
			checks = append(checks, "未发现 node_modules，需要安装依赖。")
		}
		if _, err := os.Stat(filepath.Join(root, "yarn.lock")); err == nil {
			checks = append(checks, "检测到 yarn.lock，将使用 Yarn 管理依赖。")
		} else if _, err := os.Stat(filepath.Join(root, "package-lock.json")); err == nil {
			checks = append(checks, "检测到 package-lock.json，将使用 npm 管理依赖。")
		} else {
			checks = append(checks, "未检测到锁定文件，安装时会根据 package.json 生成依赖状态。")
		}
		return Result{Path: root, Output: strings.Join(checks, "\n")}, nil
	case "install":
		name, args = manager, []string{"install"}
	case "build":
		if err := fixLegacyLvyScript(root); err != nil {
			return Result{}, err
		}
		name, args = manager, []string{"run", "build"}
	case "npm-publish":
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(tag) {
			return Result{}, errors.New("npm 标签格式无效")
		}
		status, err := (Manager{}).NPMStatus(root)
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
		if token != "" {
			output, err := publishWithToken(root, tag, token)
			return Result{Path: root, Output: output}, err
		}
		name, args = "npm", []string{"publish", "--tag", tag, "--registry=https://registry.npmjs.org"}
	case "npm-version":
		if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
			return Result{}, errors.New("版本号应为 1.2.3")
		}
		name, args = "npm", []string{"version", version, "--no-git-tag-version"}
	case "dev":
		if err := fixLegacyLvyScript(root); err != nil {
			return Result{}, err
		}
		name, args = manager, []string{"run", "dev"}
	case "commit":
		if strings.TrimSpace(message) == "" {
			return Result{}, errors.New("请填写本次提交说明")
		}
		name, args = "git", []string{"add", "."}
		if _, err := run(root, name, args...); err != nil {
			return Result{}, err
		}
		name, args = "git", []string{"commit", "-m", message}
	case "pm2":
		if _, err := run(root, manager, "run", "build"); err != nil {
			return Result{}, err
		}
		name, args = "npx", []string{"pm2", "startOrRestart", "pm2.config.cjs"}
	case "install-package":
		if !allowedPackage(packageName) {
			return Result{}, errors.New("不支持的 AlemonJS 包")
		}
		name, args = manager, []string{"add", "-D", packageName}
	case "git-init":
		if _, err := run(root, "git", "init"); err != nil {
			return Result{}, err
		}
		name, args = "git", []string{"branch", "-M", "main"}
	default:
		return Result{}, errors.New("未知的机器人操作")
	}
	output, err := run(root, name, args...)
	return Result{Path: root, Output: output}, err
}
func allowedPackage(name string) bool {
	if strings.HasPrefix(name, "git+https://github.com/") || strings.HasPrefix(name, "git+https://gitee.com/") {
		return true
	}
	for _, item := range []string{"alemonjs", "@alemonjs/bubble", "@alemonjs/db", "@alemonjs/discord", "@alemonjs/onebot", "@alemonjs/qq-bot", "@alemonjs/kook", "@alemonjs/telegram"} {
		if name == item {
			return true
		}
	}
	return false
}
func project(root string) error {
	_, err := projectPath(root)
	return err
}

func projectPath(root string) (string, error) {
	if root == "." {
		current, err := os.Getwd()
		if err != nil {
			return "", errors.New("无法读取当前运行目录")
		}
		root = current
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("请选择完整的机器人文件夹路径")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("机器人文件夹不存在")
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err != nil {
		return "", errors.New("该文件夹不是可管理的 Node.js 机器人项目（缺少 package.json）")
	}
	return root, nil
}
func file(root, name string) (string, error) {
	if err := project(root); err != nil {
		return "", err
	}
	if name != ".npmrc" && name != "alemon.config.yaml" && name != "README.md" {
		return "", errors.New("不支持的文件")
	}
	return filepath.Join(root, name), nil
}

// fixLegacyLvyScript upgrades templates created before lvy was called directly.
func fixLegacyLvyScript(root string) error {
	path := filepath.Join(root, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("无法读取 package.json：%w", err)
	}
	fixed := strings.ReplaceAll(string(data), "npx lvy ", "lvy ")
	if fixed == string(data) {
		return nil
	}
	if err := os.WriteFile(path, []byte(fixed), 0644); err != nil {
		return fmt.Errorf("无法更新开发脚本：%w", err)
	}
	return nil
}
func run(root, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("%s：%w", text, err)
	}
	return text, nil
}

func runWithEnv(root string, values map[string]string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	for key, value := range values {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("%s：%w", text, err)
	}
	return text, nil
}

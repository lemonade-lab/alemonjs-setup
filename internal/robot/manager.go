// Package robot provides guarded local project management operations.
package robot

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
func (Manager) Run(root, action, message, packageName string) (Result, error) {
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
	case "install":
		name, args = manager, []string{"install"}
	case "build":
		name, args = manager, []string{"run", "build"}
	case "dev":
		name, args = manager, []string{"run", "dev"}
	case "commit":
		name, args = "git", []string{"add", "."}
		if _, err := run(root, name, args...); err != nil {
			return Result{}, err
		}
		name, args = "git", []string{"commit", "-m", message}
		if strings.TrimSpace(message) == "" {
			return Result{}, errors.New("请填写本次提交说明")
		}
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
	default:
		return Result{}, errors.New("未知的机器人操作")
	}
	output, err := run(root, name, args...)
	return Result{Path: root, Output: output}, err
}
func allowedPackage(name string) bool {
	for _, item := range []string{"alemonjs", "@alemonjs/bubble", "@alemonjs/db", "@alemonjs/discord", "@alemonjs/onebot", "@alemonjs/qq-bot"} {
		if name == item {
			return true
		}
	}
	return false
}
func project(root string) error {
	if root == "." {
		current, err := os.Getwd()
		if err != nil {
			return errors.New("无法读取当前运行目录")
		}
		root = current
	}
	if !filepath.IsAbs(root) {
		return errors.New("请选择完整的机器人文件夹路径")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return errors.New("机器人文件夹不存在")
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err != nil {
		return errors.New("该文件夹不是可管理的 Node.js 机器人项目（缺少 package.json）")
	}
	return nil
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

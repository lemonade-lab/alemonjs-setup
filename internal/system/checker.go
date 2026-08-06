// Package system contains safe, read-only checks for local prerequisites.
package system

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Check struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
}
type Report struct {
	GoalID    string  `json:"goalId"`
	Ready     bool    `json:"ready"`
	Platform  string  `json:"platform"`
	Checks    []Check `json:"checks"`
	CheckedAt string  `json:"checkedAt"`
}
type Checker struct{ timeout time.Duration }

func NewChecker() *Checker { return &Checker{timeout: 5 * time.Second} }

func (c *Checker) CheckGoal(goalID, variant string) Report {
	checks := []Check{c.platform()}
	switch goalID {
	case "install", "develop":
		checks = append(checks, c.command("node", "Node.js", "--version", "请安装 Node.js LTS 版本后重新检查。"), c.command("git", "Git", "--version", "请安装 Git 后重新检查。"))
	case "web":
		if variant == "clean" {
			checks = append(checks, c.command("node", "Node.js", "--version", "请安装 Node.js LTS 版本后重新检查。"), c.command("git", "Git", "--version", "请安装 Git 后重新检查。"))
		} else {
			checks = append(checks, c.command("docker", "Docker", "--version", "请安装并启动 Docker Desktop 后重新检查。"))
		}
	case "mobile":
		// Mobile installation is completed on the phone; the desktop app only checks host support.
	case "build":
		if variant == "git" {
			// Git 发布实际只需要 Node.js 与 Git。Yarn/PNPM 会在执行时
			// 通过 npx 临时运行，jq 也没有参与当前发布链路，不能把它们
			// 误报为新用户必须全局安装的环境。
			checks = append(checks, c.command("node", "Node.js", "--version", "请安装 Node.js LTS 版本后重新检查。"), c.command("git", "Git", "--version", "请安装 Git 后重新检查。"))
		} else {
			checks = append(checks, c.command("node", "Node.js", "--version", "请安装 Node.js LTS 版本后重新检查。"), c.command("npm", "npm", "--version", "请随 Node.js 一并安装 npm 后重新检查。"))
		}
	}
	ready := true
	for _, check := range checks {
		if check.Status != "ready" {
			ready = false
			break
		}
	}
	return Report{goalID, ready, runtime.GOOS + "/" + runtime.GOARCH, checks, time.Now().Format(time.RFC3339)}
}

func (c *Checker) platform() Check {
	switch runtime.GOOS {
	case "darwin", "windows", "linux":
		return Check{ID: "platform", Name: "当前系统", Status: "ready", Detail: runtime.GOOS + "（" + runtime.GOARCH + "）"}
	default:
		return Check{ID: "platform", Name: "当前系统", Status: "missing", Detail: "暂未支持 " + runtime.GOOS, Suggestion: "请在 Windows、macOS 或 Linux 上运行此工具。"}
	}
}

func (c *Checker) command(id, name, argument, suggestion string) Check {
	path, err := exec.LookPath(id)
	if err != nil {
		return Check{ID: id, Name: name, Status: "missing", Detail: "未检测到", Suggestion: suggestion}
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, argument).CombinedOutput()
	if ctx.Err() != nil {
		return Check{ID: id, Name: name, Status: "warning", Detail: "检测超时", Suggestion: "请确认程序可以正常启动后重试。"}
	}
	if err != nil {
		return Check{ID: id, Name: name, Status: "warning", Detail: "已找到，但无法正常运行", Suggestion: "请重新安装或修复 " + name + " 后重试。"}
	}
	version := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	return Check{ID: id, Name: name, Status: "ready", Detail: version}
}


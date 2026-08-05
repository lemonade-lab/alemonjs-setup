// Package system provides small cross-platform integrations for the setup app.
package system

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const serviceName = "com.alemonjs.albs"

// ServiceStatus reports whether the user-level service is registered and running.
func ServiceStatus() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		path, err := launchAgentPath()
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(path); err != nil {
			return "未安装后台服务。运行 albs install 进行安装。", nil
		}
		uid := strconv.Itoa(os.Getuid())
		if err := exec.Command("launchctl", "print", "gui/"+uid+"/"+serviceName).Run(); err != nil {
			return "后台服务已安装，目前已停止。", nil
		}
		return "后台服务运行中。", nil
	case "linux":
		output, err := exec.Command("systemctl", "--user", "is-active", "albs.service").CombinedOutput()
		if err != nil {
			return "后台服务未运行（" + strings.TrimSpace(string(output)) + "）。", nil
		}
		return "后台服务运行中。", nil
	case "windows":
		output, err := exec.Command("schtasks", "/Query", "/TN", "AlemonJS Setup", "/FO", "LIST").CombinedOutput()
		if err != nil {
			return "未安装后台服务。运行 albs install 进行安装。", nil
		}
		return "后台服务已注册。\n" + strings.TrimSpace(string(output)), nil
	default:
		return "", fmt.Errorf("暂不支持在 %s 上管理后台服务", runtime.GOOS)
	}
}

func StartService() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		path, err := launchAgentPath()
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(path); err != nil {
			return "", errors.New("未安装后台服务，请先运行 albs install")
		}
		uid := strconv.Itoa(os.Getuid())
		_ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+serviceName).Run()
		if output, err := exec.Command("launchctl", "bootstrap", "gui/"+uid, path).CombinedOutput(); err != nil {
			return "", fmt.Errorf("启动后台服务失败：%s", strings.TrimSpace(string(output)))
		}
		return "后台服务已启动。", nil
	case "linux":
		if output, err := exec.Command("systemctl", "--user", "start", "albs.service").CombinedOutput(); err != nil {
			return "", fmt.Errorf("启动后台服务失败：%s", strings.TrimSpace(string(output)))
		}
		return "后台服务已启动。", nil
	case "windows":
		if output, err := exec.Command("schtasks", "/Run", "/TN", "AlemonJS Setup").CombinedOutput(); err != nil {
			return "", fmt.Errorf("启动后台服务失败：%s", strings.TrimSpace(string(output)))
		}
		return "后台服务已启动。", nil
	default:
		return "", fmt.Errorf("暂不支持在 %s 上管理后台服务", runtime.GOOS)
	}
}

func StopService() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		uid := strconv.Itoa(os.Getuid())
		if output, err := exec.Command("launchctl", "bootout", "gui/"+uid+"/"+serviceName).CombinedOutput(); err != nil {
			return "", fmt.Errorf("停止后台服务失败：%s", strings.TrimSpace(string(output)))
		}
		return "后台服务已停止；登录启动配置仍然保留。", nil
	case "linux":
		if output, err := exec.Command("systemctl", "--user", "stop", "albs.service").CombinedOutput(); err != nil {
			return "", fmt.Errorf("停止后台服务失败：%s", strings.TrimSpace(string(output)))
		}
		return "后台服务已停止；登录启动配置仍然保留。", nil
	case "windows":
		if output, err := exec.Command("schtasks", "/End", "/TN", "AlemonJS Setup").CombinedOutput(); err != nil {
			return "", fmt.Errorf("停止后台服务失败：%s", strings.TrimSpace(string(output)))
		}
		return "后台服务已停止；登录启动配置仍然保留。", nil
	default:
		return "", fmt.Errorf("暂不支持在 %s 上管理后台服务", runtime.GOOS)
	}
}

func RestartService() (string, error) {
	if _, err := StopService(); err != nil {
		return "", err
	}
	return StartService()
}

func UninstallService() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		uid := strconv.Itoa(os.Getuid())
		_ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+serviceName).Run()
		path, err := launchAgentPath()
		if err != nil {
			return "", err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	case "linux":
		_ = exec.Command("systemctl", "--user", "disable", "--now", "albs.service").Run()
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if err := os.Remove(filepath.Join(home, ".config", "systemd", "user", "albs.service")); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	case "windows":
		_ = exec.Command("schtasks", "/Delete", "/TN", "AlemonJS Setup", "/F").Run()
	default:
		return "", fmt.Errorf("暂不支持在 %s 上管理后台服务", runtime.GOOS)
	}
	return "后台服务已移除。albs 命令文件仍保留，便于以后重新安装。", nil
}

// InstallService registers the current binary as a user-level background service.
func InstallService(port string) (string, error) {
	value, err := strconv.ParseUint(port, 10, 16)
	if err != nil || value == 0 {
		return "", errors.New("端口应为 1 到 65535 的数字")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法定位当前程序：%w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	installed, note, err := installCommand(executable)
	if err != nil {
		return "", err
	}
	executable = installed
	var result string
	switch runtime.GOOS {
	case "darwin":
		result, err = installLaunchAgent(executable, port)
	case "linux":
		result, err = installSystemdUserService(executable, port)
	case "windows":
		result, err = installScheduledTask(executable, port)
	default:
		return "", fmt.Errorf("暂不支持在 %s 上注册后台服务", runtime.GOOS)
	}
	if err != nil {
		return "", err
	}
	return "albs 命令已安装到：" + executable + "\n" + note + "\n" + result, nil
}

func installCommand(source string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	directory := filepath.Join(home, ".local", "bin")
	if runtime.GOOS == "windows" {
		directory = filepath.Join(home, "AppData", "Local", "albs")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", "", fmt.Errorf("无法创建 albs 命令目录：%w", err)
	}
	name := "albs"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(directory, name)
	if filepath.Clean(source) != filepath.Clean(target) {
		input, err := os.Open(source)
		if err != nil {
			return "", "", err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
		if err != nil {
			return "", "", fmt.Errorf("无法安装 albs 命令：%w", err)
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return "", "", copyErr
		}
		if closeErr != nil {
			return "", "", closeErr
		}
	}
	note := "现在可使用 albs open 打开引导。"
	if !pathContains(directory) {
		note = "请将 " + directory + " 加入 PATH 后，可直接使用 albs 命令。"
	}
	return target, note, nil
}

func pathContains(directory string) bool {
	for _, item := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(item) == filepath.Clean(directory) {
			return true
		}
	}
	return false
}

func OpenBrowser(port string) error {
	url := "http://127.0.0.1:" + port
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("无法打开浏览器：%w", err)
	}
	return nil
}

func installLaunchAgent(executable, port string) (string, error) {
	path, err := launchAgentPath()
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	logs := filepath.Join(home, "Library", "Logs", "albs.log")
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>serve</string><string>--port</string><string>%s</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string></dict></plist>
`, serviceName, xmlEscape(executable), xmlEscape(port), xmlEscape(logs), xmlEscape(logs))
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return "", err
	}
	uid := strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+serviceName).Run()
	if output, err := exec.Command("launchctl", "bootstrap", "gui/"+uid, path).CombinedOutput(); err != nil {
		return "", fmt.Errorf("注册 LaunchAgent 失败：%s", strings.TrimSpace(string(output)))
	}
	if output, err := exec.Command("launchctl", "kickstart", "-k", "gui/"+uid+"/"+serviceName).CombinedOutput(); err != nil {
		return "", fmt.Errorf("启动后台服务失败：%s", strings.TrimSpace(string(output)))
	}
	return "已注册后台服务。登录后会自动运行，访问地址：http://127.0.0.1:" + port, nil
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", serviceName+".plist"), nil
}

func installSystemdUserService(executable, port string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(directory, "albs.service")
	content := fmt.Sprintf("[Unit]\nDescription=AlemonJS Setup\n[Service]\nExecStart=%s serve --port %s\nRestart=on-failure\n[Install]\nWantedBy=default.target\n", shellQuote(executable), port)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	if output, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return "", fmt.Errorf("刷新 systemd 配置失败：%s", strings.TrimSpace(string(output)))
	}
	if output, err := exec.Command("systemctl", "--user", "enable", "--now", "albs.service").CombinedOutput(); err != nil {
		return "", fmt.Errorf("启动后台服务失败：%s", strings.TrimSpace(string(output)))
	}
	return "已注册 systemd 用户服务，访问地址：http://127.0.0.1:" + port, nil
}

func installScheduledTask(executable, port string) (string, error) {
	command := `"` + executable + `" serve --port ` + port
	if output, err := exec.Command("schtasks", "/Create", "/TN", "AlemonJS Setup", "/SC", "ONLOGON", "/TR", command, "/F").CombinedOutput(); err != nil {
		return "", fmt.Errorf("注册计划任务失败：%s", strings.TrimSpace(string(output)))
	}
	if output, err := exec.Command("schtasks", "/Run", "/TN", "AlemonJS Setup").CombinedOutput(); err != nil {
		return "", fmt.Errorf("启动后台服务失败：%s", strings.TrimSpace(string(output)))
	}
	return "已注册登录启动任务，访问地址：http://127.0.0.1:" + port, nil
}

func xmlEscape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(value)
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", `"'"'`) + "'" }

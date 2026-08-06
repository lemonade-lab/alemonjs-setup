package system

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// RunWithPrivileges runs one already-approved local operation with the
// operating system's native administrator prompt.  It never changes ownership
// or stores credentials: every call requires a new authorization.
func RunWithPrivileges(directory string, values map[string]string, name string, args ...string) (string, error) {
	script := privilegedScript(directory, values, name, args)
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// osascript delegates authentication to macOS.  The command is built
		// from individually quoted arguments rather than user-provided shell.
		statement := "do shell script " + strconv.Quote(script) + " with administrator privileges"
		command = exec.Command("osascript", "-e", statement)
	case "linux":
		// pkexec is the standard Polkit entry point on desktop Linux.
		command = exec.Command("pkexec", "/bin/sh", "-lc", script)
	case "windows":
		return runWindowsElevated(directory, values, name, args...)
	default:
		return "", fmt.Errorf("当前系统不支持权限提升：%s", runtime.GOOS)
	}
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if runtime.GOOS == "linux" && errors.Is(err, exec.ErrNotFound) {
			return "此 Linux 系统没有可用的 pkexec 权限服务。请将机器人项目放到当前用户拥有的目录（例如 ~/alemonjs），或安装并启用 polkit/pkexec 后重试。", fmt.Errorf("Linux 缺少 pkexec：%w", err)
		}
		if text == "" {
			text = "用户取消了系统权限授权，或系统拒绝了本次提升。"
		}
		return text, fmt.Errorf("需要管理员权限才能完成此操作：%w", err)
	}
	return text, nil
}

func runWindowsElevated(directory string, values map[string]string, name string, args ...string) (string, error) {
	outputFile, err := os.CreateTemp("", "alx-elevated-output-*.txt")
	if err != nil {
		return "", fmt.Errorf("无法准备权限操作：%w", err)
	}
	outputPath := outputFile.Name()
	defer os.Remove(outputPath)
	if err := outputFile.Chmod(0666); err != nil {
		return "", fmt.Errorf("无法准备权限操作：%w", err)
	}
	if err := outputFile.Close(); err != nil {
		return "", fmt.Errorf("无法准备权限操作：%w", err)
	}
	scriptFile, err := os.CreateTemp("", "alx-elevated-command-*.ps1")
	if err != nil {
		return "", fmt.Errorf("无法准备权限操作：%w", err)
	}
	scriptPath := scriptFile.Name()
	defer os.Remove(scriptPath)
	if _, err := scriptFile.WriteString(windowsScript(directory, values, name, args, outputPath)); err != nil {
		return "", fmt.Errorf("无法准备权限操作：%w", err)
	}
	if err := scriptFile.Close(); err != nil {
		return "", fmt.Errorf("无法准备权限操作：%w", err)
	}
	launcher := "$p = Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File'," + powershellQuote(scriptPath) + ") -Verb RunAs -Wait -PassThru; exit $p.ExitCode"
	command := exec.Command("powershell.exe", "-NoProfile", "-Command", launcher)
	launcherOutput, runErr := command.CombinedOutput()
	data, readErr := os.ReadFile(outputPath)
	text := strings.TrimSpace(string(data))
	if text == "" {
		text = strings.TrimSpace(string(launcherOutput))
	}
	if runErr != nil {
		if text == "" {
			text = "用户取消了 Windows UAC 授权，或系统拒绝了本次提升。"
		}
		return text, fmt.Errorf("需要管理员权限才能完成此操作：%w", runErr)
	}
	if readErr != nil {
		return "", fmt.Errorf("无法读取提升操作输出：%w", readErr)
	}
	return text, nil
}

func windowsScript(directory string, values map[string]string, name string, args []string, outputPath string) string {
	lines := []string{"$ErrorActionPreference = 'Stop'"}
	if directory != "" {
		lines = append(lines, "Set-Location -LiteralPath "+powershellQuote(directory))
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, "$env:"+key+" = "+powershellQuote(values[key]))
	}
	arguments := make([]string, len(args))
	for index, arg := range args {
		arguments[index] = powershellQuote(arg)
	}
	lines = append(lines,
		"$output = & "+powershellQuote(name)+" @("+strings.Join(arguments, ",")+") 2>&1",
		"$exitCode = $LASTEXITCODE",
		"[System.IO.File]::WriteAllText("+powershellQuote(outputPath)+", ($output | Out-String -Width 4096))",
		"exit $exitCode",
	)
	return strings.Join(lines, "\r\n")
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// WriteFileWithPrivileges is used only after a normal write has failed with a
// permission error. Content is base64 encoded so it is never interpreted as a
// shell fragment.
func WriteFileWithPrivileges(path string, content []byte) error {
	encoded := base64.StdEncoding.EncodeToString(content)
	if runtime.GOOS == "windows" {
		_, err := RunWithPrivileges("", nil, "powershell.exe", "-NoProfile", "-Command", "[System.IO.File]::WriteAllBytes("+powershellQuote(path)+", [System.Convert]::FromBase64String("+powershellQuote(encoded)+"))")
		return err
	}
	decoder := "-d"
	if runtime.GOOS == "darwin" {
		decoder = "-D"
	}
	_, err := RunWithPrivileges("", nil, "sh", "-lc", "printf %s "+shellQuote(encoded)+" | base64 "+decoder+" > "+shellQuote(path))
	return err
}

func privilegedScript(directory string, values map[string]string, name string, args []string) string {
	parts := make([]string, 0, len(args)+len(values)+4)
	if directory != "" {
		parts = append(parts, "cd -- "+shellQuote(directory), "exec")
	}
	parts = append(parts, "env")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, shellQuote(key+"="+values[key]))
	}
	parts = append(parts, shellQuote(name))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

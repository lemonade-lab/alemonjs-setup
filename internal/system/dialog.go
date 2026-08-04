package system

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ChooseDirectory opens the host system's directory picker.
func ChooseDirectory() (string, error) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("osascript", "-e", "POSIX path of (choose folder with prompt \"选择文件夹\")")
	case "windows":
		command = exec.Command("powershell", "-NoProfile", "-Command", "Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.FolderBrowserDialog; if($d.ShowDialog() -eq 'OK'){[Console]::Write($d.SelectedPath)}")
	case "linux":
		command = exec.Command("zenity", "--file-selection", "--directory", "--title=选择文件夹")
	default:
		return "", fmt.Errorf("当前系统暂不支持文件夹选择")
	}
	output, err := command.Output()
	path := strings.TrimSpace(string(output))
	if err != nil || path == "" {
		return "", fmt.Errorf("未选择文件夹")
	}
	return path, nil
}

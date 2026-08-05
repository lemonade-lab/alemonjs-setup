package system

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ChooseDirectories opens the host system's directory picker.
func ChooseDirectories() ([]string, error) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("osascript", "-e", "set selectedFolders to choose folder with prompt \"选择机器人目录\" with multiple selections allowed", "-e", "set paths to {}", "-e", "repeat with selectedFolder in selectedFolders", "-e", "set end of paths to POSIX path of selectedFolder", "-e", "end repeat", "-e", "set AppleScript's text item delimiters to linefeed", "-e", "return paths as text")
	case "windows":
		command = exec.Command("powershell", "-NoProfile", "-Command", "Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.FolderBrowserDialog; if($d.ShowDialog() -eq 'OK'){[Console]::Write($d.SelectedPath)}")
	case "linux":
		command = exec.Command("zenity", "--file-selection", "--directory", "--multiple", "--separator=\n", "--title=选择机器人目录")
	default:
		return nil, fmt.Errorf("当前系统暂不支持文件夹选择")
	}
	output, err := command.Output()
	paths := strings.FieldsFunc(strings.TrimSpace(string(output)), func(r rune) bool { return r == '\n' || r == '\r' })
	if err != nil || len(paths) == 0 {
		return nil, fmt.Errorf("未选择文件夹")
	}
	return paths, nil
}

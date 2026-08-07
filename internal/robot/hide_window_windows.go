//go:build windows

package robot

import (
	"os/exec"
	"syscall"
)

// HideWindow suppresses the console window that Windows would otherwise create
// for a spawned Node/package-manager child process. Without it, every install,
// status check, or run flashes a "node window", which is confusing when alx is
// driven from its own GUI.
func HideWindow(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

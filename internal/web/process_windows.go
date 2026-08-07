//go:build windows

package web

import (
	"os"
	"os/exec"

	"alemonx/internal/robot"
)

// configureManagedProcess hides the console window that Windows would otherwise
// open for the supervised dev/app process group.
func configureManagedProcess(command *exec.Cmd) {
	robot.HideWindow(command)
}

func interruptManagedProcess(command *exec.Cmd) error {
	return command.Process.Signal(os.Interrupt)
}

func forceStopManagedProcess(command *exec.Cmd) error {
	return command.Process.Kill()
}

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

// processGroupID returns 0 on Windows; process-group signalling is not used.
func processGroupID(command *exec.Cmd) int {
	return 0
}

func processGroupAlive(_ int) bool { return false }

func killProcessGroup(_ int) {}

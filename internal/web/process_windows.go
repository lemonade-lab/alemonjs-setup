//go:build windows

package web

import (
	"os"
	"os/exec"
)

func configureManagedProcess(_ *exec.Cmd) {}

func interruptManagedProcess(command *exec.Cmd) error {
	return command.Process.Signal(os.Interrupt)
}

func forceStopManagedProcess(command *exec.Cmd) error {
	return command.Process.Kill()
}

//go:build !windows

package web

import (
	"os/exec"
	"syscall"
)

// configureManagedProcess makes the command and all of its descendants a
// private process group. This lets Stop terminate Yarn and the process it
// spawned, instead of leaving the robot orphaned after only Yarn exits.
func configureManagedProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptManagedProcess(command *exec.Cmd) error {
	return syscall.Kill(-command.Process.Pid, syscall.SIGINT)
}

func forceStopManagedProcess(command *exec.Cmd) error {
	return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}

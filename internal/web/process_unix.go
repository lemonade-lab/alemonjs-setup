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

// processGroupID returns the process-group id for a configured command. The
// command was started with Setpgid, so the group equals its own pid.
func processGroupID(command *exec.Cmd) int {
	if command.Process == nil {
		return 0
	}
	return command.Process.Pid
}

// processGroupAlive reports whether a process group still exists.
func processGroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || err == syscall.EPERM
}

// killProcessGroup forcibly terminates every member of a process group.
func killProcessGroup(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

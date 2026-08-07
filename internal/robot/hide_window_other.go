//go:build !windows

package robot

import "os/exec"

// HideWindow is a no-op outside Windows, where child processes do not spawn a
// separate console window.
func HideWindow(_ *exec.Cmd) {}

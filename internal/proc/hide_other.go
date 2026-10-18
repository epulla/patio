//go:build !windows

package proc

import "os/exec"

func HideConsole(*exec.Cmd) {}

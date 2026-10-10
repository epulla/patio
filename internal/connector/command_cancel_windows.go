//go:build windows

package connector

import (
	"os/exec"
	"time"
)

func configureCommandCancel(cmd *exec.Cmd) {
	cmd.WaitDelay = 3 * time.Second
}

package filetools

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// TrashCommand builds the OS-specific argv used to move a path to Trash.
func TrashCommand(goos, absPath string) (string, []string, error) {
	switch goos {
	case "darwin":
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", `\r`, "\n", `\n`, "\t", `\t`).Replace(absPath)
		return "osascript", []string{"-e", `tell application "Finder" to delete POSIX file "` + escaped + `"`}, nil
	case "windows":
		quoted := strings.ReplaceAll(absPath, "'", "''")
		script := "$ErrorActionPreference = 'Stop'; Add-Type -AssemblyName Microsoft.VisualBasic; $p = '" + quoted + "'; " +
			"if ([System.IO.Directory]::Exists($p)) { [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory($p, [Microsoft.VisualBasic.FileIO.UIOption]::OnlyErrorDialogs, [Microsoft.VisualBasic.FileIO.RecycleOption]::SendToRecycleBin) } " +
			"else { [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile($p, [Microsoft.VisualBasic.FileIO.UIOption]::OnlyErrorDialogs, [Microsoft.VisualBasic.FileIO.RecycleOption]::SendToRecycleBin) }"
		return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", script}, nil
	case "linux":
		return "gio", []string{"trash", "--", absPath}, nil
	default:
		return "", nil, fmt.Errorf("trash is not supported on %s", goos)
	}
}

var trashRunner = func(ctx context.Context, path string) error {
	name, args, err := TrashCommand(runtime.GOOS, path)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s is unavailable", name)
	}
	return exec.CommandContext(ctx, name, args...).Run()
}

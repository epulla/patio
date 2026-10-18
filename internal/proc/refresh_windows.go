//go:build windows

package proc

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

// RefreshPath reloads PATH from the registry, so CLIs installed while Patio
// runs are found without a restart. Failures keep the current PATH.
func RefreshPath() {
	machine, machineErr := readPath(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
	user, userErr := readPath(registry.CURRENT_USER, `Environment`)
	if machineErr != nil && userErr != nil {
		return
	}
	_ = os.Setenv("PATH", registryPath(machine, user, os.Getenv("PATH"), os.Getenv))
}

func readPath(root registry.Key, path string) (string, error) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue("Path")
	return value, err
}

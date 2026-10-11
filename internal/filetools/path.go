package filetools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolve(root, rel string) (string, string, error) {
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || windowsAbsolute(rel) {
		return "", "", errors.New("path must be relative to the work directory")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolve work directory: %w", err)
	}
	rootInfo, err := os.Stat(rootReal)
	if err != nil {
		return "", "", err
	}
	if !rootInfo.IsDir() {
		return "", "", errors.New("work directory is not a directory")
	}
	clean := filepath.Clean(rel)
	if clean == "" {
		clean = "."
	}
	target := filepath.Join(rootReal, clean)
	if !pathWithin(rootReal, target) {
		return "", "", errors.New("path resolves outside the work directory")
	}
	ancestor := target
	var missing []string
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", "", fmt.Errorf("no existing ancestor for %q", rel)
		}
		missing = append(missing, filepath.Base(ancestor))
		ancestor = parent
	}
	ancestorReal, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", "", err
	}
	if !pathWithin(rootReal, ancestorReal) {
		return "", "", errors.New("path resolves outside the work directory")
	}
	resolved := ancestorReal
	for i := len(missing) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, missing[i])
	}
	if !pathWithin(rootReal, resolved) {
		return "", "", errors.New("path resolves outside the work directory")
	}
	return target, rootReal, nil
}

func windowsAbsolute(path string) bool {
	if strings.HasPrefix(path, `\`) || strings.HasPrefix(path, `//`) {
		return true
	}
	return len(path) >= 2 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':'
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func samePath(left, right string) bool {
	left, _ = filepath.Abs(left)
	right, _ = filepath.Abs(right)
	return filepath.Clean(left) == filepath.Clean(right)
}

func isRootPath(root, target string) bool {
	real, err := filepath.EvalSymlinks(target)
	return err == nil && samePath(real, root)
}

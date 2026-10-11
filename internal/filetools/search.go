package filetools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxSearchHits = 200

var findRG = func() string {
	if path, err := exec.LookPath("rg"); err == nil {
		return path
	}
	if path, err := exec.LookPath("/opt/homebrew/bin/rg"); err == nil {
		return path
	}
	return ""
}

func runSearch(ctx context.Context, root, query, glob, rel string) (string, error) {
	if query == "" {
		return "", errors.New("query must not be empty")
	}
	searchRoot, _, err := resolve(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(searchRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("search path is not a directory")
	}
	var hits []string
	if rg := findRG(); rg != "" {
		hits, err = searchWithRG(ctx, rg, searchRoot, query, glob)
	} else {
		hits, err = searchWithWalk(ctx, searchRoot, query, glob)
	}
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "No matches", nil
	}
	return strings.Join(hits, "\n"), nil
}

func searchWithRG(ctx context.Context, rg, root, query, glob string) ([]string, error) {
	args := []string{"--line-number", "--no-heading", "--color", "never"}
	if glob != "" {
		args = append(args, "--glob", glob)
	}
	args = append(args, "--", query, root)
	cmd := exec.CommandContext(ctx, rg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	hits := make([]string, 0, maxSearchHits)
	for scanner.Scan() {
		hits = append(hits, strings.TrimSuffix(scanner.Text(), "\r"))
		if len(hits) == maxSearchHits {
			_ = cmd.Process.Kill()
			break
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}
	if len(hits) == maxSearchHits {
		return hits, nil
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 1 {
			return hits, nil
		}
		if len(stderr.Bytes()) != 0 {
			return nil, fmt.Errorf("rg: %s", strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("rg: %w", waitErr)
	}
	return hits, nil
}

func searchWithWalk(ctx context.Context, root, query, glob string) ([]string, error) {
	hits := make([]string, 0, maxSearchHits)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.Type()&os.ModeType != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if glob != "" {
			matched, err := globMatch(glob, rel)
			if err != nil {
				return fmt.Errorf("invalid glob: %w", err)
			}
			if !matched {
				return nil
			}
		}
		fileInfo, err := os.Lstat(path)
		if err != nil || !fileInfo.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		reader := bufio.NewScanner(file)
		reader.Buffer(make([]byte, 64*1024), 1024*1024)
		fileHits := make([]string, 0)
		binary := false
		remaining := maxSearchHits - len(hits)
		limitReached := false
		for lineNumber := 1; reader.Scan(); lineNumber++ {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return err
			}
			line := reader.Bytes()
			if bytes.IndexByte(line, 0) >= 0 || !utf8.Valid(line) {
				binary = true
				break
			}
			if strings.Contains(string(line), query) {
				fileHits = append(fileHits, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), lineNumber, string(line)))
				if len(fileHits) == remaining {
					limitReached = true
					break
				}
			}
		}
		_ = file.Close()
		if binary || reader.Err() != nil {
			return nil
		}
		hits = append(hits, fileHits...)
		if limitReached || len(hits) == maxSearchHits {
			return io.EOF
		}
		return nil
	})
	if errors.Is(err, io.EOF) {
		return hits, nil
	}
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func globMatch(glob, rel string) (bool, error) {
	glob = filepath.ToSlash(glob)
	rel = filepath.ToSlash(rel)
	patternParts := strings.Split(glob, "/")
	pathParts := strings.Split(rel, "/")
	if len(patternParts) == 1 {
		return filepath.Match(glob, filepath.Base(rel))
	}
	return matchGlobParts(patternParts, pathParts)
}

func matchGlobParts(pattern, path []string) (bool, error) {
	if len(pattern) == 0 {
		return len(path) == 0, nil
	}
	if pattern[0] == "**" {
		if matched, err := matchGlobParts(pattern[1:], path); matched || err != nil {
			return matched, err
		}
		if len(path) == 0 {
			return false, nil
		}
		return matchGlobParts(pattern, path[1:])
	}
	if len(path) == 0 {
		return false, nil
	}
	matched, err := filepath.Match(pattern[0], path[0])
	if err != nil || !matched {
		return matched, err
	}
	return matchGlobParts(pattern[1:], path[1:])
}

package filetools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxReadBytes = 256 * 1024
	maxListItems = 500
)

// Tool describes one local file tool in OpenAI's function-tool format.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Write       bool            `json:"-"`
}

var toolDefinitions = []Tool{
	{Name: "list_dir", Description: "List files and directories", Parameters: schema(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path, relative to the work directory"}},"additionalProperties":false}`)},
	{Name: "read_file", Description: "Read a text file", Parameters: schema(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
	{Name: "search", Description: "Search text files", Parameters: schema(`{"type":"object","properties":{"query":{"type":"string"},"glob":{"type":"string"},"path":{"type":"string"}},"required":["query"],"additionalProperties":false}`)},
	{Name: "write_file", Description: "Write a text file", Write: true, Parameters: schema(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)},
	{Name: "edit_file", Description: "Replace exact text in a file", Write: true, Parameters: schema(`{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"],"additionalProperties":false}`)},
	{Name: "move", Description: "Move a file or directory", Write: true, Parameters: schema(`{"type":"object","properties":{"source":{"type":"string"},"destination":{"type":"string"}},"required":["source","destination"],"additionalProperties":false}`)},
	{Name: "copy", Description: "Copy a file or directory", Write: true, Parameters: schema(`{"type":"object","properties":{"source":{"type":"string"},"destination":{"type":"string"}},"required":["source","destination"],"additionalProperties":false}`)},
	{Name: "duplicate", Description: "Duplicate a file or directory alongside the original", Write: true, Parameters: schema(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
	{Name: "delete", Description: "Move a file or directory to the operating system trash", Write: true, Parameters: schema(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
}

func schema(value string) json.RawMessage { return json.RawMessage(value) }

// Tools returns the descriptions of all supported tools.
func Tools() []Tool {
	tools := make([]Tool, len(toolDefinitions))
	copy(tools, toolDefinitions)
	return tools
}

// Run validates arguments and runs one tool against root.
func Run(ctx context.Context, root, name string, args json.RawMessage) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	values, err := decodeArgs(args)
	if err != nil {
		return "", err
	}
	switch name {
	case "list_dir":
		path, err := optionalString(values, "path", ".")
		if err != nil {
			return "", err
		}
		return runListDir(root, path)
	case "read_file":
		path, err := requiredString(values, "path")
		if err != nil {
			return "", err
		}
		return runReadFile(root, path)
	case "search":
		query, err := requiredString(values, "query")
		if err != nil {
			return "", err
		}
		glob, err := optionalString(values, "glob", "")
		if err != nil {
			return "", err
		}
		path, err := optionalString(values, "path", ".")
		if err != nil {
			return "", err
		}
		return runSearch(ctx, root, query, glob, path)
	case "write_file":
		path, err := requiredString(values, "path")
		if err != nil {
			return "", err
		}
		content, err := requiredString(values, "content")
		if err != nil {
			return "", err
		}
		return runWriteFile(root, path, content)
	case "edit_file":
		path, err := requiredString(values, "path")
		if err != nil {
			return "", err
		}
		oldString, err := requiredString(values, "old_string")
		if err != nil {
			return "", err
		}
		newString, err := requiredString(values, "new_string")
		if err != nil {
			return "", err
		}
		return runEditFile(root, path, oldString, newString)
	case "move", "copy":
		source, err := requiredString(values, "source")
		if err != nil {
			return "", err
		}
		destination, err := requiredString(values, "destination")
		if err != nil {
			return "", err
		}
		return runMoveCopy(root, source, destination, name == "copy")
	case "duplicate":
		path, err := requiredString(values, "path")
		if err != nil {
			return "", err
		}
		return runDuplicate(root, path)
	case "delete":
		path, err := requiredString(values, "path")
		if err != nil {
			return "", err
		}
		return runDelete(ctx, root, path)
	default:
		return "", fmt.Errorf("unknown file tool %q", name)
	}
}

// Summary gives a short human-readable approval description.
func Summary(name string, args json.RawMessage) string {
	values, err := decodeArgs(args)
	if err != nil {
		return strings.ReplaceAll(name, "_", " ")
	}
	var target string
	switch name {
	case "move", "copy":
		target = stringArg(values, "destination", "")
	case "edit_file", "write_file", "duplicate", "delete":
		target = stringArg(values, "path", "")
	default:
		return strings.ReplaceAll(name, "_", " ")
	}
	verb := map[string]string{"write_file": "Write", "edit_file": "Edit", "move": "Move", "copy": "Copy", "duplicate": "Duplicate", "delete": "Delete"}[name]
	if target == "" {
		return verb
	}
	return verb + " " + filepath.ToSlash(target)
}

func decodeArgs(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var values map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("tool arguments must be a JSON object")
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	if values == nil {
		return nil, errors.New("tool arguments must be a JSON object")
	}
	return values, nil
}

func requiredString(values map[string]json.RawMessage, key string) (string, error) {
	raw, ok := values[key]
	if !ok {
		return "", fmt.Errorf("missing %q", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%q must be a string", key)
	}
	return value, nil
}

func stringArg(values map[string]json.RawMessage, key, fallback string) string {
	raw, ok := values[key]
	if !ok {
		return fallback
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return fallback
	}
	return value
}

func optionalString(values map[string]json.RawMessage, key, fallback string) (string, error) {
	raw, ok := values[key]
	if !ok {
		return fallback, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%q must be a string", key)
	}
	return value, nil
}

func runListDir(root, rel string) (string, error) {
	target, _, err := resolve(root, rel)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", err
	}
	if len(entries) > maxListItems {
		entries = entries[:maxListItems]
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%d", kind, entry.Name(), info.Size()))
	}
	return strings.Join(lines, "\n"), nil
}

func runReadFile(root, rel string) (string, error) {
	target, _, err := resolve(root, rel)
	if err != nil {
		return "", err
	}
	file, err := os.Open(target)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxReadBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxReadBytes {
		return "", fmt.Errorf("file exceeds %d-byte read limit", maxReadBytes)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", errors.New("file is binary or invalid UTF-8")
	}
	return string(data), nil
}

func runWriteFile(root, rel, content string) (string, error) {
	target, _, err := resolve(root, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if _, _, err := resolve(root, rel); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return "", err
	}
	return "File written", nil
}

func runEditFile(root, rel, oldString, newString string) (string, error) {
	target, _, err := resolve(root, rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if len(data) > maxReadBytes {
		return "", fmt.Errorf("file exceeds %d-byte edit limit", maxReadBytes)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", errors.New("file is binary or invalid UTF-8")
	}
	count := strings.Count(string(data), oldString)
	if count != 1 {
		return "", fmt.Errorf("old_string must match exactly once; matched %d times", count)
	}
	if _, _, err := resolve(root, rel); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, []byte(strings.Replace(string(data), oldString, newString, 1)), 0o644); err != nil {
		return "", err
	}
	return "File edited", nil
}

func runMoveCopy(root, source, destination string, copyFile bool) (string, error) {
	src, realRoot, err := resolve(root, source)
	if err != nil {
		return "", err
	}
	if isRootPath(realRoot, src) {
		return "", errors.New("cannot move or copy the work directory itself")
	}
	dst, _, err := resolve(root, destination)
	if err != nil {
		return "", err
	}
	if samePath(src, dst) || pathWithin(src, dst) {
		return "", errors.New("destination cannot be source or inside source")
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", fmt.Errorf("destination already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if _, _, err := resolve(root, destination); err != nil {
		return "", err
	}
	if copyFile {
		if err := copyPath(src, dst); err != nil {
			return "", err
		}
	} else if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	if copyFile {
		return "File copied", nil
	}
	return "File moved", nil
}

func runDuplicate(root, rel string) (string, error) {
	source, realRoot, err := resolve(root, rel)
	if err != nil {
		return "", err
	}
	if isRootPath(realRoot, source) {
		return "", errors.New("cannot duplicate the work directory itself")
	}
	dir, name := filepath.Dir(source), filepath.Base(source)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		base, ext = name, ""
	}
	for index := 1; ; index++ {
		copyName := base + " copy" + ext
		if index > 1 {
			copyName = fmt.Sprintf("%s copy %d%s", base, index, ext)
		}
		destination := filepath.Join(dir, copyName)
		if _, err := os.Lstat(destination); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		relDestination, err := filepath.Rel(realRoot, destination)
		if err != nil {
			return "", err
		}
		if _, _, err := resolve(root, relDestination); err != nil {
			return "", err
		}
		if err := copyPath(source, destination); err != nil {
			return "", err
		}
		return filepath.Base(destination), nil
	}
}

func runDelete(ctx context.Context, root, rel string) (string, error) {
	target, realRoot, err := resolve(root, rel)
	if err != nil {
		return "", err
	}
	if isRootPath(realRoot, target) {
		return "", errors.New("cannot delete the work directory")
	}
	if err := trashRunner(ctx, target); err != nil {
		return "", fmt.Errorf("send to trash: %w", err)
	}
	return "Moved to Trash", nil
}

func copyPath(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("copying symbolic links is not supported: %s", source)
	}
	if info.IsDir() {
		if err := os.Mkdir(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("copying non-regular files is not supported: %s", source)
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

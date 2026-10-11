package filetools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveRejectsUnsafePaths(t *testing.T) {
	t.Run("parent traversal and sibling prefix", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "root")
		sibling := filepath.Join(base, "root2")
		mustMkdir(t, root)
		mustMkdir(t, sibling)
		for _, path := range []string{"../root2/secret", "../../outside"} {
			t.Run(path, func(t *testing.T) {
				if _, _, err := resolve(root, path); err == nil {
					t.Fatalf("resolve(%q) unexpectedly succeeded", path)
				}
			})
		}
	})
	t.Run("absolute path", func(t *testing.T) {
		root := t.TempDir()
		if _, _, err := resolve(root, filepath.Join(root, "secret")); err == nil {
			t.Fatal("absolute path unexpectedly succeeded")
		}
		if _, _, err := resolve(root, `C:\secret`); err == nil {
			t.Fatal("Windows absolute path unexpectedly succeeded")
		}
	})
	t.Run("symlink escape", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "root")
		outside := filepath.Join(base, "outside")
		mustMkdir(t, root)
		mustMkdir(t, outside)
		if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, _, err := resolve(root, "link/secret.txt"); err == nil {
			t.Fatal("symlink escape unexpectedly succeeded")
		}
	})
}

func TestWritesRejectSymlinkedFileOutsideWorkdir(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside.txt")
	mustMkdir(t, root)
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, tc := range []struct {
		name string
		args map[string]string
	}{
		{name: "write", args: map[string]string{"path": "link.txt", "content": "overwritten"}},
		{name: "edit", args: map[string]string{"path": "link.txt", "old_string": "original", "new_string": "overwritten"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := "write_file"
			if tc.name == "edit" {
				tool = "edit_file"
			}
			if _, err := Run(context.Background(), root, tool, rawArgs(t, tc.args)); err == nil {
				t.Fatal("write through outside symlink unexpectedly succeeded")
			}
			got, err := os.ReadFile(outside)
			if err != nil || string(got) != "original" {
				t.Fatalf("outside file = %q, %v", got, err)
			}
		})
	}
}

func TestEditRequiresExactlyOneMatch(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		count         int
	}{
		{name: "none", content: "before", count: 0},
		{name: "once", content: "before target after", count: 1},
		{name: "twice", content: "target target", count: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Run(context.Background(), root, "edit_file", rawArgs(t, map[string]string{
				"path": "notes.md", "old_string": "target", "new_string": "changed",
			}))
			if tc.count != 1 {
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("matched %d", tc.count)) {
					t.Fatalf("expected exact match count %d, got %v", tc.count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(root, "notes.md"))
			if err != nil || string(got) != "before changed after" {
				t.Fatalf("edited content = %q, err = %v", got, err)
			}
		})
	}
}

func TestDuplicateNaming(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"notes.md", "notes copy.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Run(context.Background(), root, "duplicate", rawArgs(t, map[string]string{"path": "notes.md"}))
	if err != nil {
		t.Fatal(err)
	}
	if result != "notes copy 2.md" {
		t.Fatalf("duplicate result = %q", result)
	}
	got, err := os.ReadFile(filepath.Join(root, result))
	if err != nil || string(got) != "notes.md" {
		t.Fatalf("duplicate content = %q, err = %v", got, err)
	}
}

func TestReadFileLimitsAndBinary(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name string
		data []byte
		want string
		bad  bool
	}{
		{name: "at limit", data: []byte(strings.Repeat("a", maxReadBytes)), want: strings.Repeat("a", maxReadBytes)},
		{name: "over limit", data: []byte(strings.Repeat("a", maxReadBytes+1)), bad: true},
		{name: "NUL", data: []byte("a\x00b"), bad: true},
		{name: "invalid UTF-8", data: []byte{0xff, 0xfe}, bad: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "file.txt"), tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), root, "read_file", rawArgs(t, map[string]string{"path": "file.txt"}))
			if tc.bad {
				if err == nil {
					t.Fatal("expected read rejection")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("read result length=%d err=%v", len(got), err)
			}
		})
	}
}

func TestSearchFallbackAndHitCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < maxSearchHits+5; i++ {
		name := fmt.Sprintf("%03d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldFindRG := findRG
	findRG = func() string { return "" }
	t.Cleanup(func() { findRG = oldFindRG })
	got, err := Run(context.Background(), root, "search", rawArgs(t, map[string]string{"query": "needle"}))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(got, "needle"); count != maxSearchHits {
		t.Fatalf("fallback returned %d hits, want %d", count, maxSearchHits)
	}
}

func TestSearchRGHitCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper is a shell script")
	}
	root := t.TempDir()
	tool := filepath.Join(root, "rg")
	script := "#!/bin/sh\n" + `i=0; while [ $i -lt 250 ]; do echo "f:$i:needle"; i=$((i+1)); done` + "\n"
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := searchWithRG(context.Background(), tool, root, "needle", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxSearchHits {
		t.Fatalf("rg returned %d hits, want %d", len(got), maxSearchHits)
	}
}

func TestTrashCommandArgvByOS(t *testing.T) {
	tests := []struct {
		goos, path, command string
		args                []string
		contains            string
	}{
		{goos: "darwin", path: `/tmp/a"b\c`, command: "osascript", args: []string{"-e", `tell application "Finder" to delete POSIX file "/tmp/a\"b\\c"`}},
		{goos: "linux", path: "/tmp/a b", command: "gio", args: []string{"trash", "--", "/tmp/a b"}},
		{goos: "windows", path: `C:\a'b`, command: "powershell.exe", contains: `C:\a''b`},
	}
	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			command, args, err := TrashCommand(tc.goos, tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if command != tc.command {
				t.Fatalf("command = %q, want %q", command, tc.command)
			}
			if tc.args != nil && strings.Join(args, "\x00") != strings.Join(tc.args, "\x00") {
				t.Fatalf("argv = %#v, want %#v", args, tc.args)
			}
			if tc.contains != "" && !strings.Contains(strings.Join(args, " "), tc.contains) {
				t.Fatalf("argv %#v missing escaped path %q", args, tc.contains)
			}
		})
	}
	if _, _, err := TrashCommand("plan9", "/tmp/a"); err == nil {
		t.Fatal("unsupported OS unexpectedly succeeded")
	}
}

func TestDeleteUsesInjectedTrashRunnerAndBlocksRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRunner := trashRunner
	defer func() { trashRunner = oldRunner }()
	var called string
	trashRunner = func(_ context.Context, path string) error { called = path; return nil }
	if _, err := Run(context.Background(), root, "delete", rawArgs(t, map[string]string{"path": "."})); err == nil {
		t.Fatal("deleting root unexpectedly succeeded")
	}
	if called != "" {
		t.Fatal("trash runner called for root")
	}
	if _, err := Run(context.Background(), root, "delete", rawArgs(t, map[string]string{"path": "notes"})); err != nil {
		t.Fatal(err)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if called != filepath.Join(rootReal, "notes") {
		t.Fatalf("trash path = %q", called)
	}
}

func TestCopyDirectory(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "source", "nested"))
	if err := os.WriteFile(filepath.Join(root, "source", "nested", "file.txt"), []byte("contents"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), root, "copy", rawArgs(t, map[string]string{"source": "source", "destination": "target"})); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "target", "nested", "file.txt"))
	if err != nil || string(got) != "contents" {
		t.Fatalf("copied content = %q, err = %v", got, err)
	}
}

func TestToolDescriptionsAndSummary(t *testing.T) {
	tools := Tools()
	if len(tools) != 9 {
		t.Fatalf("got %d tools", len(tools))
	}
	for _, tool := range tools {
		if len(tool.Parameters) == 0 || !json.Valid(tool.Parameters) {
			t.Errorf("%s has invalid schema", tool.Name)
		}
		wantWrite := tool.Name == "write_file" || tool.Name == "edit_file" || tool.Name == "move" || tool.Name == "copy" || tool.Name == "duplicate" || tool.Name == "delete"
		if tool.Write != wantWrite {
			t.Errorf("%s Write = %v, want %v", tool.Name, tool.Write, wantWrite)
		}
	}
	got := Summary("write_file", json.RawMessage(`{"path":"notes.md","content":"x"}`))
	if got != "Write notes.md" {
		t.Fatalf("summary = %q", got)
	}
}

func TestListDirEmptyFolder(t *testing.T) {
	got, err := Run(context.Background(), t.TempDir(), "list_dir", json.RawMessage(`{}`))
	if err != nil || got != "(empty folder)" {
		t.Fatalf("empty folder result = %q, %v", got, err)
	}
}

func rawArgs(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

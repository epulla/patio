package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type chatFile struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

func (s *Server) chatFileRoot(w http.ResponseWriter, r *http.Request) (chat, string, bool) {
	c, ok := s.chatForAttachments(w, r)
	if !ok {
		return chat{}, "", false
	}
	return c, c.Workdir, true
}

func cleanChatFilePath(rel string) (string, error) {
	if strings.ContainsRune(rel, 0) || filepath.IsAbs(filepath.FromSlash(rel)) {
		return "", errPathOutsideWorkdir
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == ".." {
			return "", errPathOutsideWorkdir
		}
	}
	return filepath.Clean(filepath.FromSlash(rel)), nil
}

func (s *Server) listChatFiles(w http.ResponseWriter, r *http.Request) {
	_, workdir, ok := s.chatFileRoot(w, r)
	if !ok {
		return
	}
	root, err := os.OpenRoot(workdir)
	if os.IsNotExist(err) && r.URL.Query().Get("path") == "" {
		write(w, http.StatusOK, []chatFile{})
		return
	}
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer root.Close()
	path, err := cleanChatFilePath(r.URL.Query().Get("path"))
	if err != nil {
		chatFileError(w, err)
		return
	}
	dir, err := root.Open(path)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		chatFileError(w, err)
		return
	}
	result := make([]chatFile, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		child := filepath.Join(path, entry.Name())
		info, err := root.Stat(child)
		if err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(r.URL.Query().Get("path"), entry.Name()))
		result = append(result, chatFile{Name: entry.Name(), Path: rel, Dir: info.IsDir(), Size: info.Size(), Modified: info.ModTime().UTC().Format(time.RFC3339)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Dir != result[j].Dir {
			return result[i].Dir
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	write(w, http.StatusOK, result)
}

func chatFileError(w http.ResponseWriter, err error) {
	if err == errPathOutsideWorkdir {
		errw(w, http.StatusBadRequest, err.Error())
	} else if os.IsNotExist(err) {
		errw(w, http.StatusNotFound, "file not found")
	} else {
		errw(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) rawChatFile(w http.ResponseWriter, r *http.Request) {
	_, workdir, ok := s.chatFileRoot(w, r)
	if !ok {
		return
	}
	root, err := os.OpenRoot(workdir)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer root.Close()
	path, err := cleanChatFilePath(r.URL.Query().Get("path"))
	if err != nil {
		chatFileError(w, err)
		return
	}
	f, err := root.Open(path)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		chatFileError(w, err)
		return
	}
	if info.IsDir() {
		errw(w, 400, "path is a directory")
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		switch ext {
		case ".md", ".markdown":
			contentType = "text/markdown"
		case ".csv":
			contentType = "text/csv"
		case ".tsv":
			contentType = "text/tab-separated-values"
		case ".yaml", ".yml":
			contentType = "text/yaml"
		case ".log":
			contentType = "text/plain"
		}
	}
	if contentType == "" {
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		contentType = http.DetectContentType(buf[:n])
		_, _ = f.Seek(0, io.SeekStart)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filepath.Base(path)}))
	if contentType != "application/pdf" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

func (s *Server) deleteChatFile(w http.ResponseWriter, r *http.Request) {
	c, workdir, ok := s.chatFileRoot(w, r)
	if !ok {
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		errw(w, 400, "cannot delete chat folder")
		return
	}
	root, err := os.OpenRoot(workdir)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer root.Close()
	rel, err = cleanChatFilePath(rel)
	if err != nil {
		chatFileError(w, err)
		return
	}
	if rel == "." {
		errw(w, 400, "cannot delete chat folder")
		return
	}
	// The parent is pinned as its own root, so the identity check and the delete see the same folder.
	parent, err := root.OpenRoot(filepath.Dir(rel))
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer parent.Close()
	base := filepath.Base(rel)
	// RemoveAll drops a final symlink itself, never its target.
	target, err := parent.Lstat(base)
	if err != nil {
		chatFileError(w, err)
		return
	}
	s.attachmentsMu.Lock()
	defer s.attachmentsMu.Unlock()
	// Compare by file identity, since a symlinked parent can alias the attachments folder.
	pending := false
	if attachments, e := root.Lstat("attachments"); e == nil && len(s.attachments[c.ID]) > 0 {
		_, named := s.attachments[c.ID][base]
		dir, e := parent.Stat(".")
		pending = os.SameFile(target, attachments) || named && e == nil && os.SameFile(dir, attachments)
	}
	if pending {
		errw(w, http.StatusConflict, "remove it from the message first")
		return
	}
	if err := parent.RemoveAll(base); err != nil {
		chatFileError(w, err)
		return
	}
	noContent(w)
}

func (s *Server) openChatFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		errw(w, 400, "invalid path")
		return
	}
	_, workdir, ok := s.chatFileRoot(w, r)
	if !ok {
		return
	}
	root, err := os.OpenRoot(workdir)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer root.Close()
	path, err := cleanChatFilePath(in.Path)
	if err != nil {
		chatFileError(w, err)
		return
	}
	info, err := root.Stat(path)
	if err != nil {
		chatFileError(w, err)
		return
	}
	if info.IsDir() {
		errw(w, 400, "path is a directory")
		return
	}
	resolved, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		chatFileError(w, err)
		return
	}
	// open follows the path by name.
	if err := s.openFile(filepath.Join(resolved, path)); err != nil {
		errw(w, 500, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) openChatFileWindow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		errw(w, 400, "invalid path")
		return
	}
	_, workdir, ok := s.chatFileRoot(w, r)
	if !ok {
		return
	}
	root, err := os.OpenRoot(workdir)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer root.Close()
	path, err := cleanChatFilePath(in.Path)
	if err != nil {
		chatFileError(w, err)
		return
	}
	info, err := root.Stat(path)
	if err != nil {
		chatFileError(w, err)
		return
	}
	if info.IsDir() {
		errw(w, 400, "path is a directory")
		return
	}
	if s.openFileWindow == nil {
		errw(w, 500, "file windows unavailable")
		return
	}
	if err := s.openFileWindow(r.PathValue("id"), filepath.ToSlash(in.Path)); err != nil {
		errw(w, 500, err.Error())
		return
	}
	noContent(w)
}

func (s *Server) listChatZip(w http.ResponseWriter, r *http.Request) {
	_, workdir, ok := s.chatFileRoot(w, r)
	if !ok {
		return
	}
	root, err := os.OpenRoot(workdir)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer root.Close()
	path, err := cleanChatFilePath(r.URL.Query().Get("path"))
	if err != nil {
		chatFileError(w, err)
		return
	}
	f, err := root.Open(path)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		chatFileError(w, err)
		return
	}
	z, err := zip.NewReader(f, info.Size())
	if err != nil {
		chatFileError(w, err)
		return
	}
	if len(z.File) > 5000 {
		errw(w, 413, "zip has too many entries")
		return
	}
	type item struct {
		Name string `json:"name"`
		Size uint64 `json:"size"`
		Dir  bool   `json:"dir"`
	}
	out := make([]item, 0, len(z.File))
	for _, f := range z.File {
		out = append(out, item{f.Name, f.UncompressedSize64, f.FileInfo().IsDir()})
	}
	write(w, 200, out)
}

func (s *Server) quickLookChatFile(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "darwin" {
		errw(w, 501, "QuickLook is only available on macOS")
		return
	}
	_, workdir, ok := s.chatFileRoot(w, r)
	if !ok {
		return
	}
	root, err := os.OpenRoot(workdir)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer root.Close()
	path, err := cleanChatFilePath(r.URL.Query().Get("path"))
	if err != nil {
		chatFileError(w, err)
		return
	}
	f, err := root.Open(path)
	if err != nil {
		chatFileError(w, err)
		return
	}
	defer f.Close()
	tmp, err := os.MkdirTemp("", "patio-quicklook-")
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	defer os.RemoveAll(tmp)
	copyPath := filepath.Join(tmp, filepath.Base(path))
	out, err := os.Create(copyPath)
	if err != nil {
		errw(w, 500, err.Error())
		return
	}
	if _, err = io.Copy(out, f); err != nil {
		out.Close()
		errw(w, 500, err.Error())
		return
	}
	if err := out.Close(); err != nil {
		errw(w, 500, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "qlmanage", "-p", "-o", tmp, copyPath)
	if err := cmd.Run(); err != nil {
		errw(w, 422, "no preview")
		return
	}
	preview := filepath.Join(tmp, filepath.Base(copyPath)+".qlpreview", "Preview.html")
	data, err := os.ReadFile(preview)
	if err != nil {
		errw(w, 422, "no preview")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox")
	_, _ = w.Write(data)
}

func openDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		// Not `cmd /c start`: cmd would parse &, | and ^ in the file name.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("unsupported operating system %q", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

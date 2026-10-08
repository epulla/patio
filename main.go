package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"my-tickets/internal/api"
	"my-tickets/internal/db"
	"my-tickets/web"
)

func main() {
	port := flag.Int("port", 0, "HTTP port, or 0 for a random free port")
	dbFile := flag.String("d", "", "database file path (default: per-OS data directory)")
	noBrowser := flag.Bool("no-browser", false, "do not open browser")
	showHidden := flag.Bool("show-hidden", false, "show comments and assignee in the UI")
	flag.Parse()
	home := ""
	if *dbFile == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
	}
	path, err := dbPath(*dbFile, runtime.GOOS, os.Getenv, home)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Fatal(err)
	}
	database, err := db.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()
	mime.AddExtensionType(".js", "text/javascript")

	mux := http.NewServeMux()
	mux.Handle("/api/", api.New(database, "local", *showHidden))
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	mux.HandleFunc("/", spaHandler)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	url := "http://" + listener.Addr().String()
	log.Printf("listening at %s", url)
	log.Printf("database at %s", path)
	if !*noBrowser {
		openBrowser(url)
	}
	if err := http.Serve(listener, mux); err != nil {
		log.Fatal(err)
	}
}

func dbPath(flagValue, goos string, getenv func(string) string, home string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}

	switch goos {
	case "windows":
		local := getenv("LOCALAPPDATA")
		if local == "" {
			return "", fmt.Errorf("LOCALAPPDATA is not set")
		}
		// Use Local instead of Roaming because SQLite WAL breaks on redirected or network Roaming profiles.
		return filepath.Join(local, "my-tickets", "my-tickets.db"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "my-tickets", "my-tickets.db"), nil
	default:
		xdg := getenv("XDG_DATA_HOME")
		if filepath.IsAbs(xdg) {
			return filepath.Join(xdg, "my-tickets", "my-tickets.db"), nil
		}
		return filepath.Join(home, ".local", "share", "my-tickets", "my-tickets.db"), nil
	}
}

func spaHandler(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		http.NotFound(w, r)
		return
	}
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusNotFound)
		return
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		http.Error(w, "frontend not built", http.StatusNotFound)
		return
	}
	requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requested == "" {
		requested = "."
	}
	if _, err := fs.Stat(sub, requested); err != nil {
		r.URL.Path = "/"
	}
	http.FileServerFS(sub).ServeHTTP(w, r)
}

func openBrowser(url string) {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{url}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		command, args = "xdg-open", []string{url}
	}
	if err := exec.Command(command, args...).Start(); err != nil {
		log.Printf("open browser: %v", err)
	}
}

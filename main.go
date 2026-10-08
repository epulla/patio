package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"my-tickets/internal/api"
	"my-tickets/internal/db"
	mcpserver "my-tickets/internal/mcp"
	"my-tickets/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		runMCP(os.Args[2:])
		return
	}
	dbFile := flag.String("d", "", "database file path (default: per-OS data directory)")
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
	mime.AddExtensionType(".js", "text/javascript")

	mux := http.NewServeMux()
	mux.Handle("/api/", api.New(database, "local", *showHidden))
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	var appCtx context.Context
	// WKWebView ignores window.print(); Wails exposes native printing only from Go.
	mux.HandleFunc("POST /api/print", func(w http.ResponseWriter, _ *http.Request) {
		wruntime.WindowPrint(appCtx)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", spaHandler)
	log.Printf("database at %s", path)
	runErr := wails.Run(&options.App{
		Title:     "My Tickets",
		Width:     1280,
		Height:    800,
		OnStartup: func(ctx context.Context) { appCtx = ctx },
		AssetServer: &assetserver.Options{
			Handler: mux,
		},
	})
	if err := database.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}

func runMCP(args []string) {
	flags := flag.NewFlagSet("mcp", flag.ExitOnError)
	dbFile := flags.String("d", "", "database file path (default: per-OS data directory)")
	if err := flags.Parse(args); err != nil {
		log.Fatal(err)
	}
	home := ""
	if *dbFile == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
	}
	dbFilePath, err := dbPath(*dbFile, runtime.GOOS, os.Getenv, home)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dbFilePath), 0o700); err != nil {
		log.Fatal(err)
	}
	database, err := db.Open(dbFilePath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()
	server := mcpserver.NewServer(api.New(database, "ai", true))
	if err := server.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
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

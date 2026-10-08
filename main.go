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
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"my-tickets/internal/api"
	"my-tickets/internal/db"
	mcpserver "my-tickets/internal/mcp"
	"my-tickets/internal/reminder"
	"my-tickets/web"
)

var version = "dev"

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
	exe, err := os.Executable()
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
	mux.Handle("/api/", api.New(database, "local", *showHidden, version))
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	mux.HandleFunc("GET /api/mcp", func(w http.ResponseWriter, r *http.Request) {
		args := []string{"mcp"}
		if *dbFile != "" {
			args = append(args, "-d", path)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		tools, latency, checkErr := mcpserver.Check(ctx, &sdk.CommandTransport{Command: exec.CommandContext(ctx, exe, "mcp", "-d", path)})
		result := struct {
			Command   string              `json:"command"`
			Args      []string            `json:"args"`
			OK        bool                `json:"ok"`
			Error     string              `json:"error"`
			Tools     []map[string]string `json:"tools"`
			LatencyMS int64               `json:"latency_ms"`
		}{Command: exe, Args: args, OK: checkErr == nil, Tools: []map[string]string{}, LatencyMS: latency.Milliseconds()}
		if checkErr != nil {
			result.Error = checkErr.Error()
		} else {
			for _, tool := range tools {
				result.Tools = append(result.Tools, map[string]string{"name": tool.Name, "description": tool.Description})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("write MCP response: %v", err)
		}
	})
	var appCtx context.Context
	var stopBackground context.CancelFunc
	var background sync.WaitGroup
	// WKWebView ignores window.print(); Wails exposes native printing only from Go.
	mux.HandleFunc("POST /api/print", func(w http.ResponseWriter, _ *http.Request) {
		wruntime.WindowPrint(appCtx)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", spaHandler)
	log.Printf("database at %s", path)
	runErr := wails.Run(&options.App{
		Title:  "My Tickets",
		Width:  1280,
		Height: 800,
		OnStartup: func(ctx context.Context) {
			appCtx = ctx
			backgroundCtx, cancel := context.WithCancel(ctx)
			stopBackground = cancel
			background.Add(2)
			go func() {
				defer background.Done()
				if err := db.Watch(backgroundCtx, database, time.Second, func() {
					wruntime.EventsEmit(ctx, "db-changed")
				}); err != nil {
					log.Printf("watch database changes: %v", err)
				}
			}()
			go func() {
				defer background.Done()
				if err := reminder.Run(backgroundCtx, database, 30*time.Second, func(title, message string) error {
					return beeep.Notify("Reminder: "+title, message, "")
				}); err != nil {
					log.Printf("run reminders: %v", err)
				}
			}()
		},
		AssetServer: &assetserver.Options{
			Handler: mux,
		},
	})
	if stopBackground != nil {
		stopBackground()
	}
	background.Wait()
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
	server := mcpserver.NewServer(api.New(database, "ai", true, version), version)
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

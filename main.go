package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	"sync/atomic"
	"time"

	"github.com/gen2brain/beeep"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"patio/internal/api"
	"patio/internal/db"
	mcpserver "patio/internal/mcp"
	"patio/internal/proc"
	"patio/internal/reminder"
	"patio/web"
)

var version = "dev"

//go:embed build/appicon.png
var appIcon []byte

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		runMCP(os.Args[2:])
		return
	}
	proc.LoadLoginShellPath()
	dbFile := flag.String("d", "", "database file path (default: per-OS data directory)")
	showHidden := flag.Bool("show-hidden", false, "show assignee in the UI")
	chatWindow := flag.String("chat", "", "open a chat in its own window")
	openPath := flag.String("open", "", "navigate main window to an app path")
	flag.Parse()
	home, err := os.UserHomeDir()
	if err != nil {
		if *dbFile == "" {
			log.Fatal(err)
		}
		log.Printf("configure log file: %v", err)
	}
	logFilePath := ""
	var logFile *os.File
	if err == nil {
		logFilePath, err = logPath(runtime.GOOS, os.Getenv, home)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(logFilePath), 0o700)
		}
		if err == nil && *chatWindow == "" {
			err = rotateLog(logFilePath)
		}
		if err == nil {
			logFile, err = os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		}
		if err != nil {
			log.Printf("configure log file: %v", err)
			logFilePath = ""
		} else {
			log.SetOutput(io.MultiWriter(os.Stderr, logFile))
			log.SetPrefix(fmt.Sprintf("[%d] ", os.Getpid()))
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
	chatTitle := ""
	if *chatWindow != "" {
		if err := database.QueryRow(`SELECT title FROM chat WHERE id=?`, *chatWindow).Scan(&chatTitle); err != nil {
			log.Fatalf("chat %q not found: %v", *chatWindow, err)
		}
	}
	windowID := api.NewWindowID()
	mime.AddExtensionType(".js", "text/javascript")

	var appCtx context.Context
	notes := &notifier{
		send: func(title, body, link string) error {
			return wruntime.SendNotification(appCtx, wruntime.NotificationOptions{
				ID: fmt.Sprint(time.Now().UnixNano()), Title: title, Body: body, Data: map[string]any{"link": link},
			})
		},
		fallback: func(title, body string) error { return beeep.Notify(title, body, "") },
	}
	mux := http.NewServeMux()
	patioToolNames, err := mcpserver.RegisteredToolNames()
	if err != nil {
		log.Printf("discover Patio MCP tool names for Copilot approvals: %v", err)
	}
	apiServer := api.New(database, "local", *showHidden, version, filepath.Dir(path), api.Options{
		DBPath:        path,
		PatioMCPTools: patioToolNames,
		ChatWindow:    *chatWindow,
		OpenPath: func() string {
			if *chatWindow == "" {
				return *openPath
			}
			return ""
		}(),
		WindowID:    windowID,
		LogPath:     logFilePath,
		StartWindow: func(args []string) error { return startWindow(exe, args) },
		FocusWindow: func() { wruntime.WindowUnminimise(appCtx); wruntime.WindowShow(appCtx) },
		Notify:      notes.Notify,
		Emit: func(event string, data any) {
			wruntime.EventsEmit(appCtx, event, data)
		},
	})
	mux.Handle("/api/", apiServer)
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
	var stopBackground context.CancelFunc
	var background sync.WaitGroup
	windowStop := make(chan struct{})
	// WKWebView ignores window.print(); Wails exposes native printing only from Go.
	mux.HandleFunc("POST /api/print", func(w http.ResponseWriter, _ *http.Request) {
		wruntime.WindowPrint(appCtx)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", spaHandler)
	log.Printf("database at %s", path)
	runErr := wails.Run(&options.App{
		Title:  appTitle(chatTitle),
		Width:  appWidth(*chatWindow),
		Height: 800,
		Linux: &linux.Options{
			Icon:             appIcon,
			ProgramName:      "patio",
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
		OnStartup: func(ctx context.Context) {
			appCtx = ctx
			if *chatWindow == "" {
				if err := api.CleanupStaleCopilotMCPConfigs(); err != nil {
					log.Printf("clean stale Copilot MCP configs: %v", err)
				}
			}
			showPath := func(path string) {
				wruntime.WindowUnminimise(ctx)
				wruntime.WindowShow(ctx)
				if path != "" {
					wruntime.EventsEmit(ctx, "navigate", path)
				}
			}
			go func() {
				if err := wruntime.InitializeNotifications(ctx); err != nil {
					log.Printf("native notifications unavailable, using beeep: %v", err)
					return
				}
				if !wruntime.IsNotificationAvailable(ctx) {
					log.Printf("native notifications unavailable, using beeep")
					return
				}
				wruntime.OnNotificationResponse(ctx, func(result wruntime.NotificationResult) {
					if result.Error != nil {
						log.Printf("native notification response: %v", result.Error)
						return
					}
					link, _ := result.Response.UserInfo["link"].(string)
					showPath(link)
				})
				ok, err := wruntime.RequestNotificationAuthorization(ctx)
				if !ok || err != nil {
					log.Printf("native notifications not authorized, using beeep: %v", err)
					return
				}
				notes.ready.Store(true)
			}()
			if err := apiServer.RegisterWindow(*chatWindow, os.Getpid()); err != nil {
				log.Printf("register app window: %v", err)
			}
			apiServer.RunWindowHeartbeat(windowStop, showPath)
			backgroundCtx, cancel := context.WithCancel(ctx)
			stopBackground = cancel
			if *chatWindow == "" {
				if err := apiServer.DeliverAgentMessages(backgroundCtx); err != nil {
					log.Printf("deliver queued agent messages at startup: %v", err)
				}
			}
			background.Add(1)
			go func() {
				defer background.Done()
				purge := func() {
					if err := apiServer.PurgeNotifications(); err != nil {
						log.Printf("purge notifications: %v", err)
					}
				}
				purge()
				ticker := time.NewTicker(24 * time.Hour)
				defer ticker.Stop()
				for {
					select {
					case <-backgroundCtx.Done():
						return
					case <-ticker.C:
						purge()
					}
				}
			}()
			background.Add(1)
			go func() {
				defer background.Done()
				if err := db.Watch(backgroundCtx, database, time.Second, func() {
					wruntime.EventsEmit(ctx, "db-changed")
					if *chatWindow == "" {
						if err := apiServer.DeliverAgentMessages(backgroundCtx); err != nil {
							log.Printf("deliver queued agent messages after database change: %v", err)
						}
					}
				}); err != nil {
					log.Printf("watch database changes: %v", err)
				}
			}()
			if *chatWindow == "" {
				background.Add(4)
				go func() {
					defer background.Done()
					if err := apiServer.StartScheduledRunner(backgroundCtx, 30*time.Second); err != nil {
						log.Printf("run scheduled tasks: %v", err)
					}
				}()
				go func() {
					defer background.Done()
					if err := apiServer.StartMonitorRunner(backgroundCtx, 30*time.Second); err != nil {
						log.Printf("run monitors: %v", err)
					}
				}()
				go func() {
					defer background.Done()
					if err := apiServer.StartHealthMonitor(backgroundCtx, 5*time.Minute); err != nil {
						log.Printf("monitor health: %v", err)
					}
				}()
				go func() {
					defer background.Done()
					if err := reminder.Run(backgroundCtx, database, 30*time.Second, func(item reminder.Item, message string) error {
						title := item.Title
						if item.Kind == "reminder" {
							title = "Reminder: " + title
						}
						if err := apiServer.Notify("calendar", title, message, "/calendar", true); err != nil {
							return err
						}
						return nil
					}); err != nil {
						log.Printf("run reminders: %v", err)
					}
				}()
			}
		},
		OnDomReady: func(ctx context.Context) {
			if *chatWindow == "" {
				return
			}
			screens, err := wruntime.ScreenGetAll(ctx)
			if err != nil {
				log.Printf("get screens for chat window: %v", err)
				return
			}
			var screen *wruntime.Screen
			for i := range screens {
				if screens[i].IsCurrent {
					screen = &screens[i]
					break
				}
			}
			if screen == nil {
				for i := range screens {
					if screens[i].IsPrimary {
						screen = &screens[i]
						break
					}
				}
			}
			if screen == nil {
				log.Printf("no current or primary screen for chat window")
				return
			}
			n, err := apiServer.LiveChatWindowCount()
			if err != nil {
				log.Printf("count live chat windows: %v", err)
				return
			}
			// burnedout: assumed right-edge cascade, 32px step, wraps after 5
			n %= 5
			x := screen.Size.Width - 900 - 24 - 32*n
			y := 40 + 32*n
			if x < 0 {
				x = 0
			}
			if y < 0 {
				y = 0
			}
			wruntime.WindowSetPosition(ctx, x, y)
		},
		OnShutdown: func(ctx context.Context) {
			wruntime.CleanupNotifications(ctx)
			close(windowStop)
			if err := apiServer.UnregisterWindow(); err != nil {
				log.Printf("unregister app window: %v", err)
			}
			if err := apiServer.Close(); err != nil {
				log.Printf("close chat processes: %v", err)
			}
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

func appTitle(chatTitle string) string {
	if chatTitle != "" {
		return chatTitle
	}
	return "Patio"
}
func appWidth(chatID string) int {
	if chatID != "" {
		return 900
	}
	return 1280
}
func startWindow(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// notifier sends through Wails once authorized and falls back to beeep otherwise.
type notifier struct {
	ready    atomic.Bool
	send     func(title, body, link string) error
	fallback func(title, body string) error
}

func (n *notifier) Notify(title, body, link string) error {
	if n.ready.Load() {
		err := n.send(title, body, link)
		if err == nil {
			return nil
		}
		log.Printf("native notification failed, using beeep: %v", err)
	}
	return n.fallback(title, body)
}

func runMCP(args []string) {
	flags := flag.NewFlagSet("mcp", flag.ExitOnError)
	dbFile := flags.String("d", "", "database file path (default: per-OS data directory)")
	approveChat := flags.String("approve-chat", "", "enable Claude approval tool for chat ID")
	inApp := flags.Bool("in-app", false, "omit chat persona from MCP instructions")
	if err := flags.Parse(args); err != nil {
		log.Fatal(err)
	}
	approveEnabled := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "approve-chat" {
			approveEnabled = true
		}
	})
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
	handler := api.New(database, "ai", true, version, filepath.Dir(dbFilePath), api.Options{DBPath: dbFilePath})
	var server *sdk.Server
	if approveEnabled {
		if *approveChat == "" {
			log.Fatal("-approve-chat requires a chat ID")
		}
		exists, err := api.ChatExists(database, *approveChat)
		if err != nil {
			log.Fatalf("validate approval chat %q: %v", *approveChat, err)
		}
		if !exists {
			log.Fatalf("approval chat %q not found", *approveChat)
		}
		server, err = mcpserver.NewServerWithApproval(handler, version, mcpserver.ApprovalOptions{
			DB: database, ChatID: *approveChat, Timeout: 10 * time.Minute, PollInterval: 300 * time.Millisecond, InApp: *inApp,
		})
	} else {
		server, err = mcpserver.NewServer(handler, version, database, *inApp)
	}
	if err != nil {
		log.Fatalf("create MCP server: %v", err)
	}
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
		return filepath.Join(local, "patio", "patio.db"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "patio", "patio.db"), nil
	default:
		xdg := getenv("XDG_DATA_HOME")
		if path.IsAbs(xdg) {
			return filepath.Join(xdg, "patio", "patio.db"), nil
		}
		return filepath.Join(home, ".local", "share", "patio", "patio.db"), nil
	}
}

func logPath(goos string, getenv func(string) string, home string) (string, error) {
	switch goos {
	case "windows":
		local := getenv("LOCALAPPDATA")
		if local == "" {
			return "", fmt.Errorf("LOCALAPPDATA is not set")
		}
		return filepath.Join(local, "patio", "logs", "patio.log"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Logs", "patio", "patio.log"), nil
	default:
		xdg := getenv("XDG_STATE_HOME")
		if path.IsAbs(xdg) {
			return filepath.Join(xdg, "patio", "patio.log"), nil
		}
		return filepath.Join(home, ".local", "state", "patio", "patio.log"), nil
	}
}

func rotateLog(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) || err == nil && info.Size() <= 5*1024*1024 {
		return nil
	}
	if err != nil {
		return err
	}
	for i := 3; i >= 1; i-- {
		old := path
		if i > 1 {
			old = fmt.Sprintf("%s.%d", path, i-1)
		}
		if i == 3 {
			if err := os.Remove(fmt.Sprintf("%s.3", path)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if _, err := os.Stat(old); err == nil {
			if err := os.Rename(old, fmt.Sprintf("%s.%d", path, i)); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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

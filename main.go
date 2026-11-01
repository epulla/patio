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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gen2brain/beeep"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
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

	notes := &notifier{fallback: func(title, body string) error { return beeep.Notify(title, body, "") }}
	nsvc := notifications.New()
	notes.send = func(title, body, link string) error {
		return nsvc.SendNotification(notifications.NotificationOptions{
			ID: fmt.Sprint(time.Now().UnixNano()), Title: title, Body: body, Data: map[string]any{"link": link},
		})
	}
	notes.check = nsvc.CheckNotificationAuthorization
	var app *application.App
	var mainWindow *application.WebviewWindow
	showPath := func(path string) {
		mainWindow.UnMinimise()
		mainWindow.Show()
		mainWindow.Focus()
		if path != "" {
			app.Event.Emit("navigate", path)
		}
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
		WindowID:             windowID,
		LogPath:              logFilePath,
		StartWindow:          func(args []string) error { return startWindow(exe, args) },
		FocusWindow:          func() { showPath("") },
		Notify:               notes.Notify,
		NotificationsBlocked: notes.Blocked,
		Emit: func(event string, data any) {
			app.Event.Emit(event, data)
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
	mux.HandleFunc("POST /api/print", func(w http.ResponseWriter, r *http.Request) {
		window := app.Window.Current()
		if id, err := strconv.ParseUint(r.Header.Get("x-wails-window-id"), 10, 0); err == nil {
			if byID, ok := app.Window.GetByID(uint(id)); ok {
				window = byID
			}
		}
		if window != nil {
			if err := window.Print(); err != nil {
				log.Printf("print window: %v", err)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", spaHandler)
	log.Printf("database at %s", path)
	app = application.New(application.Options{
		Name:   "Patio",
		Icon:   appIcon,
		Assets: application.AssetOptions{Handler: mux},
		Mac:    application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		Linux:  application.LinuxOptions{ProgramName: "patio"},
		OnShutdown: func() {
			if stopBackground != nil {
				stopBackground()
			}
			if err := nsvc.ServiceShutdown(); err != nil {
				log.Printf("shut down native notifications: %v", err)
			}
			close(windowStop)
			if err := apiServer.UnregisterWindow(); err != nil {
				log.Printf("unregister app window: %v", err)
			}
			if err := apiServer.Close(); err != nil {
				log.Printf("close chat processes: %v", err)
			}
		},
	})
	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  appTitle(chatTitle),
		Width:  appWidth(*chatWindow),
		Height: 800,
		URL:    "/",
		Linux:  application.LinuxWindow{Icon: appIcon, WebviewGpuPolicy: application.WebviewGpuPolicyNever},
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		ctx := app.Context()
		if *chatWindow == "" {
			if err := api.CleanupStaleCopilotMCPConfigs(); err != nil {
				log.Printf("clean stale Copilot MCP configs: %v", err)
			}
		} else {
			positionChatWindow(mainWindow, apiServer)
		}
		go func() {
			if err := nsvc.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
				log.Printf("native notifications unavailable, using beeep: %v", err)
				return
			}
			nsvc.OnNotificationResponse(func(result notifications.NotificationResult) {
				if result.Error != nil {
					log.Printf("native notification response: %v", result.Error)
					return
				}
				link, _ := result.Response.UserInfo["link"].(string)
				showPath(link)
			})
			ok, err := nsvc.RequestNotificationAuthorization()
			notes.asked.Store(runtime.GOOS == "darwin")
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
				app.Event.Emit("db-changed")
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
			background.Add(6)
			go func() {
				defer background.Done()
				if err := apiServer.StartScheduledRunner(backgroundCtx, 30*time.Second); err != nil {
					log.Printf("run scheduled tasks: %v", err)
				}
			}()
			go func() {
				defer background.Done()
				if err := apiServer.StartHandoffSweeper(backgroundCtx, 30*time.Second); err != nil {
					log.Printf("sweep chat handoffs: %v", err)
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
				if err := apiServer.StartUpdateChecker(backgroundCtx, 6*time.Hour); err != nil {
					log.Printf("check for update: %v", err)
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
	})
	runErr := app.Run()
	background.Wait()
	if err := database.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}

// positionChatWindow cascades chat windows from the right edge of their screen.
func positionChatWindow(window *application.WebviewWindow, apiServer *api.Server) {
	screen, err := window.GetScreen()
	if err != nil || screen == nil {
		log.Printf("get screen for chat window: %v", err)
		return
	}
	n, err := apiServer.LiveChatWindowCount()
	if err != nil {
		log.Printf("count live chat windows: %v", err)
		return
	}
	// burnedout: assumed right-edge cascade, 32px step, wraps after 5
	n %= 5
	window.SetRelativePosition(max(screen.WorkArea.Width-900-24-32*n, 0), 40+32*n)
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
	asked    atomic.Bool
	send     func(title, body, link string) error
	fallback func(title, body string) error
	check    func() (bool, error)
}

// Blocked re-checks macOS authorization once Patio has asked for it, so allowing
// notifications in System Settings switches sending back to Wails.
func (n *notifier) Blocked() bool {
	if !n.asked.Load() {
		return false
	}
	ok, err := n.check()
	if err != nil {
		log.Printf("check notification authorization: %v", err)
	}
	n.ready.Store(ok)
	return !ok
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
	chatID := flags.String("chat", "", "chat ID whose bot is credited in change notifications")
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
	if *chatID == "" {
		*chatID = *approveChat
	}
	actor := ""
	if *chatID != "" {
		if actor, err = api.ChatBotName(database, *chatID); err != nil {
			log.Fatalf("look up chat bot %q: %v", *chatID, err)
		}
	}
	handler := api.New(database, "ai", true, version, filepath.Dir(dbFilePath), api.Options{DBPath: dbFilePath, MCPActor: actor})
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

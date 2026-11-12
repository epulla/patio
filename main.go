package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
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

func throttle(every time.Duration, fn func()) func() {
	var mu sync.Mutex
	var last time.Time
	pending := false
	return func() {
		mu.Lock()
		if last.IsZero() || time.Since(last) >= every {
			last = time.Now()
			mu.Unlock()
			fn()
			return
		}
		if !pending {
			pending = true
			time.AfterFunc(time.Until(last.Add(every)), func() {
				mu.Lock()
				pending = false
				last = time.Now()
				mu.Unlock()
				fn()
			})
		}
		mu.Unlock()
	}
}

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
		if err == nil {
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
	wins := &windows{db: database, chats: map[string]*application.WebviewWindow{}}
	mux := http.NewServeMux()
	patioToolNames, err := mcpserver.RegisteredToolNames()
	if err != nil {
		log.Printf("discover Patio MCP tool names for Copilot approvals: %v", err)
	}
	apiServer := api.New(database, "local", *showHidden, version, filepath.Dir(path), api.Options{
		DBPath:               path,
		PatioMCPTools:        patioToolNames,
		WindowID:             windowID,
		LogPath:              logFilePath,
		OpenChatWindow:       wins.openChat,
		OpenFileWindow:       wins.openFile,
		ShowMain:             wins.showMain,
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
	// Read once: Wails can't change this after startup.
	quitOnLastClose := !api.KeepRunningOnClose(database)
	app = application.New(application.Options{
		Name:   "Patio",
		Icon:   appIcon,
		Assets: application.AssetOptions{Handler: mux},
		Mac:    application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: quitOnLastClose},
		Linux:  application.LinuxOptions{ProgramName: "patio"},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceID(path),
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				wins.showMain(openPathArg(data.Args))
			},
		},
		OnShutdown: func() {
			wins.quitting.Store(true)
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
	wins.mu.Lock()
	wins.app = app
	wins.mu.Unlock()
	wins.showMain(*openPath)
	// Dock click brings the main window back, whether it was closed or hidden.
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		wins.showMain("")
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		ctx := app.Context()
		if err := api.CleanupStaleCopilotMCPConfigs(); err != nil {
			log.Printf("clean stale Copilot MCP configs: %v", err)
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
				wins.showMain(link)
			})
			ok, err := nsvc.RequestNotificationAuthorization()
			notes.asked.Store(runtime.GOOS == "darwin")
			if !ok || err != nil {
				log.Printf("native notifications not authorized, using beeep: %v", err)
				return
			}
			notes.ready.Store(true)
		}()
		if err := apiServer.RegisterWindow(os.Getpid()); err != nil {
			log.Printf("register app window: %v", err)
		}
		apiServer.RunWindowHeartbeat(windowStop)
		backgroundCtx, cancel := context.WithCancel(ctx)
		stopBackground = cancel
		if err := apiServer.DeliverAgentMessages(backgroundCtx); err != nil {
			log.Printf("deliver queued agent messages at startup: %v", err)
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
			emitDBChanged := throttle(2*time.Second, func() {
				if backgroundCtx.Err() == nil {
					app.Event.Emit("db-changed")
				}
			})
			if err := db.Watch(backgroundCtx, database, time.Second, func() {
				emitDBChanged()
				if err := apiServer.ApplyMCPReloadRequest(); err != nil {
					log.Printf("reload MCP servers on request: %v", err)
				}
				if err := apiServer.DeliverAgentMessages(backgroundCtx); err != nil {
					log.Printf("deliver queued agent messages after database change: %v", err)
				}
			}); err != nil {
				log.Printf("watch database changes: %v", err)
			}
		}()
		background.Add(6)
		background.Add(1)
		go func() {
			defer background.Done()
			if err := apiServer.StartTelegramBridge(backgroundCtx); err != nil {
				log.Printf("run Telegram bridge: %v", err)
			}
		}()
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

// windows tracks the main window and one window per popped-out chat, all in this process.
type windows struct {
	app   *application.App
	db    *sql.DB
	mu    sync.Mutex
	main  *application.WebviewWindow
	chats map[string]*application.WebviewWindow
	// quitting lets the app's own shutdown close the main window instead of hiding it.
	quitting atomic.Bool
}

// showMain focuses the main window, or reopens it after it was closed, at path when set.
// Invalid paths fall back to "/": Wails treats an unparsable start URL as fatal.
func (w *windows) showMain(path string) {
	if !api.ValidAppPath(path) {
		path = ""
	}
	w.mu.Lock()
	if w.app == nil {
		// A second launch arrived before startup finished; the main window is about to open anyway.
		w.mu.Unlock()
		return
	}
	if win := w.main; win != nil {
		app := w.app
		w.mu.Unlock()
		focusWindow(win)
		if path != "" {
			// Broadcast; chat windows ignore navigate.
			app.Event.Emit("navigate", path)
		}
		return
	}
	defer w.mu.Unlock()
	start := "/"
	if path != "" {
		start = path
	}
	// Created under the lock so concurrent calls can't open two; close hooks run off the main thread, so this can't deadlock.
	win := w.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Patio",
		Width:  1280,
		Height: 800,
		URL:    start,
		Linux:  application.LinuxWindow{Icon: appIcon, WebviewGpuPolicy: application.WebviewGpuPolicyNever},
	})
	w.main = win
	// A hook runs synchronously before Wails' own close listener destroys the window.
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		// Keep running so chats, scheduled tasks and bridges continue. Without a tray, other
		// platforms minimize so the window stays reachable from the taskbar.
		if !w.quitting.Load() && api.KeepRunningOnClose(w.db) {
			event.Cancel()
			if runtime.GOOS == "darwin" {
				win.Hide()
			} else {
				win.Minimise()
			}
			return
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.main == win {
			w.main = nil
		}
	})
}

// openChat focuses the chat's window or opens one, cascading from the right edge of the screen.
func (w *windows) openChat(id string) error {
	w.mu.Lock()
	if w.app == nil {
		w.mu.Unlock()
		return errors.New("app is starting")
	}
	if win, ok := w.chats[id]; ok {
		w.mu.Unlock()
		focusWindow(win)
		return nil
	}
	defer w.mu.Unlock()
	var title string
	if err := w.db.QueryRow(`SELECT title FROM chat WHERE id=?`, id).Scan(&title); err != nil {
		return err
	}
	screen := w.app.Screen.GetPrimary()
	if w.main != nil {
		if current, err := w.main.GetScreen(); err == nil && current != nil {
			screen = current
		}
	}
	options := application.WebviewWindowOptions{
		Name:   "chat-" + id,
		Title:  title,
		Width:  900,
		Height: 800,
		URL:    "/ai/chats/" + url.PathEscape(id) + "?window=chat",
		Linux:  application.LinuxWindow{Icon: appIcon, WebviewGpuPolicy: application.WebviewGpuPolicyNever},
	}
	if screen != nil {
		// burnedout: assumed right-edge cascade, 32px step, wraps after 5
		n := len(w.chats) % 5
		options.Screen = screen
		options.InitialPosition = application.WindowXY
		options.X = max(screen.WorkArea.Width-900-24-32*n, 0)
		options.Y = 40 + 32*n
	}
	win := w.app.Window.NewWithOptions(options)
	w.chats[id] = win
	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.chats[id] == win {
			delete(w.chats, id)
		}
	})
	return nil
}

func (w *windows) openFile(chatID, rel string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.app == nil {
		return errors.New("app is starting")
	}
	w.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: filepath.Base(rel), Width: 1100, Height: 800,
		URL:   "/ai/chats/" + url.PathEscape(chatID) + "/files?window=file&path=" + url.QueryEscape(rel),
		Linux: application.LinuxWindow{Icon: appIcon, WebviewGpuPolicy: application.WebviewGpuPolicyNever},
	})
	return nil
}

func focusWindow(win *application.WebviewWindow) {
	win.UnMinimise()
	win.Show()
	win.Focus()
}

// singleInstanceID keys the lock by database so throwaway -d runs start their own app.
func singleInstanceID(dbPath string) string {
	sum := sha256.Sum256([]byte(dbPath))
	return "io.github.epulla.patio." + hex.EncodeToString(sum[:8])
}

// openPathArg returns the -open value from a second launch's arguments.
func openPathArg(args []string) string {
	flags := flag.NewFlagSet("second-instance", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.String("d", "", "")
	flags.Bool("show-hidden", false, "")
	open := flags.String("open", "", "")
	if len(args) > 0 {
		_ = flags.Parse(args[1:])
	}
	return *open
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
			DB: database, ChatID: *approveChat, ProcessChatID: *chatID, Timeout: 10 * time.Minute, PollInterval: 300 * time.Millisecond, InApp: *inApp,
		})
	} else {
		server, err = mcpserver.NewServerWithChat(handler, version, database, *inApp, *chatID)
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

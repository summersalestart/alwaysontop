package main

import (
	"context"
	"log"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// WindowInfo mirrors the shape returned by the Python engine's
// "list_windows" action. Wails automatically generates a matching
// TypeScript/JS type for this when bindings are built.
type WindowInfo struct {
	HWND        int64  `json:"hwnd"`
	Title       string `json:"title"`
	PID         int64  `json:"pid"`
	ProcessName string `json:"process_name"`
	Topmost     bool   `json:"topmost"`
}

// ListWindowsResult is returned to the frontend by ListWindows.
type ListWindowsResult struct {
	Success bool         `json:"success"`
	Windows []WindowInfo `json:"windows"`
	Error   string       `json:"error,omitempty"`
}

// SetTopmostResult is returned to the frontend by SetTopmost.
type SetTopmostResult struct {
	Success bool   `json:"success"`
	HWND    int64  `json:"hwnd"`
	Topmost bool   `json:"topmost"`
	Error   string `json:"error,omitempty"`
}

// App is the Wails-bound application struct. Every exported method on
// App becomes callable from JavaScript as window.go.main.App.<Method>().
type App struct {
	ctx    context.Context
	engine *PythonEngine
}

// NewApp constructs the App and its Python engine manager. The engine
// process itself is started in OnStartup once a Wails context exists.
func NewApp() *App {
	return &App{
		engine: NewPythonEngine(),
	}
}

// OnStartup is called by Wails once the native window/webview is ready.
func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx
	if err := a.engine.Start(); err != nil {
		log.Printf("[app] failed to start python engine: %v", err)
		runtime.EventsEmit(ctx, "engine:fatal", err.Error())
	}
}

// OnShutdown is called by Wails as the application is closing.
func (a *App) OnShutdown(_ context.Context) {
	a.engine.Stop()
}

// ListWindows asks the Python engine for the current set of visible,
// user-relevant top-level windows.
func (a *App) ListWindows() ListWindowsResult {
	response, err := a.engine.Call("list_windows", nil)
	if err != nil {
		return ListWindowsResult{Success: false, Error: err.Error()}
	}
	return decodeListWindows(response)
}

// SetTopmost applies (value=true) or removes (value=false) the
// always-on-top state for the given window handle.
func (a *App) SetTopmost(hwnd int64, value bool) SetTopmostResult {
	response, err := a.engine.Call("set_topmost", map[string]interface{}{
		"hwnd":  hwnd,
		"value": value,
	})
	if err != nil {
		return SetTopmostResult{Success: false, HWND: hwnd, Error: err.Error()}
	}
	return decodeSetTopmost(response, hwnd)
}

// MinimizeWindow minimizes the application's own native window (bound to
// the custom title bar's minimize button).
func (a *App) MinimizeWindow() {
	runtime.WindowMinimise(a.ctx)
}

// CloseWindow closes the application (bound to the custom title bar's
// close button).
func (a *App) CloseWindow() {
	runtime.Quit(a.ctx)
}

// --- response decoding helpers -------------------------------------------------

func decodeListWindows(response map[string]interface{}) ListWindowsResult {
	if success, _ := response["success"].(bool); !success {
		return ListWindowsResult{Success: false, Error: extractError(response)}
	}

	rawWindows, _ := response["windows"].([]interface{})
	windows := make([]WindowInfo, 0, len(rawWindows))
	for _, raw := range rawWindows {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		windows = append(windows, WindowInfo{
			HWND:        toInt64(m["hwnd"]),
			Title:       toString(m["title"]),
			PID:         toInt64(m["pid"]),
			ProcessName: toString(m["process_name"]),
			Topmost:     toBool(m["topmost"]),
		})
	}
	return ListWindowsResult{Success: true, Windows: windows}
}

func decodeSetTopmost(response map[string]interface{}, requestedHwnd int64) SetTopmostResult {
	if success, _ := response["success"].(bool); !success {
		return SetTopmostResult{Success: false, HWND: requestedHwnd, Error: extractError(response)}
	}
	return SetTopmostResult{
		Success: true,
		HWND:    toInt64(response["hwnd"]),
		Topmost: toBool(response["topmost"]),
	}
}

func extractError(response map[string]interface{}) string {
	if msg, ok := response["error"].(string); ok && msg != "" {
		return msg
	}
	return "Unknown engine error."
}

func toInt64(v interface{}) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toBool(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// PythonEngine owns the lifecycle of the Python backend child process and
// implements the JSON-lines request/response protocol described in
// backend/ipc.py:
//
//	stdin  -> one JSON request object per line
//	stdout -> one JSON response object per line (nothing else is ever
//	          written there by the Python side)
//	stderr -> logs only, forwarded to this process's own log
//
// Requests are tagged with a numeric "id" so responses can be correlated
// even if multiple calls are in flight concurrently (e.g. a background
// refresh overlapping with a user-triggered Apply click).
type PythonEngine struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	nextID  int64
	pending map[int64]chan map[string]interface{}

	pendingMu sync.Mutex

	restartAttempts int
	stopped         atomic.Bool
}

const (
	requestTimeout    = 5 * time.Second
	maxRestartBackoff = 10 * time.Second
)

// NewPythonEngine creates the manager but does not start the process yet.
func NewPythonEngine() *PythonEngine {
	return &PythonEngine{
		pending: make(map[int64]chan map[string]interface{}),
	}
}

// Start launches the Python backend and begins reading its stdout.
// If the process later exits unexpectedly, it is automatically restarted
// with a small backoff, up to a reasonable number of attempts.
func (p *PythonEngine) Start() error {
	p.stopped.Store(false)
	return p.spawn()
}

func (p *PythonEngine) spawn() error {
	name, args, err := resolveEngineCommand()
	if err != nil {
		return fmt.Errorf("could not locate python engine: %w", err)
	}

	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to open engine stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to open engine stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to open engine stderr: %w", err)
	}

	hideConsoleWindow(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start python engine (%s %v): %w", name, args, err)
	}

	p.mu.Lock()
	p.cmd = cmd
	p.stdin = stdin
	p.mu.Unlock()

	go p.readStdout(stdout)
	go p.readStderr(stderr)
	go p.watchProcess(cmd)

	log.Printf("[pyengine] started: %s %v (pid=%d)", name, args, cmd.Process.Pid)
	return nil
}

// watchProcess waits for the child process to exit and restarts it
// (unless Stop() was called deliberately), so a crashed engine doesn't
// permanently break the app for the rest of the session.
func (p *PythonEngine) watchProcess(cmd *exec.Cmd) {
	err := cmd.Wait()

	if p.stopped.Load() {
		log.Printf("[pyengine] process stopped intentionally")
		return
	}

	log.Printf("[pyengine] process exited unexpectedly: %v", err)
	p.failAllPending(fmt.Errorf("python engine crashed: %w", err))

	p.restartAttempts++
	backoff := time.Duration(p.restartAttempts) * 500 * time.Millisecond
	if backoff > maxRestartBackoff {
		backoff = maxRestartBackoff
	}
	log.Printf("[pyengine] restarting in %s (attempt %d)", backoff, p.restartAttempts)
	time.Sleep(backoff)

	if respawnErr := p.spawn(); respawnErr != nil {
		log.Printf("[pyengine] restart failed: %v", respawnErr)
	} else {
		p.restartAttempts = 0
	}
}

func (p *PythonEngine) readStdout(stdout io.ReadCloser) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var response map[string]interface{}
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			log.Printf("[pyengine] ignoring non-JSON stdout line: %q", line)
			continue
		}

		p.routeResponse(response)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[pyengine] stdout scanner error: %v", err)
	}
}

func (p *PythonEngine) readStderr(stderr io.ReadCloser) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		log.Printf("[pyengine:stderr] %s", scanner.Text())
	}
}

func (p *PythonEngine) routeResponse(response map[string]interface{}) {
	idVal, ok := response["id"]
	if !ok {
		log.Printf("[pyengine] response without id, dropping: %v", response)
		return
	}

	// JSON numbers decode as float64.
	idFloat, ok := idVal.(float64)
	if !ok {
		log.Printf("[pyengine] response id was not numeric: %v", response)
		return
	}
	id := int64(idFloat)

	p.pendingMu.Lock()
	ch, exists := p.pending[id]
	if exists {
		delete(p.pending, id)
	}
	p.pendingMu.Unlock()

	if !exists {
		log.Printf("[pyengine] no pending caller for response id=%d (already timed out?)", id)
		return
	}
	ch <- response
}

func (p *PythonEngine) failAllPending(err error) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	for id, ch := range p.pending {
		ch <- map[string]interface{}{"success": false, "error": err.Error()}
		delete(p.pending, id)
	}
}

// Call sends a request to the Python engine and blocks until a matching
// response arrives or requestTimeout elapses.
func (p *PythonEngine) Call(action string, payload map[string]interface{}) (map[string]interface{}, error) {
	id := atomic.AddInt64(&p.nextID, 1)

	request := map[string]interface{}{"action": action, "id": id}
	for k, v := range payload {
		request[k] = v
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	ch := make(chan map[string]interface{}, 1)
	p.pendingMu.Lock()
	p.pending[id] = ch
	p.pendingMu.Unlock()

	p.mu.Lock()
	stdin := p.stdin
	p.mu.Unlock()

	if stdin == nil {
		return nil, fmt.Errorf("python engine is not running")
	}

	if _, err := stdin.Write(append(body, '\n')); err != nil {
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
		return nil, fmt.Errorf("failed to write request to engine: %w", err)
	}

	select {
	case response := <-ch:
		return response, nil
	case <-time.After(requestTimeout):
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
		return nil, fmt.Errorf("timed out waiting for engine response to %q", action)
	}
}

// Stop terminates the Python engine process cleanly.
func (p *PythonEngine) Stop() {
	p.stopped.Store(true)

	p.mu.Lock()
	cmd := p.cmd
	stdin := p.stdin
	p.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// resolveEngineCommand decides how to launch the Python backend:
//  1. ALWAYSONTOP_PY_ENGINE env var, if set, is used verbatim as the
//     executable path (useful for custom deployments / debugging).
//  2. The embedded, PyInstaller-packaged engine (production builds
//     only), extracted to %AppData%\AlwaysOnTop\backend - see
//     pyengine_extract.go / pyengine_embed_prod.go.
//  3. A manually placed "AlwaysOnTopEngine.exe" sitting next to this
//     application's own executable, for custom/manual deployments that
//     don't rely on the embedded copy.
//  4. Falling back to a local Python interpreter running
//     backend/engine.py relative to the working directory (development).
func resolveEngineCommand() (string, []string, error) {
	if override := os.Getenv("ALWAYSONTOP_PY_ENGINE"); override != "" {
		return override, nil, nil
	}

	if extracted, err := extractedEnginePath(); err == nil {
		return extracted, nil, nil
	}

	if exePath, err := os.Executable(); err == nil {
		bundled := filepath.Join(filepath.Dir(exePath), "backend", exeSuffix("AlwaysOnTopEngine"))
		if fileExists(bundled) {
			return bundled, nil, nil
		}
	}

	scriptPath := filepath.Join("backend", "engine.py")
	if !fileExists(scriptPath) {
		return "", nil, fmt.Errorf(
			"no embedded/bundled engine found and %s not found; "+
				"in production, build with build.bat (which embeds a packaged "+
				"engine into the .exe); in development, run from the project "+
				"root with a local Python interpreter available",
			scriptPath,
		)
	}

	for _, interpreter := range []string{"python", "python3"} {
		if _, err := exec.LookPath(interpreter); err == nil {
			return interpreter, []string{scriptPath}, nil
		}
	}

	return "", nil, fmt.Errorf("no python interpreter found on PATH to run %s", scriptPath)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func exeSuffix(name string) string {
	if isWindows() {
		return name + ".exe"
	}
	return name
}

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// appDataDir returns %AppData%\AlwaysOnTop, creating it if necessary.
// This is the single, per-user location for every file the running app
// needs beyond its own .exe: the extracted Python engine binary (see
// pyengine_extract.go) and the app's log file. Nothing is ever written
// back into the install directory (e.g. Program Files), which a normal
// user account typically cannot write to anyway.
func appDataDir() (string, error) {
	base, err := os.UserConfigDir() // resolves to %AppData% on Windows
	if err != nil {
		return "", fmt.Errorf("could not resolve %%AppData%%: %w", err)
	}
	dir := filepath.Join(base, "AlwaysOnTop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", dir, err)
	}
	return dir, nil
}

// setupLogging redirects the standard logger to
// %AppData%\AlwaysOnTop\logs\app.log. The returned file should be
// closed by the caller (main) on shutdown. If %AppData% can't be
// resolved for any reason, logging silently falls back to stderr,
// which is fine in development; in a production build there is no
// console attached for stderr to appear on anyway.
func setupLogging() *os.File {
	dir, err := appDataDir()
	if err != nil {
		log.Printf("[app] could not resolve %%AppData%%, logging to stderr only: %v", err)
		return nil
	}

	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		log.Printf("[app] could not create logs directory, logging to stderr only: %v", err)
		return nil
	}

	logPath := filepath.Join(logsDir, "app.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("[app] could not open log file %s, logging to stderr only: %v", logPath, err)
		return nil
	}

	log.SetOutput(file)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.Printf("[app] ==== AlwaysOnTop starting, logging to %s ====", logPath)
	return file
}

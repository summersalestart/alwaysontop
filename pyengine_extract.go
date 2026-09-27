package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// extractedEnginePath ensures the embedded, PyInstaller-packaged Python
// engine (production builds only - see pyengine_embed_prod.go) exists as
// a real file under the current user's %AppData% directory (via the
// shared appDataDir() helper in appdata.go), and returns its path.
//
// %AppData% is used - rather than writing next to the installed .exe -
// because:
//   - the app may be installed to Program Files, which a standard user
//     account cannot write to without elevation;
//   - %AppData% is exactly where per-user application support files
//     conventionally live on Windows, independent of install location.
//
// If this build has nothing embedded (a development build), an error is
// returned so the caller falls back to another way of locating the
// engine (see resolveEngineCommand in pyengine.go).
func extractedEnginePath() (string, error) {
	if len(embeddedEngineExe) == 0 {
		return "", fmt.Errorf("this build has no embedded engine (development build)")
	}

	dir, err := appDataDir()
	if err != nil {
		return "", err
	}

	appDir := filepath.Join(dir, "backend")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("could not create %s: %w", appDir, err)
	}

	target := filepath.Join(appDir, exeSuffix("AlwaysOnTopEngine"))

	if engineUpToDate(appDir, target) {
		return target, nil
	}

	if err := writeEngineFile(appDir, target); err != nil {
		// If a previous copy is already there and usable - e.g. the
		// write failed because a still-running prior instance has it
		// locked - prefer reusing it over failing the whole startup.
		if fileExists(target) {
			log.Printf("[pyengine] could not refresh extracted engine (%v); reusing existing copy at %s", err, target)
			return target, nil
		}
		return "", err
	}

	log.Printf("[pyengine] extracted bundled engine to %s", target)
	return target, nil
}

func engineHash() string {
	sum := sha256.Sum256(embeddedEngineExe)
	return hex.EncodeToString(sum[:])
}

func hashMarkerPath(appDir string) string {
	return filepath.Join(appDir, "engine.sha256")
}

// engineUpToDate reports whether an already-extracted engine at target
// matches the currently embedded one, so a normal launch with an
// unchanged engine version does a cheap hash-file comparison instead of
// rewriting a multi-megabyte file on every single startup.
func engineUpToDate(appDir, target string) bool {
	if !fileExists(target) {
		return false
	}
	stored, err := os.ReadFile(hashMarkerPath(appDir))
	if err != nil {
		return false
	}
	return string(stored) == engineHash()
}

func writeEngineFile(appDir, target string) error {
	tmp := target + ".tmp"

	if err := os.WriteFile(tmp, embeddedEngineExe, 0o755); err != nil {
		return fmt.Errorf("failed to write extracted engine to %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to install extracted engine at %s: %w", target, err)
	}

	if err := os.WriteFile(hashMarkerPath(appDir), []byte(engineHash()), 0o644); err != nil {
		// Non-fatal: worst case, a future launch just re-extracts.
		log.Printf("[pyengine] warning: failed to record engine hash: %v", err)
	}

	return nil
}

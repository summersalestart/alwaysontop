//go:build !production

package main

// In development builds (`wails dev`, or a plain `wails build` without
// -tags production) nothing is embedded, so resolveEngineCommand() falls
// straight through to running backend/engine.py with a local Python
// interpreter - keeping the dev loop fast and Python source edits live
// without needing a PyInstaller rebuild on every change.
var embeddedEngineExe []byte

//go:build production

package main

import _ "embed"

// embeddedEngineExe holds the PyInstaller-packaged Python engine binary,
// baked directly into this Go executable. It is populated only in
// production builds - see build.bat, which runs PyInstaller to produce
// backend/embedded/AlwaysOnTopEngine.exe and then invokes
// `wails build -tags production` so this file compiles instead of
// pyengine_embed_dev.go.
//
// Embedding it (rather than shipping it as a sibling file) means the
// final AlwaysOnTop.exe is a single self-contained artifact, and lets
// extractedEnginePath() (see pyengine_extract.go) place the runtime copy
// under the current user's %AppData% regardless of where the app itself
// got installed - including a Program Files install a normal user
// account cannot write to.
//
//go:embed backend/embedded/AlwaysOnTopEngine.exe
var embeddedEngineExe []byte

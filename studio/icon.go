package main

import _ "embed"

// appIcon is the window and task-switcher icon on Linux (the macOS .icns is built from the same
// file by `wails build`). Regenerate with scripts/gen-icons.sh.
//
//go:embed build/appicon.png
var appIcon []byte

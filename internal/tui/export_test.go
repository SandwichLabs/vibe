package tui

// withEditorArgv replaces $VISUAL/$EDITOR resolution (tests).
func withEditorArgv(argv ...string) Option {
	return func(a *App) {
		a.editorArgv = func() ([]string, error) { return argv, nil }
	}
}

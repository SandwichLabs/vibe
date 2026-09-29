package container

import (
	"log/slog"
	"os"
	"path/filepath"
)

// Options holds configuration for opening or creating a Container.
type Options struct {
	Passphrase  string
	Logger      *slog.Logger
	Title       string
	Description string
	ReadOnly    bool
	Template    string
	LockApp     string
}

// Option is a functional option for configuring a Container.
type Option func(*Options)

// WithPassphrase sets the encryption passphrase for the .vibe file.
func WithPassphrase(passphrase string) Option {
	return func(o *Options) {
		o.Passphrase = passphrase
	}
}

// WithLogger sets the structured logger for the Container.
func WithLogger(logger *slog.Logger) Option {
	return func(o *Options) {
		o.Logger = logger
	}
}

// WithTitle sets the title metadata for a new .vibe file.
func WithTitle(title string) Option {
	return func(o *Options) {
		o.Title = title
	}
}

// WithDescription sets the description metadata for a new .vibe file.
func WithDescription(description string) Option {
	return func(o *Options) {
		o.Description = description
	}
}

// WithTemplate seeds a named Access-style starter template (e.g. "contacts",
// "inventory", "order-entry") into a new container. See ListTemplates for the
// registered names.
func WithTemplate(name string) Option {
	return func(o *Options) {
		o.Template = name
	}
}

// WithReadOnly opens the container in read-only mode.
func WithReadOnly(readOnly bool) Option {
	return func(o *Options) {
		o.ReadOnly = readOnly
	}
}

// WithLockApp names the process holding a writable Container's file lock
// (#80), shown to anyone refused a second writable open — e.g. "Vibe
// Studio" or "vibe mcp serve". Defaults to the executable's base name.
func WithLockApp(name string) Option {
	return func(o *Options) {
		o.LockApp = name
	}
}

func defaultOptions() *Options {
	return &Options{
		Logger:  slog.Default(),
		LockApp: filepath.Base(os.Args[0]),
	}
}

func applyOptions(opts []Option) *Options {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o
}

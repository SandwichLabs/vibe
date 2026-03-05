package container

import "log/slog"

// Options holds configuration for opening or creating a Container.
type Options struct {
	Passphrase  string
	Logger      *slog.Logger
	Title       string
	Description string
	ReadOnly    bool
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

// WithReadOnly opens the container in read-only mode.
func WithReadOnly(readOnly bool) Option {
	return func(o *Options) {
		o.ReadOnly = readOnly
	}
}

func defaultOptions() *Options {
	return &Options{
		Logger: slog.Default(),
	}
}

func applyOptions(opts []Option) *Options {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o
}

package main

import (
	"fmt"

	"github.com/SandwichLabs/slaw/internal/repl"
	"github.com/SandwichLabs/slaw/internal/session"
)

// ReplOpenResult is a new console: its id (the suffix of its "repl:out:<id>"
// event) and the size it starts with.
type ReplOpenResult struct {
	ID   string `json:"id"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	// Connection is the catalog the console starts in.
	Connection string `json:"connection"`
}

// ReplOpen starts a console on a connection ("" = the workspace default). The
// output arrives as "repl:out:<id>" frames; nothing is sent before the first
// ReplAck(id, 0).
func (a *App) ReplOpen(connection string) (*ReplOpenResult, error) {
	if connection != "" {
		if err := session.ValidateName(connection); err != nil {
			return nil, fmt.Errorf("unknown connection %q", connection)
		}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	id, err := a.consoles.open(a, connection)
	if err != nil {
		return nil, err
	}
	if connection == "" {
		connection = a.sess.DefaultCatalog()
	}
	return &ReplOpenResult{ID: id, Cols: 80, Rows: 24, Connection: connection}, nil
}

// ReplInput sends what the person typed (or pasted) to a console.
func (a *App) ReplInput(id string, data string) error {
	if len(data) > repl.MaxInput {
		return repl.ErrInputTooLarge
	}
	return a.consoles.input(id, data)
}

// ReplResize tells a console the terminal's size in cells.
func (a *App) ReplResize(id string, cols, rows int) error {
	return a.consoles.resize(id, cols, rows)
}

// ReplAck says the terminal has rendered output frames up to seq; ReplAck(id,
// 0) attaches the terminal. It takes no workspace lock.
func (a *App) ReplAck(id string, seq int) error {
	return a.consoles.ack(id, seq)
}

// ReplClose ends a console.
func (a *App) ReplClose(id string) error {
	return a.consoles.close(id)
}

// ReplUse switches a console to a connection ("" = the workspace default) and
// returns the connection the console is on. It refuses, with a sentence, while a
// statement runs or text is typed. A "repl:conn:<id>" event reports every change
// of the console's connection, including a typed `.conn`.
func (a *App) ReplUse(id string, name string) (string, error) {
	return a.consoles.use(id, name)
}

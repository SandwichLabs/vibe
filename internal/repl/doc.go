// Package repl is the data console's engine (#258): a Session that takes the
// bytes a terminal widget produces, edits lines, runs the statements on one
// pinned hub connection and writes back what a terminal draws.
//
// The rule: the console is a renderer for Vibe's own Go REPL. It starts no
// shell, opens no PTY, runs no subprocess and never calls a `duckdb` binary.
// There is one run path: a statement takes the editor's gates
// (usersql.CheckInteractive), then engine.ExecuteOn (the same lazy attach,
// binding, wildcard expansion and error shaping as RunQuery), then the
// embedding surface's AfterRun hook. The agent never reads the console: there
// is no MCP method and no ToolDef for it.
//
// Layering: repl is a domain package. It imports engine, session, usersql, lsp
// and queries, and nothing of Wails, studio, agents, tui, hub or cmd. The
// surface that embeds it (Studio's bindings in R2, `vibe repl` in the
// terminal) supplies the byte stream, the AfterRun hook and the completer.
//
// Output and backpressure: the session writes to its io.Writer synchronously
// from one goroutine, through a bufio.Writer flushed at the end of every
// statement, every prompt, and whenever 64 KiB is buffered. The writer may
// block as long as it likes (a frontend that acknowledges what it has drawn),
// but it must return an error once its tab is closed so Close cannot hang.
// Input is bounded per call (MaxInput) and in the queue behind the line
// editor.
//
// Everything printed that Vibe did not write itself (cell values, column and
// type names, error text, the workspace name) passes through
// engine.TerminalText with C1 and bidirectional characters stripped, so no
// value can emit an escape, OSC or CSI sequence. The only escapes the console
// writes are named constants (the SGR colours, clear screen, bracketed paste).
package repl

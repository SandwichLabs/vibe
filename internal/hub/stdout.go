package hub

import "os"

// ProtectStdout makes stdout safe to use as a protocol channel. It returns a
// private handle on the original stdout, to give to the stdio MCP server, and
// then points file descriptor 1 (and os.Stdout) at stderr for the rest of the
// process, so nothing else can write a byte into the protocol stream: not a Go
// fmt.Println, not a C library's printf (DuckDB's MotherDuck SSO prints a
// sign-in link to stdout, spike #107 finding I), not a panic.
//
// Call it before anything else touches the workspace, and only once, in a
// process whose stdout is the protocol channel (`vibe mcp serve` over stdio).
func ProtectStdout() (*os.File, error) {
	proto, err := redirectStdout()
	if err != nil {
		return nil, err
	}
	os.Stdout = os.Stderr
	return proto, nil
}

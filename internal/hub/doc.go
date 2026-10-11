// Package hub decides which process owns a workspace's DuckDB hub, and lets
// the others find and reach it (#112, plan §4.4).
//
// DuckDB allows one read-write process or many read-only processes per
// .duckdb file, never a mix, so exactly one process per workspace owns the hub
// (Studio, or a headless `vibe mcp serve`). Ownership is a non-blocking flock
// on <workspace>/.vibe-local/hub.lock (internal/filelock, the same crash-safe
// lock behind .vibe files, #80): it dies with the process, so it is never
// stale.
//
// # The lock file
//
// hub.lock is mode 0600 JSON written by the holder under the lock: who it is
// (app, pid, host) and, when it serves MCP, the endpoint (loopback URL and
// bearer token) and whether it honours a release request. Another process
// reads it with ReadHolder without holding the lock.
//
// Trust model: the token in hub.lock is a local secret protected only by file
// permissions and the user's directory, so the lock file is trusted for the
// same user only. ReadHolder refuses a file that is not a regular file owned
// by the current uid, or is readable by group or others, refuses an endpoint
// that is not loopback, and ignores the content unless the flock is really
// held right now (a leftover file after a crash, or one committed to a cloned
// repository, can never point a client anywhere). The token never appears on a
// command line, in a config snippet, in a log line or in an error message.
//
// # Release (take over)
//
// A holder that can let go (WithReleaseHandler) advertises control "sigusr1".
// TakeOver (via the internal requestRelease) sends it SIGUSR1 after checking that the lock is really held,
// by a process on this host, with the recorded pid, that advertises it; the
// holder runs its handler (close the session, Release) and TakeOver then
// acquires the lock. SIGUSR1 rather than a control socket because a unix
// socket path under a deep workspace path can exceed the 104/108 byte limit,
// and because a signal needs no extra listener to secure.
//
// # Platforms
//
// On non-unix platforms (Windows is not a release target) locking, probing and
// release requests are no-ops: AcquireHub always succeeds and ReadHolder
// reports ErrNotHeld.
package hub

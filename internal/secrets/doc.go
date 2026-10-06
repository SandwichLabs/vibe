// Package secrets keeps credentials out of files, logs and the agent's reach
// (plan §4.3, #114).
//
// A credential is one JSON blob per connection ({user, password, token,
// key_id, secret, fingerprint, server}); where it lives is the per-person
// sign-in method:
//
//   - keychain: the OS keychain through go-keyring (service "vibe", or
//     $VIBE_KEYRING_SERVICE). Every call has a timeout, because a locked
//     Secret Service collection can block forever.
//   - env:PREFIX: environment variables, read-only. PREFIX_USER,
//     PREFIX_PASSWORD, PREFIX_TOKEN, PREFIX_KEY_ID and PREFIX_SECRET fill the
//     fields of the blob; "env:MOTHERDUCK" therefore reads MOTHERDUCK_TOKEN.
//   - prompt: in memory for the life of the process, optionally promoted to
//     the keychain ("remember").
//
// There is no plaintext-file fallback: with no keychain and no environment
// variables a connection is "needs credentials", and the message names the
// env: alternative.
//
// A keychain or prompt credential is bound to a fingerprint of the server it
// was entered for (type, host, port, database or bucket scope; see
// workspace.Fingerprint). If the connection definition now points somewhere
// else, Resolve returns ErrFingerprintChanged and does not hand out the
// secret.
//
// Secret values never cross the Wails bridge, MCP or the agent: callers can
// set, clear and ask for a CredentialStatus (which carries the readable
// username or key id but never a secret). Redactor and the slog handler
// scrub every resolved secret value, and credential-shaped patterns such as
// password='...', from errors and logs.
package secrets

// Package workspace is the folder-shaped unit of a Vibe project (plan §4.1):
// a directory with a committed vibe.yaml marker, notebooks, queries and skills
// as plain files, and connection definitions merged from three scopes.
//
//	acme-analytics/
//	├── vibe.yaml            committed: id, name, default_connection, agent ceiling,
//	│                        shared connection definitions (no secrets)
//	├── notebooks/*.vnb      one notebook per file
//	├── queries/*.sql        SQL with a `-- vibe:` header
//	├── skills/<name>/SKILL.md
//	├── data/                local files and local .duckdb files
//	├── .gitignore, .gitattributes
//	└── .vibe-local/         never committed: connections.yaml (this machine's
//	                         connections and per-person settings), state.json
//
// # Scopes
//
// Connection definitions come from three files, merged most-specific-wins:
// local (.vibe-local/connections.yaml) beats shared (vibe.yaml) beats user
// (<config dir>/vibe/connections.yaml, honouring VIBE_CONFIG_DIR). The
// winner replaces the whole entry; Merged reports the scope it came from and
// which lower scopes it shadows. A user-scope entry never overrides a
// workspace one.
//
// # Ceiling, not a switch
//
// The committed sandbox and ask_before_writes can only tighten what the agent
// may do. Loosening is stored per person under .vibe-local/, and
// EffectivePolicy clamps it to the committed ceiling, so a pulled commit can
// never disable anyone's safeguards.
//
// # Secrets
//
// Nothing here ever holds a credential. Definitions carry only what a
// connection needs (host, database, path, ...); the sign-in method is a
// per-person choice stored locally; a fingerprint of the server identity
// lets the keychain layer notice when a pull pointed a connection somewhere
// else. A definition that looks like it embeds a password is rejected.
package workspace

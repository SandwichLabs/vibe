// Package connections is the connection type registry (plan §4.2, #115): what
// each kind of connection (postgres, mysql, motherduck, sqlite, ducklake, s3, gcs, r2,
// azure, iceberg and delta tables, and the
// local duckdb file that internal/session knows itself) needs, how its secret
// and attach SQL are emitted, and how it is tested.
//
// The package sits on top of internal/session's ConnectionType seam and
// internal/secrets; it imports neither internal/workspace nor anything above
// it, so workspace can validate and open connections through it.
//
// Rules every type follows:
//
//   - A DSN or credential never appears in an ATTACH string. Postgres is
//     "ATTACH ” AS x (TYPE postgres, SECRET s)"; every identifier and literal
//     goes through session.QuoteIdent / QuoteLiteral.
//   - Secrets are emitted as []session.SecretStep and run by the session on its
//     dedicated, non-logging path. Which values are secret is declared with
//     SecretParam.Secret, not left to key-name guessing.
//   - A type declares its fields (name, kind, required, secret, per-person), so
//     a form can be generated from ListConnectionTypes().
//   - Extensions are installed only on consent (session.InstallExtension, or
//     Test's WithInstall). Connecting never installs.
//
// Test(ctx, def, cred) attaches the definition in a throwaway, hardened,
// in-memory DuckDB with a 10 s timeout and returns a redacted result.
// MotherDuck is tested in a subprocess (one token per process).
package connections

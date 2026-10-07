package connections

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/secrets"
	"github.com/sandwich-labs/vibe-app/internal/session"
	"github.com/sandwich-labs/vibe-app/internal/storageurl"
)

// ducklakeType attaches a DuckLake: a lakehouse whose catalog (the metadata)
// lives in a database and whose rows live in Parquet files under a data path.
//
//	ATTACH 'ducklake:/abs/lake.ducklake' AS "x" (DATA_PATH '/abs/data/' [, READ_ONLY])
//
// v1 scope (#119): the metadata catalog is a local DuckDB file (Path, with the
// same rules as a duckdb connection), and the data path (option data_path) is
// either a local folder or a URL inside the scope of a storage connection of
// the workspace (s3, gcs, r2, azure). A Postgres or SQLite metadata catalog
// would need a credential or a second file in the ATTACH string, so it is not
// offered; the metadata catalog's own internals (__ducklake_metadata_<name>)
// are hidden from the rail and denied to the agent.
//
// The type is remote in the session's sense (lazy attach; every write needs the
// person's approval) because a write creates Parquet files and a snapshot
// that other readers of the lake see.
type ducklakeType struct{}

func (ducklakeType) Name() string                      { return "ducklake" }
func (ducklakeType) Remote() bool                      { return true }
func (ducklakeType) Extensions() []string              { return []string{"ducklake"} }
func (ducklakeType) NeedsCredential(session.Spec) bool { return false }

// metadataSchemeRE catches a metadata "path" that DuckLake would read as
// another kind of catalog (postgres:, sqlite:, duckdb:, ...). A Windows drive
// ("C:\") is one letter and is let through.
var metadataSchemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]+:`)

// IsDataURL reports whether a data path is a URL (a storage location) rather
// than a folder.
func IsDataURL(p string) bool { return strings.Contains(p, "://") }

func (ducklakeType) Validate(s session.Spec) error {
	if err := noStrayFields(s, "path"); err != nil {
		return err
	}
	if err := requireField(s, "a metadata file (path)", s.Path); err != nil {
		return err
	}
	if metadataSchemeRE.MatchString(s.Path) {
		return fmt.Errorf("ducklake connection %q: the metadata catalog must be a DuckDB file on this computer (not %q)", s.Name, s.Path)
	}
	if err := unknownOptions(s, "data_path"); err != nil {
		return err
	}
	dp := opt(s, "data_path")
	if err := requireField(s, "a data path (option data_path)", dp); err != nil {
		return err
	}
	if IsDataURL(dp) {
		if err := storageurl.Validate(dp); err != nil {
			return fmt.Errorf("ducklake connection %q: data path must be a folder or a plain storage URL: %w", s.Name, err)
		}
	} else if metadataSchemeRE.MatchString(dp) {
		return fmt.Errorf("ducklake connection %q: data path %q is not a folder or a storage URL", s.Name, dp)
	}
	return nil
}

func (ducklakeType) SecretSteps(session.Spec, secrets.Credential) ([]session.SecretStep, error) {
	return nil, nil
}

func (t ducklakeType) AttachSQL(s session.Spec) ([]string, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(s.Path) {
		return nil, fmt.Errorf("ducklake connection %q: the metadata path was not resolved (it must be absolute here)", s.Name)
	}
	dp := opt(s, "data_path")
	if !IsDataURL(dp) && !filepath.IsAbs(dp) {
		return nil, fmt.Errorf("ducklake connection %q: the data path was not resolved (it must be absolute here)", s.Name)
	}
	if s.ReadOnly {
		// A read-only attach of a missing catalog is a confusing DuckLake error.
		if _, err := os.Stat(s.Path); err != nil {
			return nil, fmt.Errorf("the DuckLake metadata file for %q is not readable (a read-only connection cannot create it): %w", s.Name, err)
		}
	}
	sep := string(filepath.Separator)
	if IsDataURL(dp) {
		sep = "/"
	}
	if !strings.HasSuffix(dp, sep) {
		dp += sep
	}
	stmt := "ATTACH " + session.QuoteLiteral("ducklake:"+s.Path) + " AS " + session.QuoteIdent(s.Name) +
		" (DATA_PATH " + session.QuoteLiteral(dp)
	if s.ReadOnly {
		stmt += ", READ_ONLY"
	}
	return []string{stmt + ")"}, nil
}

// PostAttach vets what the attached lake's own catalog says before the lake is
// usable. A DuckLake catalog names every data file it reads, and a crafted one
// (a committed metadata file, or one edited after the fact) can name any file on
// this computer. So the lake is refused, and detached, when
//
//   - the data path stored in the catalog is not the configured one, or
//   - any data file, delete file, scheduled-for-deletion file, table or schema
//     path is absolute, has a ".." segment, a scheme or a backslash.
//
// This is why DuckLake connections are only accepted from your own scopes (see
// workspace.ConnectionDef.ValidateIn, which accepts DuckLake only from your own
// scopes): even so, a catalog you were handed is
// checked before it is read.
func (t ducklakeType) PostAttach(ctx context.Context, q session.Querier, s session.Spec) error {
	meta := session.QuoteIdent("__ducklake_metadata_"+s.Name) + ".main."
	var stored sql.NullString
	err := q.QueryRowContext(ctx, `SELECT value FROM `+meta+`ducklake_metadata WHERE key = 'data_path' AND scope IS NULL`).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("could not verify the lake's catalog (data path): %w", err)
	}
	if stored.Valid {
		want := opt(s, "data_path")
		sep := string(filepath.Separator)
		if IsDataURL(want) {
			sep = "/"
		}
		if !strings.HasSuffix(want, sep) {
			want += sep
		}
		got := stored.String
		if !strings.HasSuffix(got, "/") && !strings.HasSuffix(got, sep) {
			got += sep
		}
		if got != want {
			return fmt.Errorf("the lake's catalog says its data lives somewhere other than the data path of this connection; refusing to read it")
		}
	}
	for _, tbl := range []string{"ducklake_data_file", "ducklake_delete_file", "ducklake_files_scheduled_for_deletion", "ducklake_table", "ducklake_schema"} {
		var bad int
		err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+meta+tbl+`
			WHERE NOT coalesce(path_is_relative, false)
			   OR path IS NULL
			   OR regexp_matches(path, '(^|[/\\])\.\.([/\\]|$)')
			   OR path LIKE '%://%' OR path LIKE '/%' OR path LIKE '%\%'`).Scan(&bad)
		if err != nil {
			return fmt.Errorf("could not verify the lake's catalog (%s): %w", tbl, err)
		}
		if bad > 0 {
			return fmt.Errorf("the lake's catalog (%s) names %d file(s) by an absolute path, a URL or a path that leaves the data folder; refusing to read it", tbl, bad)
		}
	}
	if !IsDataURL(opt(s, "data_path")) {
		if err := refuseSymlinksUnder(opt(s, "data_path")); err != nil {
			return err
		}
	}
	return nil
}

// refuseSymlinksUnder fails when anything under dir is a symbolic link. A
// catalog row such as "link/secret.parquet" is a clean relative path, but if
// "link" points out of the data folder the lake would read what is there. The
// folder itself may be a link (that is the person's own choice); what is inside
// it may not.
func refuseSymlinksUnder(dir string) error {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil //nolint:nilerr // no data folder yet: nothing to read
	}
	const maxEntries = 500000
	seen := 0
	var link string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if seen++; seen > maxEntries {
			return fs.SkipAll
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link = p
			return fs.SkipAll
		}
		return nil
	})
	switch {
	case link != "":
		return fmt.Errorf("the lake's data folder contains a symbolic link (%s); refusing to read it, because a link can lead out of the folder", filepath.Base(link))
	case seen > maxEntries:
		return fmt.Errorf("the lake's data folder has too many files to verify; refusing to read it")
	}
	return nil
}

func (ducklakeType) Probe(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	var n int
	err := sess.DB().QueryRowContext(ctx, `SELECT count(*) FROM duckdb_tables() WHERE database_name = $1`, s.Name).Scan(&n)
	if err != nil {
		return 0, "", fmt.Errorf("attached, but listing tables failed: %w", err)
	}
	return n, fmt.Sprintf("%d tables", n), nil
}

func (ducklakeType) Info() TypeInfo {
	return TypeInfo{
		Name: "ducklake", Label: "DuckLake",
		Fields: []Field{
			{Name: "path", Label: "Metadata file", Kind: KindPath, Required: true, Help: "The DuckDB file that holds the lake's catalog (created on first write)."},
			{Name: "data_path", Label: "Data folder", Kind: KindText, Required: true, Option: true, Help: "A folder for the Parquet files, or a URL inside the scope of an S3, GCS, R2 or Azure connection of this workspace."},
		},
		Extensions:        []string{"ducklake"},
		Capabilities:      Capabilities{Catalog: true, Writable: true, ReadOnlyAttach: true, Remote: true, Test: true},
		FingerprintFields: []string{"type", "path", "option:data_path"},
		SignInMethods:     []string{},
	}
}

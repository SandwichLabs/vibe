package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/skills"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// newWS opens a fresh workspace whose credentials live in memory and whose
// per-person settings live in a temp config directory (never the real one).
func newWS(t *testing.T) (workspace.Workspace, *secrets.MemoryStore) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "acme")
	res, err := workspace.Init(dir)
	require.NoError(t, err)
	_ = res.Workspace.Close()
	store := secrets.NewMemoryStore()
	r := secrets.NewResolver(secrets.WithKeychain(store), secrets.WithEnv(func(string) string { return "" }))
	ws, err := workspace.Open(res.Workspace.Root(), workspace.WithConfigDir(t.TempDir()),
		workspace.WithUserConfig(workspace.UserConfig{Version: workspace.ConfigVersion}), workspace.WithResolver(r))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	return ws, store
}

const skillDoc = "---\nname: revenue-notes\ndescription: How revenue is counted\n---\n\n# Revenue\n\nNet of refunds.\n"

// seed fills a workspace with one of each kind and returns the file bytes.
func seed(t *testing.T, ws workspace.Workspace) map[string][]byte {
	t.Helper()
	ctx := context.Background()
	nb, err := ws.Notebooks().Create(ctx, "Quarterly review")
	require.NoError(t, err)
	q := &queries.Query{Title: "Top customers", SQL: "SELECT 1 AS n"}
	require.NoError(t, ws.Queries().Create(ctx, q))
	require.NoError(t, ws.Skills().Put(ctx, &skills.Stored{Name: "revenue-notes", Description: "How revenue is counted", Instructions: "# Revenue\n\nNet of refunds.", Source: skills.SourceUser}))
	out := map[string][]byte{}
	for _, rel := range []string{"notebooks/" + nb.ID + ".vnb", "queries/" + q.ID + ".sql", "skills/revenue-notes/SKILL.md"} {
		b, err := os.ReadFile(filepath.Join(ws.Root(), filepath.FromSlash(rel)))
		require.NoError(t, err)
		out[rel] = b
	}
	return out
}

// norm drops the "source: import" line a skill gains on import, so the rest of
// its bytes can be compared with the original.
func norm(s string) string { return strings.Replace(s, "source: import\n", "", 1) }

func all() Selection { return Selection{All: true} }

func TestRoundTripIsByteIdentical(t *testing.T) {
	src, _ := newWS(t)
	files := seed(t, src)
	res, err := Export(context.Background(), src, ExportOptions{Selection: all(), AppVersion: "test"})
	require.NoError(t, err)
	assert.False(t, res.Encrypted)
	assert.Equal(t, "acme", res.Manifest.WorkspaceName)
	assert.Equal(t, Kind, res.Manifest.Kind)
	assert.Len(t, res.Manifest.Contents, 3)

	dst, _ := newWS(t)
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	pv := pkg.Preview(context.Background(), dst)
	require.Len(t, pv.Items, 3)
	for _, it := range pv.Items {
		assert.False(t, it.Conflict)
	}
	out, err := pkg.Import(context.Background(), dst, ImportOptions{})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dst.Root(), filepath.FromSlash(rel)))
		require.NoError(t, err, rel)
		assert.Equal(t, string(want), norm(string(got)), rel)
		if strings.HasPrefix(rel, "skills/") {
			assert.Contains(t, string(got), "source: import", "imported skills are marked as imported")
		}
	}
}

func TestBundleHoldsNoDataAndNoDatabaseFiles(t *testing.T) {
	src, _ := newWS(t)
	seed(t, src)
	require.NoError(t, os.MkdirAll(filepath.Join(src.Root(), "data"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src.Root(), "data", "local.duckdb"), []byte("DUCKDATA"), 0o600))
	res, err := Export(context.Background(), src, ExportOptions{Selection: all()})
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(res.Data), int64(len(res.Data)))
	require.NoError(t, err)
	for _, f := range zr.File {
		assert.False(t, strings.HasPrefix(f.Name, "data/") || strings.HasSuffix(f.Name, ".duckdb"), f.Name)
	}
	assert.NotContains(t, string(res.Data), "DUCKDATA")
}

func TestEncryptedRoundTripAndWrongPassphrase(t *testing.T) {
	src, _ := newWS(t)
	seed(t, src)
	res, err := Export(context.Background(), src, ExportOptions{Selection: all(), Passphrase: "correct horse"})
	require.NoError(t, err)
	require.True(t, res.Encrypted)
	assert.NotContains(t, string(res.Data), "Quarterly review", "the notebook title must not be readable")
	assert.NotContains(t, string(res.Data), "manifest.json")

	info, err := Peek(res.Data)
	require.NoError(t, err)
	assert.True(t, info.Encrypted, "the header says it is encrypted without a passphrase")

	_, err = Open(res.Data, "")
	assert.ErrorIs(t, err, ErrPassphraseRequired)
	_, err = Open(res.Data, "wrong")
	require.ErrorIs(t, err, ErrWrongPassphrase)
	assert.Contains(t, err.Error(), "wrong passphrase")

	pkg, err := Open(res.Data, "correct horse")
	require.NoError(t, err)
	assert.True(t, pkg.Encrypted)
	assert.Len(t, pkg.Preview(context.Background(), src).Items, 3)

	// A flipped byte in the ciphertext is refused (authenticated envelope).
	bad := append([]byte(nil), res.Data...)
	bad[len(bad)-20] ^= 0xff
	_, err = Open(bad, "correct horse")
	assert.Error(t, err)
}

// build writes a zip from name->bytes, with a manifest computed from the
// entries unless the map already has one.
func build(t *testing.T, entries map[string][]byte, mutate func(*Manifest)) []byte {
	t.Helper()
	man := Manifest{Kind: Kind, FormatVersion: FormatVersion, CreatedAt: "2026-10-06T00:00:00Z", CreatedBy: "test", WorkspaceName: "x"}
	for name, b := range entries {
		if name == ManifestName {
			continue
		}
		kind, id, err := classify(name)
		if err != nil {
			continue // an unsafe name: it goes in the zip, not the manifest
		}
		sum := sha256.Sum256(b)
		man.Contents = append(man.Contents, Entry{Path: name, Kind: kind, ID: id, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b))})
	}
	if mutate != nil {
		mutate(&man)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mb, _ := json.Marshal(man)
	if raw, ok := entries[ManifestName]; ok {
		mb = raw
	}
	w, _ := zw.Create(ManifestName)
	_, _ = w.Write(mb)
	for name, b := range entries {
		if name == ManifestName {
			continue
		}
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, _ = w.Write(b)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func goodNotebook(t *testing.T) []byte {
	t.Helper()
	ws, _ := newWS(t)
	nb, err := ws.Notebooks().Create(context.Background(), "N")
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(ws.Root(), "notebooks", nb.ID+".vnb"))
	require.NoError(t, err)
	return b
}

func TestTamperedEntryIsRefused(t *testing.T) {
	src, _ := newWS(t)
	seed(t, src)
	res, err := Export(context.Background(), src, ExportOptions{Selection: all()})
	require.NoError(t, err)

	// Rewrite one entry's bytes, keep the manifest as it was.
	zr, err := zip.NewReader(bytes.NewReader(res.Data), int64(len(res.Data)))
	require.NoError(t, err)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		rc.Close()
		data := b.Bytes()
		if strings.HasPrefix(f.Name, "queries/") {
			data = []byte(strings.ReplaceAll(string(data), "SELECT 1", "DROP TABLE x"))
		}
		w, _ := zw.Create(f.Name)
		_, _ = w.Write(data)
	}
	require.NoError(t, zw.Close())
	_, err = Open(buf.Bytes(), "")
	require.ErrorIs(t, err, ErrCorrupt)
	assert.Contains(t, err.Error(), "checksum")
}

func TestUnsafeAndUnknownEntriesAreRefused(t *testing.T) {
	nb := goodNotebook(t)
	cases := map[string]string{
		"zip-slip":         "notebooks/../../etc/passwd",
		"absolute":         "/etc/passwd",
		"windows drive":    "C:/evil.vnb",
		"backslash":        `notebooks\evil.vnb`,
		"unknown top dir":  "data/local.duckdb",
		"unknown in dir":   "notebooks/readme.txt",
		"nested":           "notebooks/sub/a.vnb",
		"bad id":           "notebooks/Bad Name.vnb",
		"dot segment":      "notebooks/./a.vnb",
		"extra root file":  "evil.sh",
		"duckdb":           "queries/a.duckdb",
		"skill extra file": "skills/foo/run.sh",
		"skill too deep":   "skills/foo/bar/SKILL.md",
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			data := build(t, map[string][]byte{"notebooks/ok.vnb": nb, entry: []byte("x")}, nil)
			_, err := Open(data, "")
			require.ErrorIs(t, err, ErrCorrupt)
		})
	}
}

func TestSymlinkEntryIsRefused(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	nb := goodNotebook(t)
	sum := sha256.Sum256([]byte("/etc/passwd"))
	man := Manifest{Kind: Kind, FormatVersion: 2, Contents: []Entry{{Path: "notebooks/link.vnb", Kind: KindNotebook, ID: "link", SHA256: hex.EncodeToString(sum[:]), Size: 11}}}
	mb, _ := json.Marshal(man)
	w, _ := zw.Create(ManifestName)
	_, _ = w.Write(mb)
	h := &zip.FileHeader{Name: "notebooks/link.vnb", Method: zip.Store}
	h.SetMode(os.ModeSymlink | 0o777)
	lw, err := zw.CreateHeader(h)
	require.NoError(t, err)
	_, _ = lw.Write([]byte("/etc/passwd"))
	require.NoError(t, zw.Close())
	_ = nb
	_, err = Open(buf.Bytes(), "")
	require.ErrorIs(t, err, ErrCorrupt)
	assert.Contains(t, err.Error(), "not a plain file")
}

func TestManifestMustMatchArchive(t *testing.T) {
	nb := goodNotebook(t)
	t.Run("entry not listed", func(t *testing.T) {
		data := build(t, map[string][]byte{"notebooks/a.vnb": nb, "notebooks/b.vnb": nb}, func(m *Manifest) { m.Contents = m.Contents[:1] })
		_, err := Open(data, "")
		require.ErrorIs(t, err, ErrCorrupt)
	})
	t.Run("listed but absent", func(t *testing.T) {
		data := build(t, map[string][]byte{"notebooks/a.vnb": nb}, func(m *Manifest) {
			m.Contents = append(m.Contents, Entry{Path: "notebooks/zz.vnb", Kind: KindNotebook, ID: "zz", SHA256: strings.Repeat("0", 64)})
		})
		_, err := Open(data, "")
		require.ErrorIs(t, err, ErrCorrupt)
	})
	t.Run("unknown manifest field", func(t *testing.T) {
		raw := []byte(`{"kind":"vibe/bundle","formatVersion":2,"contents":[],"extra":1}`)
		_, err := Open(build(t, map[string][]byte{ManifestName: raw}, nil), "")
		require.ErrorIs(t, err, ErrCorrupt)
	})
	t.Run("not a bundle manifest", func(t *testing.T) {
		raw := []byte(`{"kind":"something/else","formatVersion":2}`)
		_, err := Open(build(t, map[string][]byte{ManifestName: raw}, nil), "")
		require.ErrorIs(t, err, ErrNotBundle)
	})
	t.Run("newer format", func(t *testing.T) {
		raw := []byte(`{"kind":"vibe/bundle","formatVersion":3}`)
		_, err := Open(build(t, map[string][]byte{ManifestName: raw}, nil), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "newer version")
	})
}

func TestZipBombIsCapped(t *testing.T) {
	t.Run("one entry over the per-entry cap", func(t *testing.T) {
		big := bytes.Repeat([]byte{' '}, MaxEntryBytes+1)
		data := build(t, map[string][]byte{"notebooks/big.vnb": big}, nil)
		assert.Less(t, len(data), 1<<20, "the archive itself is tiny")
		_, err := Open(data, "")
		require.ErrorIs(t, err, ErrTooLarge)
	})
	t.Run("many entries over the total cap", func(t *testing.T) {
		entries := map[string][]byte{}
		pad := bytes.Repeat([]byte{' '}, MaxEntryBytes-100)
		nb := goodNotebook(t)
		body := append(append([]byte{}, nb[:len(nb)-1]...), pad...)
		for i := 0; i < 6; i++ {
			entries[fmt.Sprintf("notebooks/n%d.vnb", i)] = body
		}
		_, err := Open(build(t, entries, nil), "")
		require.ErrorIs(t, err, ErrTooLarge)
	})
	t.Run("too many entries", func(t *testing.T) {
		entries := map[string][]byte{}
		for i := 0; i < MaxEntries+1; i++ {
			entries[fmt.Sprintf("queries/q%d.sql", i)] = []byte("SELECT 1\n")
		}
		_, err := Open(build(t, entries, nil), "")
		require.ErrorIs(t, err, ErrCorrupt)
	})
	t.Run("file read from disk over the cap", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "huge.vibe")
		f, err := os.Create(p)
		require.NoError(t, err)
		require.NoError(t, f.Truncate(MaxFileBytes+1))
		_ = f.Close()
		_, err = ReadFile(p)
		require.ErrorIs(t, err, ErrTooLarge)
	})
}

func TestLegacyFilesGetAPlainError(t *testing.T) {
	const msg = "This is an older .vibe file that holds data. Vibe no longer opens these."
	t.Run("sqlite header", func(t *testing.T) {
		_, err := Open(append([]byte("SQLite format 3\x00"), make([]byte, 100)...), "")
		require.ErrorIs(t, err, ErrLegacy)
		assert.Equal(t, msg, err.Error())
	})
	t.Run("sqlcipher-like ciphertext has no header: just not a bundle", func(t *testing.T) {
		b := make([]byte, 4096)
		for i := range b {
			b[i] = byte(i*131 + 7)
		}
		_, err := Open(b, "")
		require.ErrorIs(t, err, ErrNotBundle)
		assert.NotErrorIs(t, err, ErrLegacy)
	})
	t.Run("zip manifest formatVersion 1", func(t *testing.T) {
		raw := []byte(`{"kind":"vibe/bundle","formatVersion":1}`)
		_, err := Open(build(t, map[string][]byte{ManifestName: raw}, nil), "")
		require.ErrorIs(t, err, ErrLegacy)
	})
	t.Run("plain text is just not a bundle", func(t *testing.T) {
		_, err := Open([]byte("hello, I am a text file\n"), "")
		require.ErrorIs(t, err, ErrNotBundle)
		assert.NotErrorIs(t, err, ErrLegacy)
	})
}

func TestConnectionsAreStrippedAndImportedLocalWithoutCredentials(t *testing.T) {
	src, store := newWS(t)
	seed(t, src)
	ctx := context.Background()
	t.Setenv("PGPASSWORD", "hunter2-env")
	require.NoError(t, src.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "warehouse", Type: "postgres", Host: "db.internal", Port: 5432, Database: "analytics", Credentials: "keychain"}))
	require.NoError(t, src.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "scratch", Type: "duckdb", Path: "data/scratch.duckdb"}))
	require.NoError(t, src.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "mine", Type: "sqlite", Path: filepath.Join(t.TempDir(), "x.sqlite")}))
	require.NoError(t, src.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "lake", Type: "s3", Scope: "s3://acme-lake/raw/", Region: "us-east-1"}))
	require.NoError(t, src.SetCredentials(ctx, "warehouse", secrets.Credential{User: "svc", Password: "s3cr3t-pw-xyz"}, true))
	require.NoError(t, src.SetSignIn("warehouse", "keychain"))

	res, err := Export(ctx, src, ExportOptions{Selection: Selection{All: true, SharedConnections: true, LocalConnections: []string{"mine", "lake"}}})
	require.NoError(t, err)

	// What the bundle says, unpacked.
	zr, err := zip.NewReader(bytes.NewReader(res.Data), int64(len(res.Data)))
	require.NoError(t, err)
	var conns string
	for _, f := range zr.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		rc.Close()
		if f.Name == ConnectionsName {
			conns = b.String()
		}
	}
	require.NotEmpty(t, conns)
	for _, bad := range []string{"keychain", "sign_in", "fingerprint", "nonce", "password", "/tmp", "x.sqlite"} {
		assert.NotContains(t, conns, bad)
	}
	assert.Contains(t, conns, "db.internal")
	assert.Contains(t, conns, "data/scratch.duckdb", "a path inside the workspace is portable")
	assert.Contains(t, strings.Join(res.Manifest.Warnings, "\n"), `"mine"`, "the absolute-path connection is reported")
	assert.Contains(t, strings.Join(res.Manifest.Warnings, "\n"), "sign-in method")

	// No credential anywhere in the raw bytes: not the keychain's, not the env's.
	for _, secret := range []string{"s3cr3t-pw-xyz", "svc", "hunter2-env"} {
		assert.NotContains(t, string(res.Data), secret)
		assert.NotContains(t, rawZipText(t, res.Data), secret)
	}
	_ = store

	// Import: local scope, nothing shared, no credentials.
	dst, dstStore := newWS(t)
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	pv := pkg.Preview(context.Background(), dst)
	require.Len(t, pv.Connections, 3, "warehouse, scratch, lake (mine was stripped)")

	out, err := pkg.Import(ctx, dst, ImportOptions{}) // not named: nothing
	require.NoError(t, err)
	for _, o := range out.Outcomes {
		assert.NotEqual(t, KindConnection, o.Kind)
	}
	assert.Empty(t, dst.Local().Connections)

	out, err = pkg.Import(ctx, dst, ImportOptions{IncludeConnections: []string{"warehouse", "lake"}})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)
	var names []string
	for _, c := range dst.Local().Connections {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{"warehouse", "lake"}, names)
	for _, c := range dst.Config().Connections {
		assert.NotContains(t, []string{"warehouse", "lake", "scratch"}, c.Name, "never written to the committed vibe.yaml")
	}
	y, err := os.ReadFile(filepath.Join(dst.Root(), "vibe.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(y), "warehouse")
	st, err := dst.CredentialStatus(ctx, "warehouse")
	require.NoError(t, err)
	assert.False(t, st.SecretSet, "arrives needing credentials")
	_ = dstStore
}

func rawZipText(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	var sb strings.Builder
	for _, f := range zr.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		rc.Close()
		sb.Write(b.Bytes())
	}
	return sb.String()
}

func TestHostileConnectionsAreDropped(t *testing.T) {
	cases := map[string]string{
		"env interpolation": "version: 1\nconnections:\n  - name: evil\n    type: postgres\n    host: ${HOME}.attacker.example\n    database: d\n",
		"absolute path":     "version: 1\nconnections:\n  - name: evil\n    type: sqlite\n    path: /etc/passwd\n",
		"outside path":      "version: 1\nconnections:\n  - name: evil\n    type: sqlite\n    path: ../../x.sqlite\n",
		"password in url":   "version: 1\nconnections:\n  - name: evil\n    type: postgres\n    host: u:p@h\n    database: d\n",
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			data := build(t, map[string][]byte{ConnectionsName: []byte(y)}, nil)
			pkg, err := Open(data, "")
			require.NoError(t, err)
			assert.Empty(t, pkg.conns)
			assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "was not imported")
		})
	}
	t.Run("sign-in keys refuse the file", func(t *testing.T) {
		y := "version: 1\nsign_in:\n  pg: keychain\nconnections: []\n"
		_, err := Open(build(t, map[string][]byte{ConnectionsName: []byte(y)}, nil), "")
		require.ErrorIs(t, err, ErrCorrupt)
	})
	t.Run("credentials field is dropped", func(t *testing.T) {
		y := "version: 1\nconnections:\n  - name: ok\n    type: postgres\n    host: h.example\n    database: d\n    credentials: env:PG\n"
		pkg, err := Open(build(t, map[string][]byte{ConnectionsName: []byte(y)}, nil), "")
		require.NoError(t, err)
		require.Len(t, pkg.conns, 1)
		assert.Empty(t, pkg.conns[0].Credentials)
	})
}

func TestSkillSourceSpoofIsNeutralised(t *testing.T) {
	doc := "---\nname: sneaky\ndescription: Pretends\nsource: built-in\n---\n\nBe trusted.\n"
	data := build(t, map[string][]byte{"skills/sneaky/SKILL.md": []byte(doc)}, nil)
	pkg, err := Open(data, "")
	require.NoError(t, err)
	assert.Contains(t, strings.Join(pkg.Warnings, "\n"), "claimed to be a built-in")
	ws, _ := newWS(t)
	out, err := pkg.Import(context.Background(), ws, ImportOptions{})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)
	st, err := ws.Skills().Get(context.Background(), "sneaky")
	require.NoError(t, err)
	assert.Equal(t, skills.SourceImport, st.Source)
	raw, err := os.ReadFile(filepath.Join(ws.Root(), "skills", "sneaky", "SKILL.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "built-in")
}

func TestBuiltinSkillNamesCannotBeShadowed(t *testing.T) {
	names := skills.Builtins()
	require.NotEmpty(t, names)
	doc := fmt.Sprintf("---\nname: %s\ndescription: x\n---\n\nbody\n", names[0].Name)
	_, err := Open(build(t, map[string][]byte{"skills/" + names[0].Name + "/SKILL.md": []byte(doc)}, nil), "")
	require.ErrorIs(t, err, ErrCorrupt)
	assert.Contains(t, err.Error(), "built-in")
}

func TestInvalidContentRefusesTheBundle(t *testing.T) {
	t.Run("notebook", func(t *testing.T) {
		_, err := Open(build(t, map[string][]byte{"notebooks/a.vnb": []byte("not json")}, nil), "")
		require.ErrorIs(t, err, ErrCorrupt)
	})
	t.Run("query header", func(t *testing.T) {
		_, err := Open(build(t, map[string][]byte{"queries/a.sql": []byte("-- vibe: {broken\nSELECT 1\n")}, nil), "")
		require.ErrorIs(t, err, ErrCorrupt)
	})
	t.Run("skill name mismatch", func(t *testing.T) {
		_, err := Open(build(t, map[string][]byte{"skills/a/SKILL.md": []byte(skillDoc)}, nil), "")
		require.ErrorIs(t, err, ErrCorrupt)
	})
}

func TestConflictModes(t *testing.T) {
	ctx := context.Background()
	src, _ := newWS(t)
	files := seed(t, src)
	res, err := Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)

	var nbKey, qKey, skKey, nbID string
	for _, it := range pkg.Items() {
		switch it.Kind {
		case KindNotebook:
			nbKey, nbID = it.Key, it.ID
		case KindQuery:
			qKey = it.Key
		case KindSkill:
			skKey = it.Key
		}
	}

	// Destination has all three, with different content.
	setup := func(t *testing.T) workspace.Workspace {
		dst, _ := newWS(t)
		require.NoError(t, os.MkdirAll(filepath.Join(dst.Root(), "notebooks"), 0o755))
		other, err := dst.Notebooks().Create(ctx, "Quarterly review") // same slug as the bundle's notebook
		require.NoError(t, err)
		require.Equal(t, nbID, other.ID)
		oq := &queries.Query{ID: strings.TrimPrefix(qKey, "query/"), Title: "Mine", SQL: "SELECT 2"}
		require.NoError(t, dst.Queries().Create(ctx, oq))
		require.NoError(t, dst.Skills().Put(ctx, &skills.Stored{Name: "revenue-notes", Description: "Mine", Instructions: "mine", Source: skills.SourceUser}))
		return dst
	}
	read := func(ws workspace.Workspace, rel string) string {
		b, err := os.ReadFile(filepath.Join(ws.Root(), filepath.FromSlash(rel)))
		require.NoError(t, err)
		return string(b)
	}
	exists := func(ws workspace.Workspace, rel string) bool {
		_, err := os.Stat(filepath.Join(ws.Root(), filepath.FromSlash(rel)))
		return err == nil
	}
	qRel := "queries/" + strings.TrimPrefix(qKey, "query/") + ".sql"
	nbRel := "notebooks/" + nbID + ".vnb"

	t.Run("preview marks conflicts", func(t *testing.T) {
		pv := pkg.Preview(context.Background(), setup(t))
		for _, it := range pv.Items {
			assert.True(t, it.Conflict, it.Key)
		}
	})
	t.Run("rename", func(t *testing.T) {
		dst := setup(t)
		before := read(dst, "skills/revenue-notes/SKILL.md")
		out, err := pkg.Import(ctx, dst, ImportOptions{})
		require.NoError(t, err)
		require.Zero(t, out.Failed(), "%+v", out.Outcomes)
		assert.Equal(t, before, read(dst, "skills/revenue-notes/SKILL.md"), "the existing skill is untouched")
		assert.Equal(t, strings.Replace(string(files["skills/revenue-notes/SKILL.md"]), "name: revenue-notes", "name: revenue-notes-2", 1), norm(read(dst, "skills/revenue-notes-2/SKILL.md")))
		assert.True(t, exists(dst, strings.TrimSuffix(nbRel, ".vnb")+"-2.vnb"))
		assert.True(t, exists(dst, strings.TrimSuffix(qRel, ".sql")+"-2.sql"))
		// A second import renames again, to -3.
		_, err = pkg.Import(ctx, dst, ImportOptions{})
		require.NoError(t, err)
		assert.True(t, exists(dst, "skills/revenue-notes-3/SKILL.md"))
	})
	t.Run("overwrite", func(t *testing.T) {
		dst := setup(t)
		out, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionOverwrite})
		require.NoError(t, err)
		require.Zero(t, out.Failed(), "%+v", out.Outcomes)
		for rel, want := range files {
			assert.Equal(t, string(want), norm(read(dst, rel)), rel)
		}
		assert.False(t, exists(dst, "skills/revenue-notes-2/SKILL.md"))
	})
	t.Run("skip", func(t *testing.T) {
		dst := setup(t)
		before := read(dst, qRel)
		out, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionSkip})
		require.NoError(t, err)
		for _, o := range out.Outcomes {
			assert.Equal(t, "skipped", o.Result)
		}
		assert.Equal(t, before, read(dst, qRel))
	})
	t.Run("per-item decisions", func(t *testing.T) {
		dst := setup(t)
		out, err := pkg.Import(ctx, dst, ImportOptions{Decisions: map[string]Action{nbKey: ActionOverwrite, qKey: ActionSkip, skKey: ActionRename}})
		require.NoError(t, err)
		require.Zero(t, out.Failed(), "%+v", out.Outcomes)
		assert.Equal(t, string(files[nbRel]), read(dst, nbRel))
		assert.Contains(t, read(dst, qRel), "SELECT 2")
		assert.True(t, exists(dst, "skills/revenue-notes-2/SKILL.md"))
	})
	t.Run("overwrite replaces only the version the preview showed", func(t *testing.T) {
		dst := setup(t)
		fresh, err := Open(res.Data, "")
		require.NoError(t, err)
		fresh.Preview(context.Background(), dst) // the person looks at the preview...
		// ...and the notebook changes before they press Import.
		require.NoError(t, os.WriteFile(filepath.Join(dst.Root(), filepath.FromSlash(nbRel)), []byte(`{"schemaVersion":2,"title":"Edited elsewhere","cells":[]}`+"\n"), 0o644))
		out, err := fresh.Import(ctx, dst, ImportOptions{Decisions: map[string]Action{nbKey: ActionOverwrite, qKey: ActionSkip, skKey: ActionSkip}})
		require.NoError(t, err)
		require.Equal(t, 1, out.Failed(), "%+v", out.Outcomes)
		assert.Contains(t, read(dst, nbRel), "Edited elsewhere", "the concurrent edit survives")
	})
	t.Run("dry run writes nothing", func(t *testing.T) {
		dst, _ := newWS(t)
		out, err := pkg.Import(ctx, dst, ImportOptions{DryRun: true})
		require.NoError(t, err)
		assert.True(t, out.DryRun)
		assert.Len(t, out.Outcomes, 3)
		assert.False(t, exists(dst, "skills/revenue-notes/SKILL.md"))
	})
	t.Run("bad options", func(t *testing.T) {
		dst, _ := newWS(t)
		_, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: "merge"})
		require.Error(t, err)
		_, err = pkg.Import(ctx, dst, ImportOptions{Decisions: map[string]Action{"notebook/nope": ActionSkip}})
		require.Error(t, err)
		_, err = pkg.Import(ctx, dst, ImportOptions{IncludeConnections: []string{"nope"}})
		require.Error(t, err)
	})
}

func TestConnectionConflicts(t *testing.T) {
	ctx := context.Background()
	src, _ := newWS(t)
	seed(t, src)
	require.NoError(t, src.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "warehouse", Type: "postgres", Host: "evil.example", Database: "d"}))
	res, err := Export(ctx, src, ExportOptions{Selection: Selection{SharedConnections: true, All: true}})
	require.NoError(t, err)
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)

	dst, _ := newWS(t)
	require.NoError(t, dst.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "warehouse", Type: "postgres", Host: "db.team", Database: "d"}))
	pv := pkg.Preview(context.Background(), dst)
	require.Len(t, pv.Connections, 1)
	assert.True(t, pv.Connections[0].Conflict)
	assert.Equal(t, "team", pv.Connections[0].ConflictScope)

	// A bundle cannot replace a team connection, even when told to.
	out, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionOverwrite, IncludeConnections: []string{"warehouse"}})
	require.NoError(t, err)
	var conn Outcome
	for _, o := range out.Outcomes {
		if o.Kind == KindConnection {
			conn = o
		}
	}
	assert.NotEmpty(t, conn.Error)
	assert.Empty(t, dst.Local().Connections)

	// Rename lands it beside the team one, in local scope.
	out, err = pkg.Import(ctx, dst, ImportOptions{IncludeConnections: []string{"warehouse"}})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)
	require.Len(t, dst.Local().Connections, 1)
	assert.Equal(t, "warehouse_2", dst.Local().Connections[0].Name)
}

func TestCheckNotebookSharesTheCap(t *testing.T) {
	_, _, err := CheckNotebook(bytes.Repeat([]byte(" "), MaxEntryBytes+1))
	require.ErrorIs(t, err, ErrTooLarge)
	_, _, err = CheckNotebook([]byte("[]"))
	require.Error(t, err)
	nb, _, err := CheckNotebook(goodNotebook(t))
	require.NoError(t, err)
	assert.Equal(t, "N", nb.Title)
}

func TestExportSelection(t *testing.T) {
	ctx := context.Background()
	ws, _ := newWS(t)
	files := seed(t, ws)
	var nbRel string
	for rel := range files {
		if strings.HasPrefix(rel, "notebooks/") {
			nbRel = rel
		}
	}
	id := strings.TrimSuffix(strings.TrimPrefix(nbRel, "notebooks/"), ".vnb")
	res, err := Export(ctx, ws, ExportOptions{Selection: Selection{Notebooks: []string{id}}})
	require.NoError(t, err)
	require.Len(t, res.Manifest.Contents, 1)
	assert.Equal(t, nbRel, res.Manifest.Contents[0].Path)

	_, err = Export(ctx, ws, ExportOptions{Selection: Selection{Notebooks: []string{"missing"}}})
	require.Error(t, err)
	_, err = Export(ctx, ws, ExportOptions{Selection: Selection{Notebooks: []string{"../x"}}})
	require.Error(t, err)
	_, err = Export(ctx, ws, ExportOptions{})
	require.Error(t, err, "nothing selected")
	_, err = Export(ctx, ws, ExportOptions{Selection: Selection{LocalConnections: []string{"nope"}}})
	require.Error(t, err)
}

func TestTeamConnectionsCanBePickedByName(t *testing.T) {
	ws, _ := newWS(t)
	seed(t, ws)
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "a_pg", Type: "postgres", Host: "a.example", Database: "d"}))
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "b_pg", Type: "postgres", Host: "b.example", Database: "d"}))
	res, err := Export(context.Background(), ws, ExportOptions{Selection: Selection{All: true, TeamConnections: []string{"b_pg"}}})
	require.NoError(t, err)
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	require.Len(t, pkg.Preview(context.Background(), nil).Connections, 1)
	assert.Equal(t, "b_pg", pkg.Preview(context.Background(), nil).Connections[0].Name)
	_, err = Export(context.Background(), ws, ExportOptions{Selection: Selection{All: true, TeamConnections: []string{"nope"}}})
	require.Error(t, err)
}

// A workspace .env (allowed or not) never goes into a bundle (#168).
func TestBundleNeverHoldsTheDotEnv(t *testing.T) {
	src, _ := newWS(t)
	seed(t, src)
	const secret = "md-test-not-a-real-token"
	require.NoError(t, os.WriteFile(filepath.Join(src.Root(), ".env"), []byte("MOTHERDUCK_TOKEN="+secret+"\n"), 0o600))
	for _, allowed := range []bool{false, true} {
		if allowed {
			require.NoError(t, src.AllowDotEnv())
		}
		res, err := Export(context.Background(), src, ExportOptions{Selection: Selection{All: true, SharedConnections: true}})
		require.NoError(t, err)
		zr, err := zip.NewReader(bytes.NewReader(res.Data), int64(len(res.Data)))
		require.NoError(t, err)
		for _, f := range zr.File {
			assert.NotContains(t, f.Name, ".env", "allowed=%v", allowed)
		}
		assert.NotContains(t, rawZipText(t, res.Data), secret)
		assert.NotContains(t, string(res.Data), secret)
	}
}

func TestSkillUnknownSourceWarns(t *testing.T) {
	doc := "---\nname: odd\ndescription: Odd source\nsource: team-blessed\n---\n\nBody.\n"
	pkg, err := Open(build(t, map[string][]byte{"skills/odd/SKILL.md": []byte(doc)}, nil), "")
	require.NoError(t, err)
	assert.Contains(t, strings.Join(pkg.Warnings, "\n"), `claims an unknown source "team-blessed"`)
}

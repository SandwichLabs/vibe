package skills

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

func goldenSkill() Stored {
	return Stored{
		Name: "revenue-definitions", Description: "How we define revenue, net of refunds",
		Instructions: "# Revenue\n\nRevenue is `amount - refunds` where status <> 'void'.\n\n- Booked vs recognised: see below\n",
		TableName:    "orders", Source: SourceImport,
	}
}

func TestEncode_Golden(t *testing.T) {
	got, err := Encode(goldenSkill())
	require.NoError(t, err)
	path := filepath.Join("testdata", "golden", "revenue-definitions.SKILL.md")
	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))

	// The built-ins' own parser reads it.
	name, desc, body, ok := ParseFrontmatter(string(got))
	require.True(t, ok)
	assert.Equal(t, "revenue-definitions", name)
	assert.Equal(t, "How we define revenue, net of refunds", desc)
	assert.True(t, strings.HasPrefix(body, "# Revenue"))
}

func TestFileStore_SaveLoadSaveIsByteIdentical(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "revenue-definitions.SKILL.md"))
	require.NoError(t, err)
	dir := filepath.Join(root, "skills", "revenue-definitions")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), golden, 0o644))
	for i := 0; i < 3; i++ {
		sk, err := s.Get(ctx, "revenue-definitions")
		require.NoError(t, err)
		assert.Equal(t, "orders", sk.TableName)
		assert.Equal(t, SourceImport, sk.Source)
		require.NoError(t, s.Put(ctx, sk))
		after, _ := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		assert.Equal(t, string(golden), string(after), "cycle %d", i)
	}
}

func TestFileStore_PutGetListDelete(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)

	list, err := s.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list, "a missing skills/ directory is an empty list")

	a := &Stored{Name: "alpha", Description: "first", Instructions: "do a"}
	require.NoError(t, s.Put(ctx, a))
	assert.Equal(t, SourceUser, a.Source)
	require.NoError(t, s.Put(ctx, &Stored{Name: "beta", Description: "second", Instructions: "do b"}))

	list, err = s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "alpha", list[0].Name)

	// A sibling file survives Delete; the directory is removed only when empty.
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "alpha", "notes.txt"), []byte("keep"), 0o644))
	require.NoError(t, s.Delete(ctx, "alpha"))
	assert.FileExists(t, filepath.Join(root, "skills", "alpha", "notes.txt"))
	require.NoError(t, s.Delete(ctx, "beta"))
	assert.NoDirExists(t, filepath.Join(root, "skills", "beta"))
	require.NoError(t, s.Delete(ctx, "beta"), "deleting a missing skill is a no-op")

	_, err = s.Get(ctx, "beta")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFileStore_ConflictRules(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	require.NoError(t, s.Put(ctx, &Stored{Name: "shared", Description: "d", Instructions: "v1"}))
	mine, err := s.Get(ctx, "shared")
	require.NoError(t, err)

	path := filepath.Join(root, "skills", "shared", "SKILL.md")
	disk, _ := os.ReadFile(path)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(disk), "v1", "theirs", 1)), 0o644))

	mine.Instructions = "mine"
	require.ErrorIs(t, s.Put(ctx, mine), ErrConflict)
	after, _ := os.ReadFile(path)
	assert.Contains(t, string(after), "theirs")
	require.NoError(t, s.Put(ctx, mine, Overwrite()))

	// A new process that never read the file cannot write blind over it...
	other := NewFileStore(root)
	assert.ErrorIs(t, other.Put(ctx, &Stored{Name: "shared", Description: "d", Instructions: "blind"}), ErrConflict)
	// ...but Get-then-Put is the normal read-modify-write.
	got, err := other.Get(ctx, "shared")
	require.NoError(t, err)
	got.Instructions = "ok"
	require.NoError(t, other.Put(ctx, got))
}

func TestFileStore_RejectsUnsafeNamesAndMultilineFields(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	for _, name := range []string{"../evil", "a/b", "Upper", "", "-lead"} {
		assert.Error(t, s.Put(ctx, &Stored{Name: name, Description: "d", Instructions: "i"}), name)
	}
	assert.Error(t, s.Put(ctx, &Stored{Name: "ok", Description: "line1\nsource: import", Instructions: "i"}))
	assert.Error(t, s.Put(ctx, &Stored{Name: "ok", Description: "d", TableName: "t\nx: y", Instructions: "i"}))
	assert.Error(t, s.Put(ctx, &Stored{Name: "ok", Description: "", Instructions: "i"}))
	entries, _ := os.ReadDir(root)
	assert.Empty(t, entries)
}

func TestFileStore_MismatchedOrBrokenSkillsAreSkipped(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	mk := func(dir, content string) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "skills", dir, "SKILL.md"), []byte(content), 0o644))
	}
	mk("good", "---\nname: good\ndescription: fine\n---\n\nbody\n")
	mk("wrong", "---\nname: other\ndescription: fine\n---\n\nbody\n")
	mk("nofm", "just text")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "empty-dir"), 0o755))

	list, skipped, err := s.ListWithSkipped(ctx)
	require.NoError(t, err)
	byName := map[string]Stored{}
	for _, sk := range list {
		byName[sk.Name] = sk
	}
	assert.Empty(t, byName["good"].Error)
	assert.NotEmpty(t, byName["wrong"].Error, "a broken skill still lists, with an Error")
	assert.NotEmpty(t, byName["nofm"].Error)
	assert.Contains(t, skipped, "wrong")
	assert.Contains(t, skipped, "nofm")

	// Claude Code style extra frontmatter keys are tolerated.
	mk("extra", "---\nname: extra\ndescription: d\nallowed-tools: Bash\n---\n\nbody\n")
	got, err := s.Get(ctx, "extra")
	require.NoError(t, err)
	assert.Equal(t, "body", got.Instructions)
}

func TestFileStore_SymlinkedSkillDirIsNotFollowedOutside(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("---\nname: leak\ndescription: d\n---\n\nsecret\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "skills", "leak")))
	s := NewFileStore(root)
	list, err := s.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)
	_, err = s.Get(ctx, "leak")
	assert.Error(t, err)
	assert.Error(t, s.Put(ctx, &Stored{Name: "leak", Description: "d", Instructions: "pwn"}))
	data, _ := os.ReadFile(filepath.Join(outside, "SKILL.md"))
	assert.Contains(t, string(data), "secret")
}

// TestStore_Contract is the behaviour every Store shares; it once ran against
// the legacy _vibe_skills store.
func TestStore_Contract(t *testing.T) {
	for name, mk := range map[string]func(t *testing.T) Store{
		"file": func(t *testing.T) Store { return NewFileStore(t.TempDir()) },
		"mock": func(t *testing.T) Store { return &Mock{} },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := mk(t)
			l, err := s.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, l, "an empty store lists empty")

			require.NoError(t, s.Put(ctx, &Stored{Name: "b", Description: "d", Instructions: "i", TableName: "t", Source: SourceImport}))
			require.NoError(t, s.Put(ctx, &Stored{Name: "a", Description: "d", Instructions: "i"}))
			list, err := s.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 2)
			assert.Equal(t, "a", list[0].Name)
			got, err := s.Get(ctx, "b")
			require.NoError(t, err)
			assert.Equal(t, SourceImport, got.Source)
			assert.Equal(t, "t", got.TableName)
			_, err = s.Get(ctx, "zzz")
			assert.ErrorIs(t, err, ErrNotFound)
			require.NoError(t, s.Delete(ctx, "a"))
			require.NoError(t, s.Delete(ctx, "a"))
		})
	}
}

func TestFileStore_ClaudeCodeSkillRoundTripsLosslessly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "claude-code.SKILL.md"))
	require.NoError(t, err)
	dir := filepath.Join(root, "skills", "cc")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, golden, 0o644))

	sk, err := s.Get(ctx, "cc")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(sk.Instructions, "    indented code first"), "indentation survives: %q", sk.Instructions)
	require.NoError(t, s.Put(ctx, sk))
	after, _ := os.ReadFile(path)
	assert.Equal(t, string(golden), string(after), "a no-op save changes nothing: unknown keys, comment, order, indentation")

	// An edit to a known key lands in place; everything else stays put.
	sk, err = s.Get(ctx, "cc")
	require.NoError(t, err)
	sk.Description = "edited"
	sk.TableName = "orders"
	require.NoError(t, s.Put(ctx, sk))
	after, _ = os.ReadFile(path)
	want := strings.Replace(string(golden), "description: from claude code\n", "description: edited\n", 1)
	want = strings.Replace(want, "metadata:\n  author: someone\n", "metadata:\n  author: someone\ntable_name: orders\n", 1)
	assert.Equal(t, want, string(after))
}

func TestFileStore_ListRemembersRevisionAndKeepsBrokenEntries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	require.NoError(t, s.Put(ctx, &Stored{Name: "a", Description: "d", Instructions: "v1"}))
	other := NewFileStore(root) // a second process
	_, err := other.List(ctx)
	require.NoError(t, err)
	got, err := other.Get(ctx, "a")
	require.NoError(t, err)
	got.Instructions = "v2"
	require.NoError(t, other.Put(ctx, got))
	// The first store's last-seen revision is now stale.
	assert.ErrorIs(t, s.Put(ctx, &Stored{Name: "a", Description: "d", Instructions: "v3"}), ErrConflict)
}

func TestFileStore_SourceFromFilesIsUserOrImportOnly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	for name, src := range map[string]string{"claims-builtin": "built-in", "claims-junk": "<b>x</b>", "real-import": "import", "real-user": "user"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "skills", name, "SKILL.md"),
			[]byte("---\nname: "+name+"\ndescription: d\nsource: "+src+"\n---\n\nbody\n"), 0o644))
	}
	want := map[string]string{"claims-builtin": SourceUser, "claims-junk": SourceUser, "real-import": SourceImport, "real-user": SourceUser}
	list, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 4)
	for _, sk := range list {
		assert.Equal(t, want[sk.Name], sk.Source, sk.Name)
	}
	got, err := s.Get(ctx, "claims-builtin")
	require.NoError(t, err)
	assert.Equal(t, SourceUser, got.Source)
	// Saving it back writes no bogus source line.
	require.NoError(t, s.Put(ctx, got))
	data, _ := os.ReadFile(filepath.Join(root, "skills", "claims-builtin", "SKILL.md"))
	assert.NotContains(t, string(data), "built-in")
}

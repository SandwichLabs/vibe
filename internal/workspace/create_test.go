package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/templates"
)

func TestNewDir(t *testing.T) {
	parent := t.TempDir()
	dir, err := NewDir(NewRequest{Name: "  Acme Analytics ", ParentDir: " " + parent + " "})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(parent, "acme-analytics"), dir)
	dir, err = NewDir(NewRequest{Dir: filepath.Join(parent, "here"), ParentDir: "ignored"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(parent, "here"), dir)
	_, err = NewDir(NewRequest{Name: "x"})
	assert.EqualError(t, err, "Choose where to put the new workspace.")
	dir, err = NewDir(NewRequest{Name: "!!!", ParentDir: parent})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(parent, "workspace"), dir, "a name with nothing to slug falls back")
}

func TestCheckNewDir(t *testing.T) {
	parent := t.TempDir()
	chk, err := CheckNewDir(parent, "Fresh")
	require.NoError(t, err)
	assert.Equal(t, DirCheck{Dir: filepath.Join(parent, "fresh")}, chk)
	require.NoError(t, os.Mkdir(filepath.Join(parent, "fresh"), 0o755))
	chk, _ = CheckNewDir(parent, "Fresh")
	assert.True(t, chk.Exists)
	assert.False(t, chk.NotEmpty)
	require.NoError(t, os.WriteFile(filepath.Join(parent, "fresh", "f.txt"), []byte("x"), 0o600))
	chk, _ = CheckNewDir(parent, "Fresh")
	assert.True(t, chk.NotEmpty)
	_, err = CheckNewDir("", "x")
	assert.Error(t, err)
}

func TestCreateNew(t *testing.T) {
	t.Setenv("VIBE_CONFIG_DIR", t.TempDir())
	parent := t.TempDir()

	_, err := CreateNew(NewRequest{ParentDir: parent}, nil)
	assert.EqualError(t, err, "Give the workspace a name.")
	_, err = CreateNew(NewRequest{Name: "x", ParentDir: parent, Template: "nope"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown template "nope"`)
	assert.Contains(t, err.Error(), "contacts")
	assert.NoDirExists(t, filepath.Join(parent, "x"), "a refused request writes nothing")

	got, err := CreateNew(NewRequest{Name: "Acme", ParentDir: parent, Template: "contacts"}, nil)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(parent, "acme"), got.Root)
	assert.Equal(t, "contacts", got.Template.Name)
	assert.FileExists(t, filepath.Join(got.Root, ConfigFile))

	// The same folder again: now it has files.
	_, err = CreateNew(NewRequest{Name: "Acme", ParentDir: parent}, nil)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "There's already a folder called acme"), err.Error())
	// A workspace folder is a workspace already, even in place.
	_, err = CreateNew(NewRequest{Dir: got.Root, InPlace: true}, nil)
	assert.EqualError(t, err, "That folder is already a workspace. Use Open folder.")

	// In place: a folder that has files, no name needed.
	inplace := filepath.Join(t.TempDir(), "mine")
	require.NoError(t, os.Mkdir(inplace, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(inplace, "notes.txt"), []byte("x"), 0o600))
	made, err := CreateNew(NewRequest{Dir: inplace, InPlace: true}, nil)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(made.Root, ConfigFile))
	assert.FileExists(t, filepath.Join(inplace, "notes.txt"))
}

func TestSeedTemplate(t *testing.T) {
	ws, _ := newWorkspace(t)
	sess, _, err := OpenSession(context.Background(), ws, nil, WithSessionOptions(session.WithExtensionDirectory(t.TempDir())))
	require.NoError(t, err)
	defer sess.Close()
	tmpl, ok := templates.Get("contacts")
	require.True(t, ok)

	// A default connection that failed to attach is refused, before any write.
	err = SeedTemplate(context.Background(), ws, sess, []SessionIssue{{Connection: ws.DefaultConnection(), Message: "locked"}}, tmpl)
	assert.EqualError(t, err, "the default connection is not available: locked")
	// An issue on some other connection does not matter.
	require.NoError(t, SeedTemplate(context.Background(), ws, sess, []SessionIssue{{Connection: "other", Message: "x"}}, tmpl))
	tabs, err := sess.ListTables(context.Background(), "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, tabs)
	nbs, err := ws.Notebooks().List(context.Background())
	require.NoError(t, err)
	assert.Len(t, nbs, 1)
}

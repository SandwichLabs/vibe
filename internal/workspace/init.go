package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/SandwichLabs/vibe/internal/filestore"
)

// Lines Init seeds into the ignore files.
var (
	gitignoreLines = []string{LocalDir + "/", "data/*.duckdb", "data/*.duckdb.wal", ".env", ".env.*", "!.env.example"}
	gitattrsLines  = []string{"*.vnb text eol=lf"}
	// workspaceDirs are created by Init.
	workspaceDirs = []string{"notebooks", "queries", "skills", DataDir}
)

// InitOption configures Init.
type InitOption func(*initOpts)

type initOpts struct {
	name   string
	id     string
	git    bool
	logger *slog.Logger
}

// WithName sets the workspace name (default: the folder name).
func WithName(name string) InitOption { return func(o *initOpts) { o.name = name } }

// WithID fixes the workspace id (tests; normally a fresh UUID).
func WithID(id string) InitOption { return func(o *initOpts) { o.id = id } }

// WithGit asks Init to run `git init` when git is on PATH and the folder is
// not already a repository.
func WithGit(on bool) InitOption { return func(o *initOpts) { o.git = on } }

// WithInitLogger sets the logger.
func WithInitLogger(l *slog.Logger) InitOption { return func(o *initOpts) { o.logger = l } }

// InitResult describes what Init did.
type InitResult struct {
	Workspace Workspace
	// GitInitialized is true when Init ran `git init`.
	GitInitialized bool
	// Notes are human-readable remarks (git missing, already a repository).
	Notes []string
}

// Init turns dir into a workspace: a fresh-id vibe.yaml, notebooks/,
// queries/, skills/ and data/, a merged .gitignore and .gitattributes, and the
// default `local` DuckDB connection (D3). Files already in the folder are left
// alone, and existing .gitignore/.gitattributes are extended, not replaced.
// vibe.yaml is written last, so a failure part-way never leaves a folder that
// looks like a workspace but is not.
//
// It refuses a folder that is already a workspace or sits inside one.
func Init(dir string, opts ...InitOption) (*InitResult, error) {
	o := initOpts{logger: slog.Default()}
	for _, f := range opts {
		f(&o)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %q: %w", dir, err)
	}
	if root, err := Discover(abs); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyWorkspace, root)
	} else if !errors.Is(err, ErrNotWorkspace) {
		return nil, err
	}
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", dir, err)
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", dir, err)
	}

	id := o.id
	if id == "" {
		id = uuid.NewString()
	}
	name := strings.TrimSpace(o.name)
	if name == "" {
		name = filepath.Base(root)
	}
	cfg := Config{
		Version:           ConfigVersion,
		ID:                id,
		Name:              name,
		DefaultConnection: DefaultConnectionName,
		Connections: []ConnectionDef{{
			Name: DefaultConnectionName, Type: "duckdb", Path: DefaultConnectionPath, ReadOnly: Bool(false),
		}},
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	yamlBytes, err := MarshalConfig(cfg)
	if err != nil {
		return nil, err
	}

	for _, d := range workspaceDirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create %s/: %w", d, err)
		}
	}
	if err := EnsureGitFiles(root); err != nil {
		return nil, err
	}

	res := &InitResult{}
	if o.git {
		switch ran, note := gitInit(context.Background(), root); {
		case ran:
			res.GitInitialized = true
		case note != "":
			res.Notes = append(res.Notes, note)
		}
	}

	if _, err := filestore.WriteAtomic(root, filestore.Write{Rel: ConfigFile, Data: yamlBytes}, nil); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", ConfigFile, err)
	}
	ws, err := Open(root, WithLogger(o.logger))
	if err != nil {
		return nil, fmt.Errorf("failed to open the new workspace: %w", err)
	}
	res.Workspace = ws
	return res, nil
}

// EnsureGitFiles merges Vibe's lines into root's .gitignore and
// .gitattributes, creating either when absent and never removing or reordering
// anything already there.
func EnsureGitFiles(root string) error {
	if err := ensureLines(root, GitignoreFile, "# Vibe: machine-local state and data files", gitignoreLines); err != nil {
		return err
	}
	return ensureLines(root, GitattrsFile, "# Vibe: notebooks are LF text so diffs stay clean", gitattrsLines)
}

func ensureLines(root, name, comment string, want []string) error {
	abs, err := filestore.Resolve(root, name)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(abs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", name, err)
	}
	have := map[string]bool{}
	for _, l := range strings.Split(strings.ReplaceAll(string(existing), "\r\n", "\n"), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var missing []string
	for _, w := range want {
		if !have[w] {
			missing = append(missing, w)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var buf bytes.Buffer
	buf.Write(existing)
	if len(existing) > 0 {
		if !bytes.HasSuffix(existing, []byte("\n")) {
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}
	buf.WriteString(comment + "\n")
	for _, m := range missing {
		buf.WriteString(m + "\n")
	}
	expect := ""
	if len(existing) > 0 {
		expect = filestore.HashBytes(existing)
	}
	if _, err := filestore.WriteAtomic(root, filestore.Write{Rel: name, Data: buf.Bytes(), Expect: expect}, nil); err != nil {
		return fmt.Errorf("failed to update %s: %w", name, err)
	}
	return nil
}

// gitInit runs `git init` in root. ran is true on success; note explains a
// skip or failure (never an error: git is optional).
func gitInit(ctx context.Context, root string) (ran bool, note string) {
	if _, err := os.Lstat(filepath.Join(root, ".git")); err == nil {
		return false, "already a git repository"
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return false, "git was not found on PATH, so no repository was created"
	}
	cmd := exec.CommandContext(ctx, git, "init", "--quiet") //nolint:gosec // fixed arguments; cwd is the new workspace
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Sprintf("git init failed: %s", strings.TrimSpace(string(out)))
	}
	return true, ""
}

// EnsureID adds a fresh id to a vibe.yaml that has none (a hand-written file,
// or a template copied without one) and returns it. The rest of the file is
// preserved textually by prepending the key.
func EnsureID(root string) (string, error) {
	data, st, err := filestore.Read(root, ConfigFile)
	if err != nil {
		return "", err
	}
	var probe Config
	if err := decodeStrict(data, &probe); err != nil {
		return "", fmt.Errorf("invalid vibe.yaml: %w", err)
	}
	if probe.ID != "" {
		return probe.ID, nil
	}
	id := uuid.NewString()
	out := append([]byte("id: "+id+"\n"), data...)
	if _, err := filestore.WriteAtomic(root, filestore.Write{Rel: ConfigFile, Data: out, Expect: st.Rev()}, nil); err != nil {
		return "", err
	}
	return id, nil
}

func regenerateID(root string) (string, error) {
	data, st, err := filestore.Read(root, ConfigFile)
	if err != nil {
		return "", err
	}
	if !idLineRE.Match(data) {
		return EnsureID(root)
	}
	id := uuid.NewString()
	out := idLineRE.ReplaceAll(data, []byte("id: "+id))
	if _, err := ParseConfig(out); err != nil {
		return "", fmt.Errorf("refusing to rewrite vibe.yaml: %w", err)
	}
	if _, err := filestore.WriteAtomic(root, filestore.Write{Rel: ConfigFile, Data: out, Expect: st.Rev()}, nil); err != nil {
		return "", err
	}
	return id, nil
}

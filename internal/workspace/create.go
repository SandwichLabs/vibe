package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/templates"
)

// NewRequest asks for a new workspace.
type NewRequest struct {
	Name string `json:"name"`
	// Dir is the folder that becomes the workspace. Empty means a new folder
	// named after Name inside ParentDir.
	Dir       string `json:"dir"`
	ParentDir string `json:"parentDir"`
	Git       bool   `json:"git"`
	// Template is a starter template name ("" is blank).
	Template string `json:"template"`
	// InPlace initialises a folder that already has files ("Make this folder a
	// workspace?"); without it a non-empty folder is refused.
	InPlace bool `json:"inPlace"`
}

// DirCheck answers "where will this go, and is it free?".
type DirCheck struct {
	Dir      string `json:"dir"`
	Exists   bool   `json:"exists"`
	NotEmpty bool   `json:"notEmpty"`
}

// Created is a workspace Init just wrote (and closed).
type Created struct {
	Root string
	// Notes are things Init did or skipped that the person should hear about
	// (git missing, a .gitignore left alone).
	Notes []string
	// Template is the starter template to seed once the workspace is open; the
	// zero value when none was asked for.
	Template templates.Template
}

// slug is the folder name for a workspace name.
func slug(name string) string {
	return filestore.SlugOr(strings.TrimSpace(name), "workspace", 0)
}

// NewDir is the absolute folder a new workspace goes in: Dir, or a folder named
// after Name inside ParentDir.
func NewDir(req NewRequest) (string, error) {
	dir := strings.TrimSpace(req.Dir)
	if dir == "" {
		if strings.TrimSpace(req.ParentDir) == "" {
			return "", errors.New("Choose where to put the new workspace.")
		}
		dir = filepath.Join(strings.TrimSpace(req.ParentDir), slug(req.Name))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %q: %w", dir, err)
	}
	return abs, nil
}

// CheckNewDir says where a new workspace called name would go inside parentDir,
// and whether that folder is already taken.
func CheckNewDir(parentDir, name string) (DirCheck, error) {
	dir, err := NewDir(NewRequest{ParentDir: parentDir, Name: name})
	if err != nil {
		return DirCheck{}, err
	}
	chk := DirCheck{Dir: dir}
	if entries, err := os.ReadDir(dir); err == nil {
		chk.Exists = true
		chk.NotEmpty = len(entries) > 0
	} else if !os.IsNotExist(err) {
		chk.Exists = true
	}
	return chk, nil
}

// CreateNew makes a new workspace on disk (vibe.yaml, notebooks/, queries/,
// skills/, data/, optionally a git repository) and closes it again; the caller
// opens it. Without InPlace a folder that already has files is refused.
func CreateNew(req NewRequest, logger *slog.Logger) (*Created, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" && !req.InPlace {
		return nil, errors.New("Give the workspace a name.")
	}
	dir, err := NewDir(req)
	if err != nil {
		return nil, err
	}
	var tmpl templates.Template
	if req.Template != "" {
		t, ok := templates.Get(req.Template)
		if !ok {
			return nil, fmt.Errorf("unknown template %q; available templates: %s", req.Template, strings.Join(templates.List(), ", "))
		}
		tmpl = t
	}
	if !req.InPlace {
		if entries, rerr := os.ReadDir(dir); rerr == nil && len(entries) > 0 {
			return nil, fmt.Errorf("There's already a folder called %s in %s. Pick another name or location. Vibe won't put a workspace inside a folder that already has files in it, unless you use Open folder instead.", filepath.Base(dir), filepath.Dir(dir))
		}
	}
	initOpts := []InitOption{WithGit(req.Git), WithInitLogger(logger)}
	if name != "" {
		initOpts = append(initOpts, WithName(name))
	}
	res, err := Init(dir, initOpts...)
	if err != nil {
		if errors.Is(err, ErrAlreadyWorkspace) {
			return nil, errors.New("That folder is already a workspace. Use Open folder.")
		}
		return nil, fmt.Errorf("Couldn't create the workspace: %w", err)
	}
	root := res.Workspace.Root()
	_ = res.Workspace.Close()
	return &Created{Root: root, Notes: res.Notes, Template: tmpl}, nil
}

// SeedTemplate writes a template's tables into the workspace's default
// connection and its notebook into notebooks/. issues are the session's
// connection problems: a default connection that failed to attach is refused.
func SeedTemplate(ctx context.Context, ws Workspace, sess session.Session, issues []SessionIssue, tmpl templates.Template) error {
	for _, i := range issues {
		if i.Connection == ws.DefaultConnection() {
			return fmt.Errorf("the default connection is not available: %s", i.Message)
		}
	}
	err := templates.SeedTables(ctx, func(ctx context.Context, q string) error {
		_, err := sess.Exec(ctx, q)
		return err
	}, tmpl)
	if err != nil {
		return err
	}
	if _, _, err := ws.Notebooks().Insert(ctx, tmpl.Book); err != nil {
		return fmt.Errorf("failed to write the starter notebook: %w", err)
	}
	return nil
}

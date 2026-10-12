package main

// Creating a workspace, and the per-workspace id and trust bindings.

import (
	"fmt"

	"github.com/SandwichLabs/vibe/internal/templates"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// CreateWorkspaceRequest asks for a new workspace; WorkspaceDirCheck answers
// "where will this go, and is it free?". The rules live in workspace.CreateNew
// and workspace.CheckNewDir.
type (
	CreateWorkspaceRequest = workspace.NewRequest
	WorkspaceDirCheck      = workspace.DirCheck
)

// CheckWorkspaceDir says where a new workspace called name would go inside
// parentDir, and whether that folder is already taken.
func (a *App) CheckWorkspaceDir(parentDir, name string) (*WorkspaceDirCheck, error) {
	chk, err := workspace.CheckNewDir(parentDir, name)
	if err != nil {
		return nil, err
	}
	return &chk, nil
}

// CreateWorkspace makes a new workspace (vibe.yaml, notebooks/, queries/,
// skills/, data/, the `local` database, optionally a git repository and a
// starter template's tables and notebook) and opens it. With InPlace it
// initialises a folder that already has files, which is the "Make this folder
// a workspace?" flow.
func (a *App) CreateWorkspace(req CreateWorkspaceRequest) (*OpenWorkspaceResult, error) {
	created, err := workspace.CreateNew(req, a.logger)
	if err != nil {
		return nil, err
	}
	a.cancelAgentWork()
	a.mu.Lock()
	defer a.mu.Unlock()
	opened, err := a.openLocked(created.Root, openAuto)
	if err != nil {
		return nil, fmt.Errorf("The workspace was created at %s, but could not be opened: %w", created.Root, err)
	}
	opened.Warnings = append(opened.Warnings, created.Notes...)
	if req.Template != "" {
		a.seedTemplateLocked(opened, req.Template, created.Template)
	}
	return opened, nil
}

// seedTemplateLocked adds a starter template to the workspace just opened, or
// says in opened.Warnings why it could not.
func (a *App) seedTemplateLocked(opened *OpenWorkspaceResult, name string, tmpl templates.Template) {
	if a.readOnly {
		opened.Warnings = append(opened.Warnings, "The workspace's database is in use by another program, so the starter template was not added.")
		return
	}
	if err := workspace.SeedTemplate(a.ctxOrBackground(), a.ws, a.sess, a.issues, tmpl); err != nil {
		opened.Warnings = append(opened.Warnings, fmt.Sprintf("The workspace was created, but adding the %q template failed: %s", name, a.sess.Redact(err.Error())))
		return
	}
	opened.Info = a.infoLocked()
	a.emitEvent("data:changed")
}

// RegenerateWorkspaceID gives the open workspace a fresh id (a copied folder
// shares its original's id, and so its keychain entries).
func (a *App) RegenerateWorkspaceID() (*WorkspaceInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if _, err := workspace.RegenerateID(a.ws.Root()); err != nil {
		return nil, fmt.Errorf("failed to give this workspace a new id: %w", err)
	}
	if err := a.ws.Reload(); err != nil {
		return nil, fmt.Errorf("failed to reload the workspace: %w", err)
	}
	a.duplicateOf = nil
	a.recents.Record(a.ws.Root(), a.ws.ID(), a.ws.Name())
	return a.infoLocked(), nil
}

// TrustLocalSettings confirms the saved per-person settings (agent limits you
// loosened, sign-in methods) belong to this folder after it was deleted and
// cloned again.
func (a *App) TrustLocalSettings() (*WorkspaceInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if err := a.ws.TrustLocal(); err != nil {
		return nil, fmt.Errorf("failed to confirm your settings for this workspace: %w", err)
	}
	if err := a.ws.Reload(); err != nil {
		return nil, fmt.Errorf("failed to reload the workspace: %w", err)
	}
	return a.infoLocked(), nil
}

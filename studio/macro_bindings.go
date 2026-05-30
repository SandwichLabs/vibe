package main

// Phase 5 / unit P5.3: "Wails RunMacro / macro CRUD bindings + UI-intent
// contract" (parity plan §5.1/§5.3).
//
// The CRUD bindings (SaveMacro/GetMacro/ListMacro/DeleteMacro) are
// guardStudioOnly: macro definitions are builder-only, so they are
// capability-denied in App Mode. RunMacro is callable in BOTH modes (App-Mode
// users press macro-bound command buttons), but in App Mode it refuses
// privileged server-side actions (RunSQL/RunQuery) consistent with the
// capability surface, while pure-navigation macros still return their UI intents.
//
// RunMacro drives automation.Executor against the live DuckDB engine and returns
// the ordered, structured UI-intent list for the frontend to perform.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/automation"
	"github.com/sandwich-labs/vibe-app/internal/schema"
)

// MacroIntent is the WriteResult-style UI-intent DTO the frontend consumes: a
// single structured navigation/dialog request produced by running a macro
// (open_form, open_report, msg_box, ...). It mirrors automation.UIIntent but is
// an exported, Wails-bound shape.
type MacroIntent struct {
	Kind   string            `json:"kind"`
	Target string            `json:"target"`
	Args   map[string]string `json:"args,omitempty"`
}

// MacroRunResult is the structured result RunMacro returns to the frontend: the
// ordered UI-intent list (the frontend performs the navigation/dialogs) after
// any server-side mutations have already been applied through the live engine.
type MacroRunResult struct {
	Intents []MacroIntent `json:"intents"`
}

// macroQueryAdapter bridges the live engine.Manager to automation.QueryEngine so
// the Executor can run RunSQL/RunQuery actions against real DuckDB.
type macroQueryAdapter struct {
	app *App
}

// Execute runs a query through the live engine and adapts the result set to the
// automation package's engine-decoupled shape.
func (m *macroQueryAdapter) Execute(ctx context.Context, query string, params map[string]any) (*automation.ResultSet, error) {
	rs, err := m.app.engine.Execute(ctx, query, params)
	if err != nil {
		return nil, err
	}
	out := &automation.ResultSet{Rows: rs.Rows}
	for _, col := range rs.Columns {
		out.Columns = append(out.Columns, automation.Column{Name: col.Name, Type: col.Type})
	}
	return out, nil
}

// SaveMacro upserts a macro definition. Builder-only (capability-denied in App
// Mode).
func (a *App) SaveMacro(spec automation.MacroSpec) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.guardStudioOnly("SaveMacro"); err != nil {
		return err
	}
	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("failed to validate macro %q: %w", spec.ID, err)
	}
	store := automation.NewMacroStore(a.container.SQLiteDB())
	if err := store.Save(spec); err != nil {
		return fmt.Errorf("failed to save macro %q: %w", spec.ID, err)
	}
	return nil
}

// GetMacro reads a macro definition by id. Builder-only (capability-denied in
// App Mode).
func (a *App) GetMacro(id string) (*automation.MacroSpec, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.guardStudioOnly("GetMacro"); err != nil {
		return nil, err
	}
	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	store := automation.NewMacroStore(a.container.SQLiteDB())
	spec, err := store.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get macro %q: %w", id, err)
	}
	return spec, nil
}

// ListMacro returns all macro definitions. Builder-only (capability-denied in
// App Mode).
func (a *App) ListMacro() ([]automation.MacroSpec, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.guardStudioOnly("ListMacro"); err != nil {
		return nil, err
	}
	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	store := automation.NewMacroStore(a.container.SQLiteDB())
	specs, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list macros: %w", err)
	}
	return specs, nil
}

// DeleteMacro removes a macro by id. Builder-only (capability-denied in App
// Mode).
func (a *App) DeleteMacro(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.guardStudioOnly("DeleteMacro"); err != nil {
		return err
	}
	if a.container == nil {
		return fmt.Errorf("no file open")
	}
	store := automation.NewMacroStore(a.container.SQLiteDB())
	if err := store.Delete(id); err != nil {
		return fmt.Errorf("failed to delete macro %q: %w", id, err)
	}
	return nil
}

// RunMacro loads a macro spec and runs it against the live engine, returning the
// structured UI-intent list. Callable in both modes; in App Mode it refuses
// macros that contain privileged server-side actions (RunSQL/RunQuery) before
// applying any mutation, consistent with the capability surface.
func (a *App) RunMacro(id string) (*MacroRunResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	store := automation.NewMacroStore(a.container.SQLiteDB())
	spec, err := store.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to load macro %q: %w", id, err)
	}

	resolver := func(subID string) (automation.MacroSpec, error) {
		s, err := store.Get(subID)
		if err != nil {
			return automation.MacroSpec{}, err
		}
		return *s, nil
	}

	// App Mode is locked down: privileged server-side actions (raw SQL / DDL via
	// RunSQL/RunQuery) are refused before any mutation, exactly like the raw-SQL
	// bindings. Pure-navigation macros remain runnable.
	if a.mode == modeApp {
		if err := a.assertMacroAppSafe(*spec, store, nil); err != nil {
			return nil, err
		}
	}

	exec := automation.NewExecutor(
		automation.WithQueryEngine(&macroQueryAdapter{app: a}),
		automation.WithMacroResolver(resolver),
		automation.WithLogger(a.logger),
	)
	res, err := exec.Run(context.Background(), *spec)
	if err != nil {
		return nil, fmt.Errorf("failed to run macro %q: %w", id, err)
	}

	// resultToRunResult forwards server-side SetFilter applications to the frontend
	// as set_filter intents (keyed under the "where" arg the frontend's
	// applyMacroIntents consumes) alongside the ordered navigation/dialog intents.
	return resultToRunResult(res), nil
}

// RunAutoExec runs the configured AutoExec startup macro (plan §5.3): the macro
// id stored in _vibe_meta under "autoexec_macro" is loaded and run on file open,
// returning the startup UI intents (e.g. open_switchboard) for the App Shell to
// apply. When no autoexec_macro is configured it returns an empty result.
//
// This is the exported Wails binding the Vue App Shell invokes on the
// "file:opened" event so the startup macro actually drives the shell. OpenFile /
// CreateFile additionally emit an "app:autoexec" event carrying the same intents
// via runAutoExecLocked, so the feature functions even before the shell mounts.
func (a *App) RunAutoExec() (*MacroRunResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.runAutoExecLocked()
}

// runAutoExecLocked is the lock-free core of RunAutoExec. It reads
// autoexec_macro from _vibe_meta, runs the bound macro against the live engine,
// and returns the startup UI intents. Callers either hold a.mu (RunAutoExec) or
// are inside the OpenFile/CreateFile critical section that already holds the
// write lock (the open lifecycle), so this must NOT take the lock itself.
func (a *App) runAutoExecLocked() (*MacroRunResult, error) {
	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}

	// A missing autoexec_macro key (the common case) means "no startup macro" — a
	// no-op, not an error. We read directly (lock-free) since callers already hold
	// a.mu; an unset key returns ("", nil) rather than a not-found error.
	var macroID string
	err := a.container.SQLiteDB().
		QueryRow(`SELECT value FROM _vibe_meta WHERE key = 'autoexec_macro';`).
		Scan(&macroID)
	if errors.Is(err, sql.ErrNoRows) {
		return &MacroRunResult{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read autoexec_macro: %w", err)
	}
	if macroID == "" {
		return &MacroRunResult{}, nil
	}

	res, err := a.runMacroByID(context.Background(), macroID)
	if err != nil {
		return nil, fmt.Errorf("failed to run autoexec macro %q: %w", macroID, err)
	}
	return resultToRunResult(res), nil
}

// resultToRunResult flattens an automation.Result (ordered intents + recorded
// SetFilter applications) into the exported MacroRunResult the frontend consumes.
func resultToRunResult(res *automation.Result) *MacroRunResult {
	out := &MacroRunResult{}
	for _, in := range res.Intents {
		out.Intents = append(out.Intents, MacroIntent{Kind: in.Kind, Target: in.Target, Args: in.Args})
	}
	for _, f := range res.Filters {
		out.Intents = append(out.Intents, MacroIntent{
			Kind:   automation.IntentSetFilter,
			Target: f.Target,
			Args:   map[string]string{"where": f.Filter},
		})
	}
	return out
}

// runMacroByID loads a macro spec and runs it against the live DuckDB engine,
// returning the executor result. It does NOT take the App lock — callers must
// hold (or not require) it — so it can be reused from both the startup binding
// and the form event-hook path. Used by RunAutoExec and the form write hooks.
func (a *App) runMacroByID(ctx context.Context, id string) (*automation.Result, error) {
	store := automation.NewMacroStore(a.container.SQLiteDB())
	spec, err := store.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to load macro %q: %w", id, err)
	}
	resolver := func(subID string) (automation.MacroSpec, error) {
		s, err := store.Get(subID)
		if err != nil {
			return automation.MacroSpec{}, err
		}
		return *s, nil
	}
	exec := automation.NewExecutor(
		automation.WithQueryEngine(&macroQueryAdapter{app: a}),
		automation.WithMacroResolver(resolver),
		automation.WithLogger(a.logger),
	)
	return exec.Run(ctx, *spec)
}

// fireHook runs a form event hook macro (by id) and returns any error. An empty
// macro id is a no-op (no hook bound). A before_* hook surfaces its error to the
// caller to veto the write; an after_* hook's error is propagated too so a failed
// side effect is not silently swallowed.
func (a *App) fireHook(ctx context.Context, macroID string) error {
	if macroID == "" {
		return nil
	}
	if _, err := a.runMacroByID(ctx, macroID); err != nil {
		return fmt.Errorf("form event hook %q failed: %w", macroID, err)
	}
	return nil
}

// formEventsFor resolves the bound form's event hooks. A missing form (or one
// without events) yields a zero-value FormEvents (all hooks empty / no-ops).
func (a *App) formEventsFor(formID string) schema.FormEvents {
	store := schema.NewFormStore(a.container.SQLiteDB())
	spec, err := store.GetForm(formID)
	if err != nil || spec == nil || spec.Events == nil {
		return schema.FormEvents{}
	}
	return *spec.Events
}

// FireFormOpen runs a bound form's on_open then on_load event hooks (plan §5.3)
// and returns any UI intents they produce for the shell to apply. It is the
// real binding the Vue FormView calls when a form opens, so on_open/on_load are
// wired end-to-end (an on_open macro that opens a filtered subform, msg_box,
// etc. surfaces here). A form without those hooks yields an empty result.
func (a *App) FireFormOpen(formID string) (*MacroRunResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.container == nil {
		return nil, fmt.Errorf("no file open")
	}
	events := a.formEventsFor(formID)
	out := &MacroRunResult{}
	for _, hookID := range []string{events.OnOpen, events.OnLoad} {
		if hookID == "" {
			continue
		}
		res, err := a.runMacroByID(context.Background(), hookID)
		if err != nil {
			return nil, fmt.Errorf("form open hook %q failed: %w", hookID, err)
		}
		merged := resultToRunResult(res)
		out.Intents = append(out.Intents, merged.Intents...)
	}
	return out, nil
}

// SubmitFormFor is the form-aware submit the Vue FormView calls (the real
// write path for bound forms): it performs the same atomic parent+children
// SubmitForm write, firing the form's before_insert hook BEFORE the write and
// its after_insert hook only on success (plan §5.3). A before_insert hook that
// returns an error vetoes the submit — the write is never attempted, so no row
// is left behind (skip semantics: the hook executor runs on the engine
// connection, not the write transaction, so the guarantee is "no orphan row",
// equivalent to a rollback of a write that never started).
func (a *App) SubmitFormFor(formID string, parent FormSubmitRow, children []FormSubmitRow) error {
	a.mu.RLock()
	if a.container == nil {
		a.mu.RUnlock()
		return fmt.Errorf("no file open")
	}
	events := a.formEventsFor(formID)
	a.mu.RUnlock()

	ctx := context.Background()
	if err := a.fireHook(ctx, events.BeforeInsert); err != nil {
		return fmt.Errorf("before_insert hook vetoed submit: %w", err)
	}
	if err := a.SubmitForm(parent, children); err != nil {
		return err
	}
	if err := a.fireHook(ctx, events.AfterInsert); err != nil {
		return fmt.Errorf("after_insert hook failed: %w", err)
	}
	return nil
}

// UpdateRowFor is the form-aware update the Vue FormView calls (the real write
// path for bound forms): it resolves the form's bound table, fires the form's
// before_update hook BEFORE the write, performs the same UpdateRow write, and
// fires after_update only on success (plan §5.3). A before_update hook that
// returns an error vetoes the write (UpdateRow is never called).
func (a *App) UpdateRowFor(formID, pkCol string, pkVal any, values map[string]any) (*WriteResult, error) {
	a.mu.RLock()
	if a.container == nil {
		a.mu.RUnlock()
		return nil, fmt.Errorf("no file open")
	}
	events := a.formEventsFor(formID)
	table, err := a.tableForForm(formID)
	a.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if err := a.fireHook(ctx, events.BeforeUpdate); err != nil {
		return nil, fmt.Errorf("before_update hook vetoed update: %w", err)
	}
	res, err := a.UpdateRow(table, pkCol, pkVal, values)
	if err != nil {
		return nil, err
	}
	if err := a.fireHook(ctx, events.AfterUpdate); err != nil {
		return nil, fmt.Errorf("after_update hook failed: %w", err)
	}
	return res, nil
}

// tableForForm resolves and validates a form's bound table for the form-aware
// write path. It loads the form spec, requires a non-empty bound table, and
// validates the table is a real identifier — so a malformed/missing form can
// never be used to drive a write against an arbitrary string. Callers hold the
// App lock (the form store reads SQLCipher metadata).
func (a *App) tableForForm(formID string) (string, error) {
	store := schema.NewFormStore(a.container.SQLiteDB())
	spec, err := store.GetForm(formID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve form %q: %w", formID, err)
	}
	if spec == nil {
		return "", fmt.Errorf("form %q not found", formID)
	}
	if spec.Table == "" {
		return "", fmt.Errorf("form %q has no bound table", formID)
	}
	if !isValidIdentifier(spec.Table) {
		return "", fmt.Errorf("form %q bound to invalid table %q", formID, spec.Table)
	}
	return spec.Table, nil
}

// assertMacroAppSafe walks a macro (and its sub-macros) and returns
// errCapabilityDenied if it contains a privileged server-side action that App
// Mode forbids (RunSQL/RunQuery). visited bounds recursion / cycle traversal.
func (a *App) assertMacroAppSafe(spec automation.MacroSpec, store *automation.MacroStore, visited []string) error {
	for _, id := range visited {
		if id == spec.ID {
			return nil // already inspected on this chain; cycle guard
		}
	}
	chain := append(append([]string{}, visited...), spec.ID)
	for _, action := range spec.Actions {
		switch action.Kind {
		case automation.ActionRunSQL, automation.ActionRunQuery:
			a.logger.Warn("capability denied in App Mode", "method", "RunMacro", "action", action.Kind, "macro", spec.ID)
			return fmt.Errorf("%w: RunMacro action %s", errCapabilityDenied, action.Kind)
		case automation.ActionRunMacro:
			sub, err := store.Get(action.Args["target"])
			if err != nil {
				return fmt.Errorf("failed to resolve sub-macro %q: %w", action.Args["target"], err)
			}
			if err := a.assertMacroAppSafe(*sub, store, chain); err != nil {
				return err
			}
		}
	}
	return nil
}

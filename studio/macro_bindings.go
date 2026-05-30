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
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/automation"
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

	out := &MacroRunResult{}
	for _, in := range res.Intents {
		out.Intents = append(out.Intents, MacroIntent{Kind: in.Kind, Target: in.Target, Args: in.Args})
	}
	// Forward server-side SetFilter applications to the frontend as set_filter
	// intents. The Executor records SetFilter actions in res.Filters (keyed by
	// the action's "filter" arg); the frontend's applyMacroIntents consumes them
	// under the "where" arg key, merging the predicate onto the matching form/
	// report/page navigation. Translating here is what wires the SetFilter path
	// end-to-end (P5.4): without it, App.RunMacro could never emit a set_filter
	// intent in production.
	for _, f := range res.Filters {
		out.Intents = append(out.Intents, MacroIntent{
			Kind:   automation.IntentSetFilter,
			Target: f.Target,
			Args:   map[string]string{"where": f.Filter},
		})
	}
	return out, nil
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

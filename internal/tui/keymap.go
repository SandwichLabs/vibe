package tui

import (
	"github.com/charmbracelet/bubbles/key"
)

// KeyMap defines all key bindings for Vibe Studio.
type KeyMap struct {
	CycleFocus  key.Binding
	ToggleREPL  key.Binding
	Select      key.Binding
	Quit        key.Binding
	ForceQuit   key.Binding
	Save        key.Binding
	Help        key.Binding
	New         key.Binding
	Delete      key.Binding
	NavUp       key.Binding
	NavDown     key.Binding
	NextSection key.Binding
	PrevSection key.Binding
	ScrollUp    key.Binding
	ScrollDown  key.Binding
	HistoryUp   key.Binding
	HistoryDown key.Binding
	Filter      key.Binding
}

// ShortHelp returns key bindings for the short help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.CycleFocus, k.New, k.Select, k.Help, k.Quit}
}

// FullHelp returns grouped key bindings for the full help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.NavUp, k.NavDown, k.NextSection, k.PrevSection},
		{k.CycleFocus, k.ToggleREPL, k.Select, k.New},
		{k.ScrollUp, k.ScrollDown, k.HistoryUp, k.HistoryDown},
		{k.Save, k.Delete, k.Help, k.Quit},
		{k.Filter, k.ForceQuit},
	}
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		CycleFocus: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "cycle panel"),
		),
		ToggleREPL: key.NewBinding(
			key.WithKeys("`"),
			key.WithHelp("`", "toggle REPL"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		ForceQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "force quit"),
		),
		Save: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "save"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		New: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new page/query"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		NavUp: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "up"),
		),
		NavDown: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "down"),
		),
		NextSection: key.NewBinding(
			key.WithKeys("J"),
			key.WithHelp("J", "next section"),
		),
		PrevSection: key.NewBinding(
			key.WithKeys("K"),
			key.WithHelp("K", "prev section"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "scroll down"),
		),
		HistoryUp: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "history up"),
		),
		HistoryDown: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "history down"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
	}
}

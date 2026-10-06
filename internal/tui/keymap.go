package tui

import (
	"github.com/charmbracelet/bubbles/key"
)

// KeyMap defines all key bindings for Vibe Studio's TUI.
type KeyMap struct {
	SwitchView  key.Binding
	Select      key.Binding
	Back        key.Binding
	Quit        key.Binding
	ForceQuit   key.Binding
	Help        key.Binding
	NavUp       key.Binding
	NavDown     key.Binding
	HistoryUp   key.Binding
	HistoryDown key.Binding
}

// ShortHelp returns key bindings for the short help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.SwitchView, k.Select, k.Help, k.Quit}
}

// FullHelp returns grouped key bindings for the full help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.SwitchView, k.NavUp, k.NavDown, k.Select, k.Back},
		{k.HistoryUp, k.HistoryDown},
		{k.Help, k.Quit, k.ForceQuit},
	}
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		SwitchView: key.NewBinding(
			key.WithKeys("tab", "1", "2", "3"),
			key.WithHelp("tab/1/2/3", "switch view"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		ForceQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "force quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		NavUp: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "up"),
		),
		NavDown: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "down"),
		),
		HistoryUp: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "REPL history up"),
		),
		HistoryDown: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "REPL history down"),
		),
	}
}

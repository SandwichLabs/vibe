package workspace

import (
	"sync"

	"github.com/sandwich-labs/vibe-app/internal/llm/skills"
	"github.com/sandwich-labs/vibe-app/internal/notebooks"
	"github.com/sandwich-labs/vibe-app/internal/queries"
)

// Mock is a Workspace for tests of packages that depend on one. Set the
// exported fields you need; the stores default to in-memory mocks.
type Mock struct {
	RootDir string
	WSID    string
	WSName  string
	Cfg     Config
	Loc     LocalConfig
	Conns   []Connection
	Warn    []string

	NotebookStore notebooks.Store
	QueryStore    queries.Store
	SkillStore    skills.Store

	// ReloadErr / SaveErr make Reload and the Save methods fail.
	ReloadErr error
	SaveErr   error
	Saved     []Config

	mu   sync.Mutex
	st   State
	init sync.Once
}

var _ Workspace = (*Mock)(nil)

func (m *Mock) setup() {
	m.init.Do(func() {
		if m.NotebookStore == nil {
			m.NotebookStore = &notebooks.Mock{}
		}
		if m.QueryStore == nil {
			m.QueryStore = &queries.Mock{}
		}
		if m.SkillStore == nil {
			m.SkillStore = &skills.Mock{}
		}
	})
}

func (m *Mock) Root() string               { return m.RootDir }
func (m *Mock) ID() string                 { return m.WSID }
func (m *Mock) Name() string               { return m.WSName }
func (m *Mock) Config() Config             { return m.Cfg }
func (m *Mock) Local() LocalConfig         { return m.Loc }
func (m *Mock) Connections() []Connection  { return m.Conns }
func (m *Mock) DefaultConnection() string  { return m.Cfg.DefaultConnection }
func (m *Mock) Warnings() []string         { return m.Warn }
func (m *Mock) Policy() Policy             { return EffectivePolicy(CeilingOf(m.Cfg), m.Loc.Agent) }
func (m *Mock) Reload() error              { return m.ReloadErr }
func (m *Mock) Close() error               { return nil }
func (m *Mock) Notebooks() notebooks.Store { m.setup(); return m.NotebookStore }
func (m *Mock) Queries() queries.Store     { m.setup(); return m.QueryStore }
func (m *Mock) Skills() skills.Store       { m.setup(); return m.SkillStore }
func (m *Mock) Watch(...WatchOption) (*Watcher, error) {
	return nil, errMockNoWatch
}

func (m *Mock) AgentPolicy() (string, bool) {
	p := m.Policy()
	return p.Sandbox, p.AskBeforeWrites
}

func (m *Mock) SaveConfig(c Config) error {
	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.Cfg = c
	m.Saved = append(m.Saved, c)
	return nil
}

func (m *Mock) SaveLocal(l LocalConfig) error {
	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.Loc = l
	return nil
}

func (m *Mock) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st
}

func (m *Mock) SaveState(s State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.st = s
	return nil
}

// LocalConfigPath is empty: the mock keeps Loc in memory.
func (m *Mock) LocalConfigPath() string { return "" }

// LocalTrusted is always true for the mock.
func (m *Mock) LocalTrusted() bool { return true }

// TrustLocal does nothing in the mock.
func (m *Mock) TrustLocal() error { return nil }

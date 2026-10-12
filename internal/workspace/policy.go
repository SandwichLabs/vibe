package workspace

import "github.com/SandwichLabs/vibe/internal/sandbox"

// tierRank orders sandbox tiers from most to least restrictive (internal/sandbox
// owns the order); ok is false for an unknown tier.
func tierRank(tier string) (int, bool) { return sandbox.Rank(sandbox.Mode(tier)) }

// DefaultSandbox is what the agent runs at when nothing says otherwise; it
// matches the legacy container's default (#79).
const DefaultSandbox = SandboxReadWrite

// Policy is the agent policy that applies to one person in one workspace.
type Policy struct {
	// Sandbox is read-only, read-write or full.
	Sandbox string `json:"sandbox"`
	// AskBeforeWrites is true when every agent write needs the user's OK.
	AskBeforeWrites bool `json:"ask_before_writes"`
}

// Ceiling is the committed (vibe.yaml) agent policy: a limit shared by the
// whole team, never a grant.
type Ceiling struct {
	// Sandbox is the highest tier anyone may use; "" means no limit.
	Sandbox string
	// AskBeforeWrites true means the gate must stay on for everyone;
	// false or nil adds no requirement.
	AskBeforeWrites *bool
}

// LocalAgent is one person's own agent settings (.vibe-local/).
type LocalAgent struct {
	// Sandbox is the tier this person chose; "" means unset (the default).
	Sandbox string `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`
	// AskBeforeWrites false turns the gate off for this person, where the
	// ceiling allows; nil means unset (on).
	AskBeforeWrites *bool `yaml:"ask_before_writes,omitempty" json:"ask_before_writes,omitempty"`
}

// CeilingOf extracts the committed ceiling from a Config.
func CeilingOf(c Config) Ceiling {
	return Ceiling{Sandbox: c.Sandbox, AskBeforeWrites: c.AskBeforeWrites}
}

// rank returns the tier's rank, failing closed: an unknown non-empty value
// ranks as read-only.
func rank(tier string) int {
	if r, ok := tierRank(tier); ok {
		return r
	}
	return 0
}

// EffectivePolicy computes what the agent may do:
//
//   - The sandbox is the person's choice (default read-write), clamped down to
//     the committed ceiling. Local can lower it freely and raise it only up
//     to the ceiling; a committed value can never raise a local one.
//   - Ask-before-writes is on unless this person turned it off locally AND the
//     committed config does not require it. A committed "true" cannot be
//     loosened by anyone; a committed "false" or nothing loosens nobody.
//   - An unknown tier string, committed or local, fails closed to read-only.
func EffectivePolicy(c Ceiling, l LocalAgent) Policy {
	want := l.Sandbox
	if want == "" {
		want = DefaultSandbox
	}
	sandbox := minTier(want, c.Sandbox)

	ask := true
	if l.AskBeforeWrites != nil && !*l.AskBeforeWrites {
		ask = false
	}
	if c.AskBeforeWrites != nil && *c.AskBeforeWrites {
		ask = true
	}
	return Policy{Sandbox: sandbox, AskBeforeWrites: ask}
}

// minTier returns the more restrictive of want and ceiling; an empty ceiling
// is no limit and an unknown value on either side is read-only.
func minTier(want, ceiling string) string {
	if _, ok := tierRank(want); !ok {
		return SandboxReadOnly
	}
	if ceiling == "" {
		return want
	}
	if _, ok := tierRank(ceiling); !ok {
		return SandboxReadOnly
	}
	if rank(ceiling) < rank(want) {
		return ceiling
	}
	return want
}

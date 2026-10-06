package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var tiers = []string{SandboxReadOnly, SandboxReadWrite, SandboxFull}

func TestEffectivePolicy_Table(t *testing.T) {
	yes, no := Bool(true), Bool(false)
	cases := []struct {
		name  string
		c     Ceiling
		l     LocalAgent
		want  Policy
		notes string
	}{
		{"nothing set", Ceiling{}, LocalAgent{}, Policy{SandboxReadWrite, true}, "default read-write, gate on"},
		{"committed read-only, local unset", Ceiling{Sandbox: SandboxReadOnly}, LocalAgent{}, Policy{SandboxReadOnly, true}, ""},
		{"committed read-only, local tries full", Ceiling{Sandbox: SandboxReadOnly}, LocalAgent{Sandbox: SandboxFull}, Policy{SandboxReadOnly, true}, "cannot loosen past the ceiling"},
		{"committed full, local read-only", Ceiling{Sandbox: SandboxFull}, LocalAgent{Sandbox: SandboxReadOnly}, Policy{SandboxReadOnly, true}, "a committed value never loosens a local one"},
		{"committed full, local unset", Ceiling{Sandbox: SandboxFull}, LocalAgent{}, Policy{SandboxReadWrite, true}, "a ceiling is not a grant"},
		{"committed read-write, local full", Ceiling{Sandbox: SandboxReadWrite}, LocalAgent{Sandbox: SandboxFull}, Policy{SandboxReadWrite, true}, ""},
		{"no ceiling, local full", Ceiling{}, LocalAgent{Sandbox: SandboxFull}, Policy{SandboxFull, true}, "local loosens up to no limit"},
		{"gate off locally", Ceiling{}, LocalAgent{AskBeforeWrites: no}, Policy{SandboxReadWrite, false}, ""},
		{"committed gate required beats local off", Ceiling{AskBeforeWrites: yes}, LocalAgent{AskBeforeWrites: no}, Policy{SandboxReadWrite, true}, ""},
		{"committed gate false adds no requirement", Ceiling{AskBeforeWrites: no}, LocalAgent{AskBeforeWrites: no}, Policy{SandboxReadWrite, false}, ""},
		{"committed gate false never turns anyone's off", Ceiling{AskBeforeWrites: no}, LocalAgent{}, Policy{SandboxReadWrite, true}, ""},
		{"unknown committed tier fails closed", Ceiling{Sandbox: "root"}, LocalAgent{Sandbox: SandboxFull}, Policy{SandboxReadOnly, true}, ""},
		{"unknown local tier fails closed", Ceiling{}, LocalAgent{Sandbox: "root"}, Policy{SandboxReadOnly, true}, ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, EffectivePolicy(tc.c, tc.l), tc.name)
	}
}

// The properties that make the ceiling a ceiling, over every combination.
func TestEffectivePolicy_Properties(t *testing.T) {
	yes, no := Bool(true), Bool(false)
	ceilTiers := append([]string{""}, tiers...)
	localTiers := append([]string{""}, tiers...)
	bools := []*bool{nil, yes, no}
	for _, ct := range ceilTiers {
		for _, lt := range localTiers {
			for _, cb := range bools {
				for _, lb := range bools {
					c := Ceiling{Sandbox: ct, AskBeforeWrites: cb}
					l := LocalAgent{Sandbox: lt, AskBeforeWrites: lb}
					p := EffectivePolicy(c, l)

					// Never above the committed ceiling.
					if ct != "" {
						assert.LessOrEqual(t, rank(p.Sandbox), rank(ct), "ceiling %q local %q", ct, lt)
					}
					// Never above what the person asked for (default read-write).
					want := lt
					if want == "" {
						want = DefaultSandbox
					}
					assert.LessOrEqual(t, rank(p.Sandbox), rank(want), "ceiling %q local %q", ct, lt)
					// Exactly the minimum of the two.
					exp := want
					if ct != "" && rank(ct) < rank(want) {
						exp = ct
					}
					assert.Equal(t, exp, p.Sandbox)

					// A committed requirement can never be switched off.
					if cb != nil && *cb {
						assert.True(t, p.AskBeforeWrites, "committed gate must hold")
					}
					// The gate is off only when this person turned it off.
					if !p.AskBeforeWrites {
						assert.True(t, lb != nil && !*lb, "gate off only by explicit local choice")
					}
					// Changing only the committed side can never make the agent
					// more permissive than the same local settings under the
					// strictest committed side would... and tightening the
					// ceiling never loosens the result.
					tighter := EffectivePolicy(Ceiling{Sandbox: SandboxReadOnly, AskBeforeWrites: yes}, l)
					assert.LessOrEqual(t, rank(tighter.Sandbox), rank(p.Sandbox))
					assert.True(t, tighter.AskBeforeWrites)
				}
			}
		}
	}
}

func TestCeilingOf(t *testing.T) {
	c := Config{Sandbox: SandboxReadOnly, AskBeforeWrites: Bool(true)}
	got := CeilingOf(c)
	assert.Equal(t, SandboxReadOnly, got.Sandbox)
	assert.True(t, *got.AskBeforeWrites)
}

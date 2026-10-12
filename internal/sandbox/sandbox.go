// Package sandbox is the one definition of the agent's three sandbox tiers:
// their names, the strict parser for a tier string and the restrictiveness
// order. It is a leaf (imports nothing of Vibe) so the workspace policy and the
// agent's enforcement share a single parser and cannot drift apart: this is
// security policy, and a tier that one side accepts and the other rejects (or
// ranks differently) is a hole.
package sandbox

import "fmt"

// Mode is the permission level for agent operations.
type Mode string

const (
	ReadOnly  Mode = "read-only"
	ReadWrite Mode = "read-write"
	Full      Mode = "full"
)

// Parse strictly parses s as one of the three known tiers, for callers that
// must reject anything else outright rather than fail closed on a possibly
// stale value.
func Parse(s string) (Mode, error) {
	switch Mode(s) {
	case ReadOnly, ReadWrite, Full:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("unknown sandbox mode: %q (want read-only, read-write, or full)", s)
	}
}

// Rank orders the tiers from most restrictive (read-only, 0) to least (full,
// 2). ok is false for anything that is not one of the three.
func Rank(m Mode) (rank int, ok bool) {
	switch m {
	case ReadOnly:
		return 0, true
	case ReadWrite:
		return 1, true
	case Full:
		return 2, true
	}
	return 0, false
}

// Tighter returns whichever of a/b is the more restrictive tier
// (read-only < read-write < full). An unrecognised value is returned
// immediately, without being ranked: enforcement already fails closed on an
// unknown mode, so propagating it can only deny more, never grant access the
// caller did not intend.
func Tighter(a, b Mode) Mode {
	ra, ok := Rank(a)
	if !ok {
		return a
	}
	rb, ok := Rank(b)
	if !ok {
		return b
	}
	if ra <= rb {
		return a
	}
	return b
}

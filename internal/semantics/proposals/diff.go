package proposals

import "strings"

// maxDiffCells bounds the work of Diff: two texts whose line counts multiply
// to more than this are not compared line by line.
const maxDiffCells = 4_000_000

// DiffLine is one line of a Diff: Op is ' ' (unchanged), '-' (only in the
// current file) or '+' (only in the proposed one).
type DiffLine struct {
	Op   byte   `json:"op"`
	Text string `json:"text"`
}

// Diff compares two texts line by line (longest common subsequence). Lines
// are compared as written; a trailing newline is not a line. When the texts
// are too large to compare cheaply every current line is removed and every
// proposed line added, which is still a correct, if blunt, diff.
func Diff(current, proposed string) []DiffLine {
	a, b := splitLines(current), splitLines(proposed)
	if len(a)*len(b) > maxDiffCells {
		out := make([]DiffLine, 0, len(a)+len(b))
		for _, l := range a {
			out = append(out, DiffLine{'-', l})
		}
		for _, l := range b {
			out = append(out, DiffLine{'+', l})
		}
		return out
	}
	// lcs[i][j] is the length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []DiffLine
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{'-', a[i]})
			i++
		default:
			out = append(out, DiffLine{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, DiffLine{'-', a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, DiffLine{'+', b[j]})
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
}

// Unified renders a Diff with context lines around each change, hunks
// separated by "...". Nothing is printed for identical texts.
func Unified(lines []DiffLine, context int) string {
	keep := make([]bool, len(lines))
	changed := false
	for i, l := range lines {
		if l.Op == ' ' {
			continue
		}
		changed = true
		for k := max(0, i-context); k <= min(len(lines)-1, i+context); k++ {
			keep[k] = true
		}
	}
	if !changed {
		return ""
	}
	var b strings.Builder
	skipped := false
	for i, l := range lines {
		if !keep[i] {
			skipped = true
			continue
		}
		if skipped {
			b.WriteString("...\n")
			skipped = false
		}
		b.WriteByte(l.Op)
		b.WriteByte(' ')
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

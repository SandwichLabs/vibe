package sqllex

import "testing"

func TestTok_ClosedReportsAnOpenQuote(t *testing.T) {
	cases := map[string]bool{
		`'x'`: true, `'x''`: false, `'x`: false, `''`: true, `'it''s'`: true,
		`"x"`: true, `"x""`: false, `"x`: false,
		`E'x\'`: false, `E'x\''`: true, `E'x'`: true,
		`$$x$$`: true, `$t$x$t$`: true, `$$x`: false, `$t$x$$`: false,
	}
	for src, want := range cases {
		stmts := Statements(src)
		if len(stmts) != 1 || len(stmts[0]) != 1 {
			t.Fatalf("%q: want one token, got %v", src, stmts)
		}
		if got := stmts[0][0].Closed; got != want {
			t.Errorf("%q: Closed = %v, want %v", src, got, want)
		}
	}
}

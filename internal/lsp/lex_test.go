package lsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLex_DollarEscapeAndParams(t *testing.T) {
	cases := []struct {
		name string
		src  string
		kind tokKind
		text string
		open bool
		end  int // end of the first token
	}{
		{"empty tag", "$$a;b$$ x", tokString, "a;b", false, 7},
		{"tagged", "$t$ x $y$ y $t$", tokString, " x $y$ y ", false, 15},
		{"unterminated", "$$ a;b", tokString, " a;b", true, 6},
		{"unterminated tagged", "$t$ a $u$", tokString, " a $u$", true, 9},
		{"E string with escaped quote", `E'it\'s;' x`, tokString, `it\'s;`, false, 9},
		{"lower e string", `e'a\\' x`, tokString, `a\\`, false, 6},
		{"E string open", `E'it\'`, tokString, `it\'`, true, 6},
		{"plain string keeps backslash", `'a\' x`, tokString, `a\`, false, 4},
		{"doubled quote", `'a''b;'`, tokString, `a'b;`, false, 7},
		{"positional param", "$1 + 2", tokParam, "$1", false, 2},
		{"named param", "$name, 1", tokParam, "$name", false, 5},
		{"digit tag is not a quote", "$1$ x", tokParam, "$1", false, 2},
		{"lone dollar", "$ x", tokPunct, "$", false, 1},
		{"dollar inside a word", "a$b c", tokWord, "a$b", false, 3},
		{"non-ASCII tag", "$é$a;b$é$ x", tokString, "a;b", false, 11},
		{"nested comment", "/* a /* b */ c */ x", tokComment, "", false, 17},
		{"unterminated nested comment", "/* a /* b */ c", tokComment, "", true, 14},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks := lex(c.src)
			require.NotEmpty(t, toks)
			assert.Equal(t, c.kind, toks[0].kind)
			assert.Equal(t, c.text, toks[0].text)
			assert.Equal(t, c.open, toks[0].open)
			assert.Equal(t, c.end, toks[0].end)
		})
	}
}

func TestLex_EWordIsNotAnEString(t *testing.T) {
	toks := lex("SELECT e FROM t WHERE name = 'x'")
	for _, tk := range toks {
		if tk.text == "e" {
			assert.Equal(t, tokWord, tk.kind)
		}
	}
}

func TestSplit(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		stmts []string
		rest  string
		open  bool
	}{
		{"two", "SELECT 1; SELECT 2", []string{"SELECT 1"}, "SELECT 2", false},
		{"two complete", "SELECT 1; SELECT 2;", []string{"SELECT 1", "SELECT 2"}, "", false},
		{"partial word", "SELECT 1; SEL", []string{"SELECT 1"}, "SEL", false},
		{"open string", "SELECT 'a;", nil, "SELECT 'a;", true},
		{"open quoted name", `SELECT "a;`, nil, `SELECT "a;`, true},
		{"dollar quoted", "SELECT $$;$$;", []string{"SELECT $$;$$"}, "", false},
		{"open dollar", "SELECT $$;", nil, "SELECT $$;", true},
		{"E string", `SELECT E'a\';b';`, []string{`SELECT E'a\';b'`}, "", false},
		{"line comment only", "-- x", nil, "", false},
		{"line comment semicolon", "SELECT 1 -- ;\n;", []string{"SELECT 1 -- ;"}, "", false},
		{"block comment semicolon", "SELECT 1 /* ; */;", []string{"SELECT 1 /* ; */"}, "", false},
		{"open block comment", "SELECT 1 /* ;", nil, "SELECT 1 /* ;", true},
		{"empty statements", ";;", nil, "", false},
		{"blank", "  \n", nil, "", false},
		{"multiline continues", "select 1,\n", nil, "select 1,", false},
		{"nested comment hides a semicolon", "/* x /* y */ ; DROP TABLE keep; /* z */ */ SELECT 1;", []string{"/* x /* y */ ; DROP TABLE keep; /* z */ */ SELECT 1"}, "", false},
		{"non-ASCII dollar tag hides a semicolon", "SELECT $é$ ; DROP TABLE keep; $é$ AS s;", []string{"SELECT $é$ ; DROP TABLE keep; $é$ AS s"}, "", false},
		{"open nested comment", "SELECT 1 /* a /* b */ ;", nil, "SELECT 1 /* a /* b */ ;", true},
		{"carriage return ends a line comment", "SELECT 1 -- x\r; SELECT 2", []string{"SELECT 1 -- x"}, "SELECT 2", false},
		{"param", "SELECT $1; SELECT $a", []string{"SELECT $1"}, "SELECT $a", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stmts, rest, open := Split(c.src)
			assert.Equal(t, c.stmts, stmts)
			assert.Equal(t, c.rest, rest)
			assert.Equal(t, c.open, open)
		})
	}
}

func TestComplete_NothingInsideDollarQuoted(t *testing.T) {
	cat := fixture()
	for _, src := range []string{"SELECT $$ FROM ord|", "SELECT $t$ x FROM ord|", `SELECT E'it\' FROM ord|`} {
		assert.Empty(t, at(src, cat), src)
	}
}

// "$" inside a name is part of the name; after a number it opens a quote.
func TestSplit_DollarInIdentifier(t *testing.T) {
	stmts, _, _ := Split("SELECT 1 AS x$a$; ATTACH 'x' AS y; SELECT $a$")
	require.Len(t, stmts, 2)
	assert.Equal(t, "ATTACH 'x' AS y", stmts[1])
	stmts, _, _ = Split("SELECT 1e5$b$; ATTACH 'x' AS y; $b$;")
	assert.Len(t, stmts, 1)
	stmts, _, _ = Split("SELECT 1a$b$; ATTACH 'x' AS y;")
	assert.Len(t, stmts, 2)
}

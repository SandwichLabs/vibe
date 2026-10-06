package usersql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokens(t *testing.T) {
	got := Tokens("CREATE/**/SECRET s (TYPE http); -- c\nSELECT 'a;b', \"Q\"\"x\", $t$z;$t$, E'it\\'s' FROM f()")
	assert.Equal(t, [][]Token{
		{{TokWord, "CREATE"}, {TokWord, "SECRET"}, {TokWord, "S"}, {TokParen, "("}, {TokWord, "TYPE"}, {TokWord, "HTTP"}, {TokParen, ")"}},
		{{TokWord, "SELECT"}, {TokString, "a;b"}, {TokIdent, `Q"x`}, {TokString, "z;"}, {TokString, "it's"}, {TokWord, "FROM"}, {TokWord, "F"}, {TokParen, "("}, {TokParen, ")"}},
	}, got)
	assert.Empty(t, Tokens("  -- only a comment\n"))
}

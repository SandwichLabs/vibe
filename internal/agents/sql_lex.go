package agents

import "github.com/SandwichLabs/vibe/internal/sqllex"

// The policy's lexer lives in internal/sqllex (a leaf, so the saved-query
// binder can share it); these are the names the policy code uses.

type lexTok = sqllex.Tok

const (
	lkWord   = sqllex.Word
	lkIdent  = sqllex.Ident
	lkString = sqllex.String
	lkParen  = sqllex.Paren
	lkPunct  = sqllex.Punct
)

func lexStatements(src string) [][]lexTok    { return sqllex.Statements(src) }
func matchParen(toks []lexTok, open int) int { return sqllex.MatchParen(toks, open) }

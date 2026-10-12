// Package usersql holds the courtesy checks the CLI and the TUI run on SQL a
// person types, before it reaches the session.
//
// These are not a security boundary. The person at the keyboard is the user
// acting directly, not the agent, and DuckDB's configuration lock does not stop
// ATTACH or INSTALL by itself. The checks catch the common slips (attaching a
// database by hand, changing a setting, creating a secret, writing a file from
// a read-only run) with an explanation of what to do instead. The agent's own
// SQL policy lives in internal/agents and is separate.
package usersql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// Check refuses the statements Vibe manages for you: ATTACH/DETACH,
// INSTALL/LOAD, SET/RESET and CREATE/DROP SECRET. With readOnly it also
// refuses COPY ... TO and EXPORT DATABASE, which write files. It scans each
// statement's leading words, skipping comments, quoted strings (single, double,
// E'...' and $tag$...$tag$), so a semicolon inside a literal is not a boundary.
func Check(sqlText string, readOnly bool) error {
	for _, st := range statements(sqlText) {
		if len(st.words) == 0 {
			continue
		}
		first := st.words[0]
		switch first {
		case "ATTACH", "DETACH":
			return fmt.Errorf("%s is not available here: connections are defined in the workspace (vibe.yaml, or your own connections file), and Vibe attaches them", first)
		case "INSTALL", "LOAD":
			return fmt.Errorf("%s is not available here: the session loads the DuckDB extensions it needs itself", first)
		case "SET", "RESET":
			return fmt.Errorf("%s is not available here: DuckDB settings are fixed when the session opens", first)
		case "COPY":
			if readOnly && st.hasTo {
				return fmt.Errorf("COPY ... TO writes a file, which --read-only does not allow")
			}
		case "EXPORT":
			if readOnly {
				return fmt.Errorf("EXPORT writes files, which --read-only does not allow")
			}
		case "CREATE", "DROP":
			for _, w := range st.words[1:] {
				if w == "SECRET" || w == "SECRETS" {
					return fmt.Errorf("%s SECRET is not available here: credentials live in your OS keychain, never in SQL", first)
				}
				if w != "OR" && w != "REPLACE" && w != "PERSISTENT" && w != "TEMPORARY" && w != "TEMP" && w != "IF" && w != "NOT" && w != "EXISTS" {
					break
				}
			}
		}
	}
	return nil
}

// RequireSelect fails unless sqlText is exactly one SELECT statement (including
// WITH ... SELECT, VALUES and FROM-first queries), judged by DuckDB's own
// parser: json_serialize_sql parses without running anything and serialises
// only a single SELECT. Callers use it before placing text inside COPY (...).
func RequireSelect(ctx context.Context, db *sql.DB, sqlText string) error {
	var out string
	if err := db.QueryRowContext(ctx, "SELECT CAST(json_serialize_sql("+session.QuoteLiteral(sqlText)+") AS VARCHAR)").Scan(&out); err != nil {
		return fmt.Errorf("the query is not a single SELECT statement: %w", err)
	}
	var res struct {
		Error        bool   `json:"error"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return fmt.Errorf("could not check the query: %w", err)
	}
	if res.Error {
		return fmt.Errorf("the query must be exactly one SELECT statement (%s)", strings.TrimSpace(res.ErrorMessage))
	}
	return nil
}

type statement struct {
	words []string // first words, upper-cased; "?" stands for a quoted token
	hasTo bool     // a bare TO keyword appears anywhere in the statement
}

func isWordRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func statements(s string) []statement {
	var out []statement
	var cur statement
	var word strings.Builder
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		w := strings.ToUpper(word.String())
		word.Reset()
		if w == "TO" {
			cur.hasTo = true
		}
		if len(cur.words) < 6 {
			cur.words = append(cur.words, w)
		}
	}
	quoted := func() {
		if len(cur.words) < 6 {
			cur.words = append(cur.words, "?") // a quoted token is never a keyword
		}
	}
	end := func() {
		flushWord()
		out = append(out, cur)
		cur = statement{}
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '-' && i+1 < len(rs) && rs[i+1] == '-':
			flushWord()
			for i < len(rs) && rs[i] != '\n' && rs[i] != '\r' {
				i++
			}
		case r == '/' && i+1 < len(rs) && rs[i+1] == '*':
			flushWord()
			i += 2
			for i+1 < len(rs) && !(rs[i] == '*' && rs[i+1] == '/') {
				i++
			}
			i++
		case r == '$':
			// $tag$ ... $tag$ (tag may be empty); $1 and the like are parameters.
			j := i + 1
			for j < len(rs) && isWordRune(rs[j]) {
				j++
			}
			if j < len(rs) && rs[j] == '$' && (j == i+1 || !(rs[i+1] >= '0' && rs[i+1] <= '9')) {
				delim := string(rs[i : j+1])
				flushWord()
				quoted()
				k := j + 1
				for k < len(rs) && !strings.HasPrefix(string(rs[k:min(k+len(delim), len(rs))]), delim) {
					k++
				}
				i = k + len(delim) - 1
			} else {
				flushWord()
			}
		case r == '\'' || r == '"':
			escape := false
			if r == '\'' && (word.String() == "E" || word.String() == "e") {
				word.Reset() // the E of E'...' is a prefix, not a word
				escape = true
			}
			flushWord()
			quoted()
			for i++; i < len(rs); i++ {
				if escape && rs[i] == '\\' {
					i++
					continue
				}
				if rs[i] == r {
					if i+1 < len(rs) && rs[i+1] == r {
						i++
						continue
					}
					break
				}
			}
		case r == ';':
			end()
		case isWordRune(r):
			word.WriteRune(r)
		default:
			flushWord()
		}
	}
	end()
	return out
}

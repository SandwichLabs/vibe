package lsp

import (
	"fmt"
	"strings"
)

// Hover describes the name under the cursor as markdown: a table or view with
// its columns, a column with its type, or a connection. It returns "" when the
// name means nothing in the snapshot.
func Hover(text string, offset int, cat *Catalog) string {
	if cat == nil || offset > len(text) {
		return ""
	}
	toks, ok := statementTokens(text, offset)
	if !ok {
		return ""
	}
	path := pathAt(toks, offset)
	if len(path) == 0 {
		return ""
	}
	refs := tableRefs(toks, cat)
	if rel, where := cat.resolve(path); rel != nil {
		return relationDoc(rel, where)
	}
	if len(path) >= 2 {
		return ownedColumnDoc(path, refs, cat)
	}
	for _, r := range refs {
		if r.rel == nil {
			continue
		}
		if c := findColumn(r.rel, path[0]); c != nil {
			return columnDoc(c, r.where)
		}
	}
	if cn := cat.Conn(path[0]); cn != nil {
		return fmt.Sprintf("**%s** — %s connection, %d schema(s)", cn.Name, strings.TrimSpace(cn.Type), len(cn.Schemas))
	}
	return ""
}

// pathAt is the dotted name ending at the name token under offset:
// for `pg.public.orders` with the cursor on "public", [pg public].
func pathAt(toks []token, offset int) []string {
	at := -1
	for i, t := range toks {
		if t.isName() && t.start <= offset && offset <= t.end {
			at = i
			break
		}
	}
	if at < 0 {
		return nil
	}
	path := []string{toks[at].text}
	for i := at; i >= 2 && isDot(toks[i-1]) && toks[i-2].isName(); i -= 2 {
		path = append([]string{toks[i-2].text}, path...)
	}
	return path
}

// ownedColumnDoc describes owner.column, where owner is an alias, a table the
// statement names, or a catalog path.
func ownedColumnDoc(path []string, refs []tableRef, cat *Catalog) string {
	owner, col := path[:len(path)-1], path[len(path)-1]
	if len(owner) == 1 {
		for _, r := range refs {
			if r.rel != nil && strings.EqualFold(r.alias, owner[0]) {
				if c := findColumn(r.rel, col); c != nil {
					return columnDoc(c, r.where)
				}
			}
		}
	}
	if rel, where := cat.resolve(owner); rel != nil {
		if c := findColumn(rel, col); c != nil {
			return columnDoc(c, where)
		}
	}
	return ""
}

func findColumn(rel *Relation, name string) *Column {
	for i := range rel.Columns {
		if strings.EqualFold(rel.Columns[i].Name, name) {
			return &rel.Columns[i]
		}
	}
	return nil
}

func relationDoc(rel *Relation, where string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** `%s`\n\n", rel.Kind, where)
	for _, c := range rel.Columns {
		fmt.Fprintf(&b, "- `%s` %s\n", c.Name, c.Type)
	}
	return b.String()
}

func columnDoc(c *Column, where string) string {
	return fmt.Sprintf("**column** `%s` %s\n\nin `%s`", c.Name, c.Type, where)
}

package artifacts

import (
	"fmt"
	"sort"
	"strings"
	"text/template/parse"

	"github.com/SandwichLabs/vibe/internal/artifacts/pagefuncs"
)

// This file is the part of the save-time page vet that catches a template which
// parses and passes every safety rule but fails (or shows nothing) the first
// time it renders: a date layout that is not one of the closed names, and a
// field read on a row. A row of a query result is an array of cells, not a
// record, so `.activity` is an error at render ("can't evaluate field
// activity in type []interface {}").
//
// Both checks are conservative: they report only what they can see for
// certain, from a `query "name"` result, its .Rows, and the variables and dots
// those flow into. Anything they cannot follow is left to the renderer.

// dateLayoutNames are the layouts the date function accepts.
var dateLayoutNames = []string{"iso", "short", "long", "month"}

// resultFields are the fields of what query returns (pagefuncs.Result).
var resultFields = map[string]bool{"Columns": true, "Rows": true, "Truncated": true}

type valKind int

const (
	valOther  valKind = iota
	valResult         // the value of query "name"
	valRows           // its .Rows: a list of rows
	valRow            // one row: an array of cells
)

type pageLint struct {
	file     string
	vars     map[string]valKind
	problems []string
}

func (l *pageLint) addf(format string, args ...any) {
	l.problems = append(l.problems, fmt.Sprintf("html/%s: ", l.file)+fmt.Sprintf(format, args...))
}

// lintPageTemplates walks every parsed template and returns its problems, in
// template-name order so the output is stable.
func lintPageTemplates(names map[string]string, trees map[string]*parse.Tree) []string {
	keys := make([]string, 0, len(trees))
	for k := range trees {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		t := trees[k]
		if t == nil || t.Root == nil {
			continue
		}
		l := &pageLint{file: names[k], vars: map[string]valKind{}}
		l.list(t.Root, valOther)
		out = append(out, l.problems...)
	}
	return out
}

func (l *pageLint) list(n *parse.ListNode, dot valKind) {
	if n == nil {
		return
	}
	for _, c := range n.Nodes {
		l.node(c, dot)
	}
}

func (l *pageLint) node(n parse.Node, dot valKind) {
	switch n := n.(type) {
	case *parse.ActionNode:
		k := l.pipe(n.Pipe, dot)
		for _, d := range n.Pipe.Decl {
			l.vars[d.Ident[0]] = k
		}
	case *parse.IfNode:
		l.pipe(n.Pipe, dot)
		l.list(n.List, dot)
		l.list(n.ElseList, dot)
	case *parse.WithNode:
		k := l.pipe(n.Pipe, dot)
		l.bindDecl(n.Pipe, k, false)
		l.list(n.List, k)
		l.list(n.ElseList, dot)
	case *parse.RangeNode:
		k := l.pipe(n.Pipe, dot)
		inner := valOther
		if k == valRows {
			inner = valRow
		}
		l.bindDecl(n.Pipe, inner, true)
		l.list(n.List, inner)
		l.list(n.ElseList, dot)
	case *parse.TemplateNode:
		l.pipe(n.Pipe, dot)
	}
}

// bindDecl records the kind of a with/range variable: with $x := v binds v;
// range $e := rows binds the element, and range $i, $e := rows binds $e.
func (l *pageLint) bindDecl(p *parse.PipeNode, k valKind, isRange bool) {
	if p == nil || len(p.Decl) == 0 {
		return
	}
	last := p.Decl[len(p.Decl)-1]
	l.vars[last.Ident[0]] = k
	if isRange && len(p.Decl) == 2 {
		l.vars[p.Decl[0].Ident[0]] = valOther
	}
}

// pipe checks every command of a pipeline and returns the kind of its value
// (known only for a single command).
func (l *pageLint) pipe(p *parse.PipeNode, dot valKind) valKind {
	if p == nil {
		return valOther
	}
	var kind valKind
	for i, c := range p.Cmds {
		kind = l.command(c, dot, i)
	}
	if len(p.Cmds) != 1 {
		return valOther
	}
	return kind
}

func (l *pageLint) command(c *parse.CommandNode, dot valKind, pos int) valKind {
	for _, a := range c.Args {
		l.argChecks(a, dot)
	}
	if id, ok := c.Args[0].(*parse.IdentifierNode); ok {
		return l.call(id.Ident, c, dot, pos)
	}
	if len(c.Args) == 1 {
		return l.argKind(c.Args[0], dot)
	}
	return valOther
}

// call checks a function call and returns what it yields when that is known.
func (l *pageLint) call(name string, c *parse.CommandNode, dot valKind, pos int) valKind {
	switch name {
	case pagefuncs.Query:
		return valResult
	case "index":
		if len(c.Args) == 3 && l.argKind(c.Args[1], dot) == valRows {
			return valRow
		}
	case pagefuncs.Date:
		l.checkDate(c, pos)
	}
	return valOther
}

func (l *pageLint) checkDate(c *parse.CommandNode, pos int) {
	switch {
	case len(c.Args) == 3:
		if s, ok := c.Args[2].(*parse.StringNode); ok && !isDateLayout(s.Text) {
			l.addf("date layout %q is not one of %s: the layout is a name, not a Go time layout (date (param \"from\") \"long\")", s.Text, strings.Join(dateLayoutNames, ", "))
		}
	case len(c.Args) == 2 && pos > 0:
		if _, ok := c.Args[1].(*parse.StringNode); ok {
			l.addf("a piped value reaches date as its last argument, so {{ x | date \"long\" }} passes \"long\" as the value: write the value first, date x \"long\"")
		}
	}
}

func isDateLayout(s string) bool {
	for _, n := range dateLayoutNames {
		if s == n {
			return true
		}
	}
	return false
}

// argChecks looks inside one argument for a read of a field of a row or a
// result, and for nested pipelines.
func (l *pageLint) argChecks(a parse.Node, dot valKind) {
	switch a := a.(type) {
	case *parse.FieldNode:
		l.checkField(a.Ident, dot, "")
	case *parse.VariableNode:
		if k, ok := l.vars[a.Ident[0]]; ok && len(a.Ident) > 1 {
			l.checkField(a.Ident[1:], k, a.Ident[0])
		}
	case *parse.PipeNode:
		l.pipe(a, dot)
	case *parse.ChainNode:
		if p, ok := a.Node.(*parse.PipeNode); ok {
			if k := l.pipe(p, dot); k != valOther {
				l.checkField(a.Field, k, "")
			}
		}
	}
}

// checkField reports a field read on a row (never valid) or on a result (only
// Rows, Columns and Truncated exist). base is the variable it was read from.
func (l *pageLint) checkField(idents []string, k valKind, base string) {
	if len(idents) == 0 {
		return
	}
	from := "." + idents[0]
	if base != "" {
		from = base + "." + idents[0]
	}
	switch k {
	case valRow:
		l.addf("%s: a row of a query result is an array of cells, not a record, so it has no fields. Read a cell by position: {{ index . 0 }} (or {{ index $row 0 }}); the positions follow the query's SELECT list, and $q.Columns lists the names", from)
	case valResult:
		if !resultFields[idents[0]] {
			l.addf("%s: a query result has only .Rows, .Columns and .Truncated (names are case-sensitive)", from)
		}
	}
}

// argKind is the kind of a single argument used as a whole value.
func (l *pageLint) argKind(a parse.Node, dot valKind) valKind {
	switch a := a.(type) {
	case *parse.DotNode:
		return dot
	case *parse.FieldNode:
		if dot == valResult && len(a.Ident) == 1 && a.Ident[0] == "Rows" {
			return valRows
		}
	case *parse.VariableNode:
		k, ok := l.vars[a.Ident[0]]
		switch {
		case !ok:
		case len(a.Ident) == 1:
			return k
		case len(a.Ident) == 2 && k == valResult && a.Ident[1] == "Rows":
			return valRows
		}
	case *parse.PipeNode:
		return l.pipeKindQuiet(a, dot)
	case *parse.ChainNode:
		if p, ok := a.Node.(*parse.PipeNode); ok && len(a.Field) == 1 && a.Field[0] == "Rows" && l.pipeKindQuiet(p, dot) == valResult {
			return valRows
		}
	}
	return valOther
}

// pipeKindQuiet is the kind of a parenthesised pipeline without reporting its
// problems again (command has already walked it).
func (l *pageLint) pipeKindQuiet(p *parse.PipeNode, dot valKind) valKind {
	if len(p.Cmds) != 1 || len(p.Cmds[0].Args) == 0 {
		return valOther
	}
	c := p.Cmds[0]
	if id, ok := c.Args[0].(*parse.IdentifierNode); ok {
		switch {
		case id.Ident == pagefuncs.Query:
			return valResult
		case id.Ident == "index" && len(c.Args) == 3 && l.argKind(c.Args[1], dot) == valRows:
			return valRow
		}
		return valOther
	}
	if len(c.Args) == 1 {
		return l.argKind(c.Args[0], dot)
	}
	return valOther
}

package artifacts

import (
	"context"
	"fmt"
	"html/template"
	"reflect"
	"sort"
	"text/template/parse"

	"github.com/SandwichLabs/assay/internal/artifacts/pagefuncs"
)

// The render budget. See the package comment in render.go: the parsed trees
// are rewritten so that every range and every template call is charged to one
// budget before it runs.
//
//	{{ range X }}      becomes   {{ range X | assayRange__ }}
//	{{ template "t" }} is preceded by {{ $assayStep__ := assayStep__ }}
//	every other action, if, with and range is preceded by {{ $assayStep__ := assayTick__ }}
//
// assayRange__ returns its argument unchanged after charging what the range
// will iterate (the length of a slice or map, the value of an integer), so a
// range over three billion numbers is refused before its first iteration, and
// nested loops are charged once per time the inner one starts. assayStep__
// charges one step and checks the render's context; assayTick__ only checks the
// context (the deadline, a cancel), and runs before every action in every
// list, so inside a loop body the worst overrun after a deadline or a cancel
// is one action, not the rest of the loop. A declaration prints
// nothing and html/template's escaper skips it, so the page's output and its
// escaping are untouched. The two names are added to the function map after
// the author's text is parsed, so a template cannot call them.

const (
	rangeGuardName = "assayRange__"
	stepGuardName  = "assayStep__"
	tickGuardName  = "assayTick__"
	stepVarName    = "$assayStep__"
)

type workBudget struct {
	ctx  context.Context
	left int64
	// strLeft is the bytes of string the page may still build (pagefuncs.Meter).
	strLeft int64
}

// maxPageStrings is a render's string budget: a few times MaxRenderBytes, so a
// page can build what it prints and some scratch, but not a gigabyte in variables.
const maxPageStrings = 64 << 20

// Check implements pagefuncs.Meter: would n more bytes of string fit.
func (b *workBudget) Check(n int64) error {
	if n > b.strLeft {
		return pagefuncs.ErrStringBudget
	}
	return b.ctx.Err()
}

// Charge implements pagefuncs.Meter.
func (b *workBudget) Charge(n int64) error {
	if err := b.Check(n); err != nil {
		return err
	}
	b.strLeft -= n
	return nil
}

func (b *workBudget) charge(n int64) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if n < 0 {
		n = 0
	}
	if n > b.left {
		b.left = 0
		return errPageBudget
	}
	b.left -= n
	return nil
}

// rangeGuard charges a range's iterations and returns the value untouched.
func (b *workBudget) rangeGuard(v any) (any, error) {
	n := int64(1)
	if v != nil {
		switch rv := reflect.ValueOf(v); rv.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map, reflect.Chan:
			n = int64(rv.Len())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n = rv.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			if u := rv.Uint(); u > uint64(renderBudget) {
				n = int64(renderBudget) + 1
			} else {
				n = int64(u)
			}
		}
	}
	// Even an empty range costs one, so a loop of loops cannot run free.
	if err := b.charge(n + 1); err != nil {
		return nil, err
	}
	return v, nil
}

// stepGuard charges one template call.
func (b *workBudget) stepGuard() (int, error) { return 0, b.charge(1) }

// tickGuard checks the render's context and charges nothing.
func (b *workBudget) tickGuard() (int, error) { return 0, b.ctx.Err() }

// instrument adds the guards to every parsed tree of root and refuses a set
// of templates that call each other in a cycle.
func instrument(root *template.Template, b *workBudget) error {
	root.Funcs(template.FuncMap{rangeGuardName: b.rangeGuard, stepGuardName: b.stepGuard, tickGuardName: b.tickGuard})
	calls := map[string][]string{}
	for _, t := range root.Templates() {
		if t.Tree == nil || t.Tree.Root == nil {
			continue
		}
		var refs []string
		t.Tree.Root = guardList(t.Tree.Root, &refs)
		calls[t.Name()] = refs
	}
	return refuseCycles(calls)
}

// guardList returns the list with its template calls preceded by a step and
// its ranges guarded, recursing into branches. refs collects the names called.
func guardList(l *parse.ListNode, refs *[]string) *parse.ListNode {
	if l == nil {
		return nil
	}
	out := make([]parse.Node, 0, len(l.Nodes))
	for _, n := range l.Nodes {
		switch x := n.(type) {
		case *parse.TemplateNode:
			*refs = append(*refs, x.Name)
			out = append(out, stepDecl(x.Pos, x.Line, stepGuardName))
		case *parse.ActionNode:
			out = append(out, stepDecl(x.Pos, x.Line, tickGuardName))
		case *parse.IfNode:
			out = append(out, stepDecl(x.Pos, x.Line, tickGuardName))
			x.List, x.ElseList = guardList(x.List, refs), guardList(x.ElseList, refs)
		case *parse.WithNode:
			out = append(out, stepDecl(x.Pos, x.Line, tickGuardName))
			x.List, x.ElseList = guardList(x.List, refs), guardList(x.ElseList, refs)
		case *parse.RangeNode:
			out = append(out, stepDecl(x.Pos, x.Line, tickGuardName))
			guardRangePipe(x.Pipe)
			x.List, x.ElseList = guardList(x.List, refs), guardList(x.ElseList, refs)
		}
		out = append(out, n)
	}
	l.Nodes = out
	return l
}

// guardRangePipe appends the guard to a range's pipeline: "X | assayRange__".
func guardRangePipe(p *parse.PipeNode) {
	id := parse.NewIdentifier(rangeGuardName).SetPos(p.Pos)
	p.Cmds = append(p.Cmds, &parse.CommandNode{NodeType: parse.NodeCommand, Pos: p.Pos, Args: []parse.Node{id}})
}

// stepDecl is {{ $assayStep__ := <guard> }}, placed at pos.
func stepDecl(pos parse.Pos, line int, guard string) parse.Node {
	id := parse.NewIdentifier(guard).SetPos(pos)
	return &parse.ActionNode{
		NodeType: parse.NodeAction, Pos: pos, Line: line,
		Pipe: &parse.PipeNode{
			NodeType: parse.NodePipe, Pos: pos, Line: line,
			Decl: []*parse.VariableNode{{NodeType: parse.NodeVariable, Pos: pos, Ident: []string{stepVarName}}},
			Cmds: []*parse.CommandNode{{NodeType: parse.NodeCommand, Pos: pos, Args: []parse.Node{id}}},
		},
	}
}

// refuseCycles reports a template that can call itself, directly or through
// others: a render's call depth must be bounded by the number of templates.
func refuseCycles(calls map[string][]string) error {
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case visiting:
			return fmt.Errorf("template %q calls itself (recursion is not allowed in a page)", name)
		case done:
			return nil
		}
		state[name] = visiting
		for _, next := range calls[name] {
			if err := visit(next); err != nil {
				return err
			}
		}
		state[name] = done
		return nil
	}
	names := make([]string, 0, len(calls))
	for n := range calls {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := visit(n); err != nil {
			return err
		}
	}
	return nil
}

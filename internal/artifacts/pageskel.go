package artifacts

import (
	"strconv"
	"strings"
	"text/template/parse"
)

// A template is not HTML until it runs, so the URL allow-list and the
// static/scripted test cannot read it as HTML directly. They read a skeleton:
// the parsed template with every template action replaced by a marker
// character sequence, so the HTML tokenizer sees the author's markup with the
// places data will go marked and in place. Text nodes are copied as written.
// Both branches of an {{if}}, {{range}} or {{with}} are copied, one after the
// other, between control markers, so whatever markup either branch can
// produce is visible. A {{template}} call is replaced by the callee's
// skeleton, so the tokenizer sees what the browser will: a partial that opens
// or closes <style> or <svg> does so in the caller's context.
//
// The marker characters are private-use code points that a template file may
// not contain (checkPageText refuses them), so a marker in the skeleton is
// always one this code put there.
const (
	markOpen  rune = ''
	markClose rune = ''
)

type markKind int

const (
	markAction markKind = iota
	markControl
)

type mark struct {
	kind markKind
	// linkCall is set on an action that is a bare `link ...` call: one
	// command, no variable declaration. Only such an action may fill a URL
	// attribute.
	linkCall bool
}

// span is a stretch of the skeleton that came from one template file.
type span struct {
	start, end int
	file       string
}

// Bounds on the expansion, so a page cannot make the vet build a huge text
// out of a small file set (a chain of templates that each call the next twice).
const (
	maxSkeletonBytes = 2 << 20
	maxExpandCalls   = 5000
)

// budget is shared by every skeleton of one page, so a page with many
// templates that each call the same big one cannot make the vet do the
// expansion once per template.
type budget struct{ calls, bytes int }

type skeleton struct {
	text  strings.Builder
	marks []mark
	spans []span
	b     *budget
	err   error
	// trees and fileOf are the page's templates by name, and the file that
	// defines each; stack is the chain of templates being expanded.
	trees   map[string]*parse.Tree
	fileOf  map[string]string
	stack   []string
	visited map[string]bool
}

func (sk *skeleton) mark(m mark) {
	sk.text.WriteRune(markOpen)
	sk.text.WriteString(strconv.Itoa(len(sk.marks)))
	sk.text.WriteRune(markClose)
	sk.marks = append(sk.marks, m)
}

// skeletonOf builds the skeleton of the named template with every
// {{template}} call replaced by the callee's own skeleton, at the call site.
// A template does not run alone: it runs where its caller put it, and the
// caller may be inside <style>, <textarea> or <svg>, or the callee may close
// what the caller opened. Reading each template alone would tokenize it in the
// wrong state. A call cycle, or an expansion over the bounds, is refused.
// visited collects the template names that were expanded.
func skeletonOf(trees map[string]*parse.Tree, fileOf map[string]string, name string, visited map[string]bool, b *budget) (*skeleton, error) {
	sk := &skeleton{trees: trees, fileOf: fileOf, visited: visited, b: b}
	sk.expand(name)
	b.bytes += sk.text.Len()
	return sk, sk.err
}

func (sk *skeleton) fail(format string, args ...any) {
	if sk.err == nil {
		sk.err = invalid(format, args...)
	}
}

func (sk *skeleton) expand(name string) {
	if sk.err != nil {
		return
	}
	for _, n := range sk.stack {
		if n == name {
			sk.fail("html/%s: template %q calls itself (templates may not be recursive)", sk.fileOf[name], name)
			return
		}
	}
	sk.b.calls++
	if sk.b.calls > maxExpandCalls {
		sk.fail("the page's templates call each other more than %d times when expanded", maxExpandCalls)
		return
	}
	sk.visited[name] = true
	tree := sk.trees[name]
	if tree == nil || tree.Root == nil {
		return
	}
	sk.stack = append(sk.stack, name)
	start := sk.text.Len()
	sk.list(tree.Root)
	sk.spans = append(sk.spans, span{start: start, end: sk.text.Len(), file: sk.fileOf[name]})
	sk.stack = sk.stack[:len(sk.stack)-1]
	if sk.b.bytes+sk.text.Len() > maxSkeletonBytes {
		sk.fail("the page's templates expand to more than %d bytes", maxSkeletonBytes)
	}
}

// originAt names the template file the skeleton byte at offset came from (the
// innermost span that holds it).
func (sk *skeleton) originAt(offset int) string {
	best, size := "", -1
	for _, s := range sk.spans {
		if offset >= s.start && offset < s.end && (size < 0 || s.end-s.start < size) {
			best, size = s.file, s.end-s.start
		}
	}
	return best
}

func (sk *skeleton) list(l *parse.ListNode) {
	if l == nil || sk.err != nil {
		return
	}
	for _, n := range l.Nodes {
		switch n := n.(type) {
		case *parse.TextNode:
			sk.text.Write(n.Text)
		case *parse.ActionNode:
			sk.mark(mark{kind: markAction, linkCall: isLinkCall(n.Pipe)})
		case *parse.IfNode:
			sk.branch(&n.BranchNode)
		case *parse.RangeNode:
			// A range body runs more than once, so what one pass leaves open
			// (an <svg>, a <style>) is in force at the start of the next:
			// read the body twice.
			sk.branchTimes(&n.BranchNode, 2)
		case *parse.WithNode:
			sk.branch(&n.BranchNode)
		case *parse.TemplateNode:
			sk.expand(n.Name)
		}
	}
}

func (sk *skeleton) branch(b *parse.BranchNode) { sk.branchTimes(b, 1) }

func (sk *skeleton) branchTimes(b *parse.BranchNode, times int) {
	sk.mark(mark{kind: markControl})
	for i := 0; i < times; i++ {
		sk.list(b.List)
		if sk.text.Len()+sk.b.bytes > maxSkeletonBytes {
			sk.fail("the page's templates expand to more than %d bytes", maxSkeletonBytes)
			return
		}
	}
	if b.ElseList != nil {
		sk.mark(mark{kind: markControl})
		sk.list(b.ElseList)
	}
	sk.mark(mark{kind: markControl})
}

func isLinkCall(p *parse.PipeNode) bool {
	if p == nil || len(p.Decl) > 0 || len(p.Cmds) != 1 || len(p.Cmds[0].Args) == 0 {
		return false
	}
	id, ok := p.Cmds[0].Args[0].(*parse.IdentifierNode)
	return ok && id.Ident == "link"
}

func hasMark(s string) bool { return strings.ContainsAny(s, string([]rune{markOpen, markClose})) }

// markIndex reads s when it is exactly one marker and nothing else.
func markIndex(s string) (int, bool) {
	rs := []rune(s)
	if len(rs) < 3 || rs[0] != markOpen || rs[len(rs)-1] != markClose {
		return 0, false
	}
	n, err := strconv.Atoi(string(rs[1 : len(rs)-1]))
	if err != nil {
		return 0, false
	}
	return n, true
}

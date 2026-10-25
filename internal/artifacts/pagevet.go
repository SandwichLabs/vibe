package artifacts

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
	"text/template/parse"

	"github.com/SandwichLabs/vibe/internal/artifacts/pagefuncs"
)

// Derived page kinds.
const (
	PageStatic   = "static"
	PageScripted = "scripted"
)

// PageInfo is what the save-time vet derived from a page's files.
type PageInfo struct {
	// Kind is PageStatic or PageScripted, derived from the files, never
	// declared: any .js/.mjs file, a <script> element or an inline on*
	// handler in any template makes a page scripted.
	Kind  string
	Entry string
	// ScriptedBy names the files that made the page scripted (empty for a
	// static page), for the allow prompt.
	ScriptedBy []string
	// Queries, Params are the manifest names the templates use, sorted: the
	// queries a page uses (the allow prompt lists them).
	Queries []string
	Params  []string
	// Capabilities is the manifest's declared capabilities, sorted.
	Capabilities []string
	// Hash is PageHash of the page as stored (artifact.yaml, the files, the
	// kind and the capabilities): what an allow will be keyed by. Set by the
	// stores, not by VetPage.
	Hash string
}

// VetPage is the save-time check of a view: html artifact (m must have passed
// Validate). It refuses a file set that breaks H1's rules and a template that
// breaks H2's: every template must parse against the closed function map with
// missingkey=error, every query/param/link name must be a literal naming a
// manifest entry, and every URL attribute must be a link call, a #fragment or
// a literal relative assets/ path. It never renders and never runs SQL.
//
// The templates are read as skeletons (see pageskel.go): the author's markup
// with each action replaced by a marker, tokenized with the HTML tokenizer
// html/template itself is built on.
func VetPage(m *Manifest, files []PageFile) (*PageInfo, error) { return vetPage(m, files, false) }

// VetPageForWrite is VetPage plus the rules that only a write is held to: a
// literal date layout that is not a name, a field read on a query row, and an
// inline <script>. They catch a page that parses and passes every safety rule
// but fails (or never runs) when it renders. They are not part of VetPage
// because VetPage also runs when a stored page is loaded, and a page already
// on disk must still list, load and be repairable; the stores and the agent
// call this one when a page is written.
func VetPageForWrite(m *Manifest, files []PageFile) (*PageInfo, error) {
	return vetPage(m, files, true)
}

func vetPage(m *Manifest, files []PageFile, strict bool) (*PageInfo, error) {
	if m.View != ViewHTML || m.HTML == nil {
		return nil, invalid("the manifest is not a view: %s", ViewHTML)
	}
	files = sortedFiles(files)
	entry := m.HTML.Entry
	roles, err := checkPageFiles(entry, files)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if err := checkPageContent(f); err != nil {
			return nil, err
		}
	}
	v := &pageVet{strict: strict, m: m, files: map[string]bool{}, queries: map[string]bool{}, params: map[string]bool{}, names: map[string]string{}, trees: map[string]*parse.Tree{}}
	for _, f := range files {
		v.files[f.Path] = true
	}
	scripted := map[string]bool{}
	for _, f := range files {
		switch roles[f.Path] {
		case RoleScript:
			scripted[f.Path] = true
		case RoleEntry, RolePartial:
			if err := v.parseTemplate(f); err != nil {
				return nil, err
			}
		}
	}
	if err := v.checkIncludes(); err != nil {
		return nil, err
	}
	// The lint findings and the markup rules are independent: report both.
	var lint []string
	if strict {
		lint = lintPageTemplates(v.names, v.trees)
	}
	if err := v.vetMarkup(entry, scripted); err != nil || len(lint) > 0 {
		return nil, pageProblems(lint, err)
	}
	if err := escapeCheck(files, roles); err != nil {
		return nil, err
	}
	info := &PageInfo{Kind: PageStatic, Entry: entry, Queries: sortedKeys(v.queries), Params: sortedKeys(v.params), Capabilities: CapabilityNames(m)}
	if len(scripted) > 0 {
		info.Kind, info.ScriptedBy = PageScripted, sortedKeys(scripted)
	}
	if err := checkHostFit(m, info); err != nil {
		return nil, err
	}
	return info, nil
}

// checkHostFit refuses a declaration that does not fit the derived host: a
// window and capabilities exist only for a scripted page, so the manifest can
// never promise something the page's host does not offer.
func checkHostFit(m *Manifest, info *PageInfo) error {
	if info.Kind == PageStatic {
		if len(m.HTML.Capabilities) > 0 {
			return invalid("html.capabilities need script, and this page has none (it is a static page)")
		}
		if m.HTML.Window != nil {
			return invalid("html.window needs script, and this page has none (it is a static page)")
		}
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type pageVet struct {
	strict  bool
	m       *Manifest
	files   map[string]bool
	queries map[string]bool
	params  map[string]bool
	// names maps every template name (file paths and {{define}}s) to the file
	// that defines it; trees holds each one's parse tree (nil when empty).
	names map[string]string
	trees map[string]*parse.Tree
	// includes are the {{template "x"}} names, to be resolved against names.
	includes []includeRef
}

type includeRef struct{ name, file string }

// stubFuncs is the closed function map with no data behind it, for parsing.
func stubFuncs() template.FuncMap { return pagefuncs.Funcs(pagefuncs.StubEnv{}) }

// parseTemplate parses one template file and vets its function calls. The
// markup is vetted afterwards, once every template is known, because a template
// runs where its caller put it (see vetMarkup).
func (v *pageVet) parseTemplate(f PageFile) error {
	t, err := template.New(f.Path).Option("missingkey=error").Funcs(stubFuncs()).Parse(string(f.Data))
	if err != nil {
		return invalid("html/%s: %v", f.Path, err)
	}
	for _, tt := range t.Templates() {
		if strings.HasPrefix(tt.Name(), vetRootPrefix) {
			return invalid("html/%s: template names may not start with %q", f.Path, vetRootPrefix)
		}
		if prev, dup := v.names[tt.Name()]; dup {
			return invalid("html/%s: template %q is already defined in html/%s", f.Path, tt.Name(), prev)
		}
		v.names[tt.Name()] = f.Path
		v.trees[tt.Name()] = tt.Tree
		if tt.Tree == nil {
			continue
		}
		if err := v.vetFuncs(f.Path, tt.Tree.Root); err != nil {
			return err
		}
	}
	return nil
}

// vetMarkup reads the page as markup: the entry template with every call
// expanded in place, then every template the entry does not reach on its own
// (an unused partial is still a file the person is asked to allow). A template
// the entry reaches is read only in the context its callers give it.
func (v *pageVet) vetMarkup(entry string, scripted map[string]bool) error {
	visited := map[string]bool{}
	bud := &budget{}
	roots := []string{entry}
	var rest []string
	for n := range v.trees {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	roots = append(roots, rest...)
	for _, name := range roots {
		if visited[name] || v.trees[name] == nil {
			continue
		}
		sk, err := skeletonOf(v.trees, v.names, name, visited, bud)
		if err != nil {
			return err
		}
		origins, err := vetMarkup(v.names[name], sk, v.files, v.strict)
		if err != nil {
			return err
		}
		for _, o := range origins {
			scripted[o] = true
		}
	}
	return nil
}

func (v *pageVet) checkIncludes() error {
	for _, inc := range v.includes {
		if _, ok := v.names[inc.name]; !ok {
			return invalid("html/%s: {{template %q}} names no template (templates are the .tmpl files directly under %s/ and {{define}} blocks)", inc.file, inc.name, HTMLDir)
		}
	}
	return nil
}

// vetRootPrefix names the synthetic templates the escape check adds.
const vetRootPrefix = "__vet_"

// errVetStop is what the escape check's stop function returns.
var errVetStop = errors.New("vet stop")

// escapeCheck runs html/template's context escaper over every template, so an
// escaping error (a branch that ends in another HTML context, an action in a
// place that cannot be escaped, an undefined template) is refused at save and
// not on first render. It never runs the page: html/template escapes a
// template, and everything it calls, the first time the template is executed,
// before it produces any output. So each template is executed through a
// synthetic root that starts with a function that fails; the escaper has then
// done its whole job and nothing of the page's own actions has run (a range
// over three billion numbers, or a chain of calls that doubles at each level,
// costs nothing). Only html/template's own *template.Error counts.
func escapeCheck(files []PageFile, roles map[string]string) error {
	stop := func() (string, error) { return "", errVetStop }
	root := template.New("page").Option("missingkey=error").Funcs(stubFuncs()).Funcs(template.FuncMap{"vetStop": stop})
	for _, f := range files {
		if roles[f.Path] != RoleEntry && roles[f.Path] != RolePartial {
			continue
		}
		if _, err := root.New(f.Path).Parse(string(f.Data)); err != nil {
			return invalid("html/%s: %v", f.Path, err)
		}
	}
	var names []string
	for _, t := range root.Templates() {
		if t.Tree != nil && t.Name() != "page" {
			names = append(names, t.Name())
		}
	}
	sort.Strings(names)
	for i, n := range names {
		if _, err := root.New(fmt.Sprintf("%s%d", vetRootPrefix, i)).Parse(fmt.Sprintf("{{ vetStop }}{{ template %q . }}", n)); err != nil {
			return invalid("html/template: %v", err)
		}
	}
	for i := range names {
		err := root.ExecuteTemplate(io.Discard, fmt.Sprintf("%s%d", vetRootPrefix, i), nil)
		var te *template.Error
		if errors.As(err, &te) {
			return invalid("html/template: %v", te)
		}
	}
	return nil
}

// --- function calls ---------------------------------------------------------

// vetFuncs walks a template tree and checks every call to a data function:
// query, param and link take literal names that the manifest declares, and the
// builtin "call" (which would call a function value) is refused.
func (v *pageVet) vetFuncs(file string, n parse.Node) error {
	switch n := n.(type) {
	case nil:
		return nil
	case *parse.ListNode:
		if n == nil {
			return nil
		}
		for _, c := range n.Nodes {
			if err := v.vetFuncs(file, c); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		return v.vetPipe(file, n.Pipe)
	case *parse.IfNode:
		return v.vetBranch(file, &n.BranchNode)
	case *parse.RangeNode:
		return v.vetBranch(file, &n.BranchNode)
	case *parse.WithNode:
		return v.vetBranch(file, &n.BranchNode)
	case *parse.TemplateNode:
		v.includes = append(v.includes, includeRef{name: n.Name, file: file})
		return v.vetPipe(file, n.Pipe)
	}
	return nil
}

func (v *pageVet) vetBranch(file string, b *parse.BranchNode) error {
	if err := v.vetPipe(file, b.Pipe); err != nil {
		return err
	}
	if err := v.vetFuncs(file, b.List); err != nil {
		return err
	}
	if b.ElseList != nil {
		return v.vetFuncs(file, b.ElseList)
	}
	return nil
}

func (v *pageVet) vetPipe(file string, p *parse.PipeNode) error {
	if p == nil {
		return nil
	}
	for _, c := range p.Cmds {
		if err := v.vetCommand(file, c); err != nil {
			return err
		}
	}
	return nil
}

func (v *pageVet) vetCommand(file string, c *parse.CommandNode) error {
	for i, a := range c.Args {
		switch a := a.(type) {
		case *parse.PipeNode:
			if err := v.vetPipe(file, a); err != nil {
				return err
			}
		case *parse.ChainNode:
			switch n := a.Node.(type) {
			case *parse.PipeNode:
				if err := v.vetPipe(file, n); err != nil {
					return err
				}
			case *parse.IdentifierNode:
				return invalid("html/%s: a function cannot be chained directly (%s.%s): put it in parentheses or a variable first", file, n.Ident, strings.Join(a.Field, "."))
			}
		case *parse.IdentifierNode:
			if err := v.vetIdentifier(file, a.Ident, c, i); err != nil {
				return err
			}
		}
	}
	return nil
}

// vetIdentifier checks one function name in a command. As the first word it
// is a call with the rest as arguments; anywhere else it would be a call with
// none, which for the data functions is always a mistake.
func (v *pageVet) vetIdentifier(file, name string, c *parse.CommandNode, pos int) error {
	switch name {
	case "call":
		return invalid("html/%s: the builtin %q is not available in a page template", file, name)
	case pagefuncs.Query, pagefuncs.Param, pagefuncs.Link:
	default:
		return nil
	}
	if pos != 0 {
		return invalid("html/%s: %s must be called with its arguments, not passed as a value", file, name)
	}
	args := c.Args[1:]
	switch name {
	case pagefuncs.Query:
		return v.literalName(file, name, args, 0, len(args) == 1, "a query", func(n string) bool { return hasQuery(v.m, n) }, v.queries)
	case pagefuncs.Param:
		return v.literalName(file, name, args, 0, len(args) == 1, "a parameter", func(n string) bool { return hasParam(v.m, n) }, v.params)
	}
	if len(args)%2 != 0 {
		return invalid("html/%s: link takes name and value pairs written out in the call (link \"region\" .region), not a piped value", file)
	}
	for i := 0; i < len(args); i += 2 {
		if err := v.literalName(file, name, args, i, true, "a parameter", func(n string) bool { return hasParam(v.m, n) }, v.params); err != nil {
			return err
		}
	}
	return nil
}

func (v *pageVet) literalName(file, fn string, args []parse.Node, i int, shapeOK bool, what string, exists func(string) bool, used map[string]bool) error {
	if !shapeOK {
		return invalid("html/%s: %s takes exactly one literal name (a quoted string), for example %s \"name\"", file, fn, fn)
	}
	s, ok := args[i].(*parse.StringNode)
	if !ok {
		return invalid("html/%s: the name passed to %s must be a quoted literal, not a computed value", file, fn)
	}
	if !exists(s.Text) {
		return invalid("html/%s: %s %q is not %s the manifest declares", file, fn, s.Text, what)
	}
	used[s.Text] = true
	return nil
}

func hasQuery(m *Manifest, name string) bool {
	for _, q := range m.Queries {
		if q.Name == name {
			return true
		}
	}
	return false
}

func hasParam(m *Manifest, name string) bool {
	for _, p := range m.Parameters {
		if p.Name == name {
			return true
		}
	}
	return false
}

// sealPage sets info.Hash from the stored manifest bytes and the file set.
func sealPage(manifest []byte, files []PageFile, info *PageInfo) error {
	h, err := PageHash(manifest, files, info)
	if err != nil {
		return err
	}
	info.Hash = h
	return nil
}

// pageProblemsError is several problems found in one pass over a page.
type pageProblemsError struct{ list []string }

func (e *pageProblemsError) Error() string {
	return ErrInvalid.Error() + ": " + strings.Join(e.list, "\n")
}
func (e *pageProblemsError) Unwrap() error { return ErrInvalid }

// pageProblems joins the lint findings and the first markup error (which may
// be nil) into one error.
func pageProblems(lint []string, markup error) error {
	list := append([]string(nil), lint...)
	if markup != nil {
		list = append(list, strings.TrimPrefix(markup.Error(), ErrInvalid.Error()+": "))
	}
	return &pageProblemsError{list: list}
}

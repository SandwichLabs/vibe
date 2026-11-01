package artifacts

// The renderer of a view: html artifact (docs/plans/html-artifacts.md, H2 and
// P3): the entry template and its partials, executed against the closed
// function map with an Env that runs the manifest's named queries lazily and
// once per render. The page's queries all go through the PageRunner the host
// gives (Studio's is RunArtifactQuery: vetted SQL on every run, Bind, the row
// cap, the read-only gate), so the renderer itself never sees SQL, a
// connection or a path.
//
// The renderer bounds itself. A context cannot interrupt a template that
// loops without calling anything ({{ range 3000000000 }}{{ end }}), and
// html/template has no hook, so render_guard.go rewrites the parsed trees
// before they run: every range is charged what it will iterate and every
// {{template}} call one step, against one budget for the whole render.
// Recursion between templates is refused outright, so the call depth is at
// most the number of templates. Nothing runs in a goroutine, so nothing can
// outlive a render.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"path"
	"time"

	"github.com/SandwichLabs/slaw/internal/artifacts/pagefuncs"
)

// Render limits.
const (
	// MaxRenderBytes caps one page's output.
	MaxRenderBytes = 10 << 20
	// RenderTimeout bounds a whole render, queries included.
	RenderTimeout = 30 * time.Second
	// renderBudget is the iterations plus template calls one render may spend:
	// room for 20 queries of 500 rows with a hundred cells each, with no room
	// for a loop that does nothing for a long time.
	renderBudget = 2_000_000
)

// PageRows is what a PageRunner returns for one named query.
type PageRows struct {
	Columns   []string
	Rows      [][]any
	Truncated bool
}

// PageRunner runs one of the page artifact's named queries with the page's
// parameter values. The artifact is the runner's (the host pins it), never the
// template's. Its error is never shown: the page gets a fixed text.
type PageRunner func(ctx context.Context, query string, values map[string]any) (*PageRows, error)

// RenderKind classifies why a render failed, for the host's status and fixed
// error block.
type RenderKind string

// The ways a render fails.
const (
	// RenderBadValues: a parameter value was not valid.
	RenderBadValues RenderKind = "values"
	// RenderQuery: one of the page's queries could not run.
	RenderQuery RenderKind = "query"
	// RenderTemplate: the templates could not be parsed or executed.
	RenderTemplate RenderKind = "template"
	// RenderTooLarge: the output went over MaxRenderBytes.
	RenderTooLarge RenderKind = "too-large"
	// RenderTooSlow: the render ran out of time or work.
	RenderTooSlow RenderKind = "too-slow"
	// RenderCanceled: the caller stopped the render.
	RenderCanceled RenderKind = "canceled"
)

// RenderError is a failed render. Its text is fixed (Query is the manifest's
// own query name); the underlying cause is reachable through Unwrap for a
// debug log and must never be shown.
type RenderError struct {
	Kind  RenderKind
	Query string
	cause error
}

func (e *RenderError) Error() string {
	switch e.Kind {
	case RenderBadValues:
		return "a parameter value is not valid"
	case RenderQuery:
		return fmt.Sprintf("the query %q could not run", e.Query)
	case RenderTooLarge:
		return "the page is too large"
	case RenderTooSlow:
		return "the page took too long to draw"
	case RenderCanceled:
		return "the page was stopped"
	}
	return "the page could not be drawn"
}

// Unwrap returns the cause, for logs only.
func (e *RenderError) Unwrap() error { return e.cause }

var (
	errPageTooLarge = errors.New("page output over the cap")
	errPageBudget   = errors.New("page work budget spent")
	errPageQuery    = errors.New("a page query failed")
)

// RenderHTMLPage renders a (a view: html artifact exactly as loaded, files and
// all) with the parameter values (typed as ParsePageQuery returns them; Bind
// checks them again on every query) and writes the HTML to w only if the whole
// render succeeded and passed the output check: a failed render writes
// nothing. The caller owns trust: the artifact must be the one that was
// allowed, and must be a static page.
func RenderHTMLPage(ctx context.Context, run PageRunner, a *Artifact, values map[string]any, w io.Writer) error {
	if a == nil || a.Manifest.View != ViewHTML || a.Manifest.HTML == nil || run == nil {
		return &RenderError{Kind: RenderTemplate, cause: errors.New("not a page")}
	}
	byName := make(map[string]*Param, len(a.Manifest.Parameters))
	for i := range a.Manifest.Parameters {
		byName[a.Manifest.Parameters[i].Name] = &a.Manifest.Parameters[i]
	}
	supplied, err := coerceSupplied(byName, values)
	if err != nil {
		return &RenderError{Kind: RenderBadValues, cause: err}
	}
	defaults, err := Defaults(&a.Manifest, time.Now())
	if err != nil {
		return &RenderError{Kind: RenderTemplate, cause: err}
	}
	ctx, cancel := context.WithTimeout(ctx, RenderTimeout)
	defer cancel()

	env := &pageEnv{ctx: ctx, run: run, m: &a.Manifest, supplied: supplied, defaults: defaults, memo: map[string]*memoQuery{}}
	b := &workBudget{ctx: ctx, left: renderBudget, strLeft: maxPageStrings}
	root, err := parsePage(a.Files, pagefuncs.FuncsMetered(env, b))
	if err != nil {
		return &RenderError{Kind: RenderTemplate, cause: err}
	}
	if err := instrument(root, b); err != nil {
		return &RenderError{Kind: RenderTemplate, cause: err}
	}
	out := &cappedWriter{ctx: ctx, max: MaxRenderBytes}
	if err := root.ExecuteTemplate(out, a.Manifest.HTML.Entry, nil); err != nil {
		return renderFailure(ctx, env, err)
	}
	if err := checkRendered(out.buf.Bytes(), a.Page != nil && a.Page.Kind == PageScripted, pagePaths(a.Files)); err != nil {
		return &RenderError{Kind: RenderTemplate, cause: err}
	}
	if _, err := w.Write(out.buf.Bytes()); err != nil {
		return fmt.Errorf("failed to write the page: %w", err)
	}
	return nil
}

// renderFailure classifies an execution error. The sentinels travel through
// html/template's wrapping (errors.Is).
func renderFailure(ctx context.Context, env *pageEnv, err error) error {
	switch {
	case env.failed != "":
		return &RenderError{Kind: RenderQuery, Query: env.failed, cause: err}
	case errors.Is(err, errPageTooLarge), errors.Is(err, pagefuncs.ErrStringBudget):
		return &RenderError{Kind: RenderTooLarge, cause: err}
	case errors.Is(err, errPageBudget), errors.Is(err, context.DeadlineExceeded):
		return &RenderError{Kind: RenderTooSlow, cause: err}
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &RenderError{Kind: RenderTooSlow, cause: err}
		}
		return &RenderError{Kind: RenderCanceled, cause: err}
	}
	return &RenderError{Kind: RenderTemplate, cause: err}
}

// parsePage parses every .tmpl under html/ into one template set, with the
// closed function map (the guard functions are added after parsing, so author
// text cannot name them).
func parsePage(files []PageFile, funcs template.FuncMap) (*template.Template, error) {
	root := template.New("page").Option("missingkey=error").Funcs(funcs)
	for _, f := range files {
		if path.Ext(f.Path) != ".tmpl" || path.Dir(f.Path) != "." {
			continue
		}
		if _, err := root.New(f.Path).Parse(string(f.Data)); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", f.Path, err)
		}
	}
	return root, nil
}

// cappedWriter collects output up to a cap, and stops a render whose context
// ended (a loop that writes is interrupted at its next write).
type cappedWriter struct {
	ctx context.Context
	max int
	buf bytes.Buffer
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if c.buf.Len()+len(p) > c.max {
		return 0, errPageTooLarge
	}
	return c.buf.Write(p)
}

// PageErrorHTML is the fixed document a host shows when a render failed: a
// short heading and one fixed sentence by kind, no template text, no row, no
// policy reason. It needs no asset and no script.
func PageErrorHTML(err error) []byte {
	title, line := "This page could not be drawn.", "Its template has an error. Open the artifact's files to fix it."
	var re *RenderError
	if errors.As(err, &re) {
		switch re.Kind {
		case RenderBadValues:
			line = "A parameter value is not valid. Reset the controls above."
		case RenderQuery:
			line = fmt.Sprintf("The query %q could not run.", re.Query)
		case RenderTooLarge:
			line = "The page is over the 10 MB limit."
		case RenderTooSlow:
			line = "The page took too long to draw."
		case RenderCanceled:
			line = "The page was stopped."
		}
	}
	var b bytes.Buffer
	_ = errorPage.Execute(&b, struct{ Title, Line string }{title, line})
	return b.Bytes()
}

var errorPage = template.Must(template.New("err").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Page error</title>` +
	`<style>html{color-scheme:light dark}body{font:14px system-ui,sans-serif;margin:16px;color:CanvasText;background:Canvas}h1{font-size:16px;margin:0 0 8px}</style></head>` +
	`<body><h1>{{ .Title }}</h1><p>{{ .Line }}</p></body></html>`))

// pagePaths is the set of a page's file paths.
func pagePaths(files []PageFile) map[string]bool {
	m := make(map[string]bool, len(files))
	for _, f := range files {
		m[f.Path] = true
	}
	return m
}

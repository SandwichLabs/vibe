package agents

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

// --- HTML pages through write_artifact / get_artifact (H7) -------------------
//
// A page is content: the agent can write its files and read them back, and
// that is all. Nothing here (and no tool) renders a page, runs one of its
// queries, or allows it; allow, deny and revoke are the person's, in Studio or
// the CLI. Every write changes the page's hash, so an edit by the agent always
// asks the person again.

// Budgets for get_artifact's view of a page. A text file over
// artifactPageFileBudgetBytes, or one that would take the total over
// artifactPageTotalBudgetBytes, comes back without its text (never as a
// prefix, so a partial file cannot exist for the agent to write back);
// write_artifact's {"keep": true} keeps the stored text.
const (
	artifactPageFileBudgetBytes  = artifactViewBudgetBytes
	artifactPageTotalBudgetBytes = 4 * artifactViewBudgetBytes
)

// pageNote is what the agent is told after it writes a page.
const pageNote = "the page is stored but nothing renders it until the person allows it in Studio (or the CLI); it asks again after every edit, and no tool can allow it or run it"

// pageFilesForAgent describes a page for get_artifact: its derived kind,
// entry, the manifest queries and parameters its templates use, the declared
// capabilities, and each file (text whole, or omitted; a binary asset only by
// size).
func pageFilesForAgent(a *artifacts.Artifact) map[string]any {
	out := map[string]any{}
	files := make([]map[string]any, 0, len(a.Files))
	total := 0
	for _, f := range a.Files {
		row := map[string]any{"path": f.Path, "role": artifacts.PageFileRole(f.Path, pageEntry(a)), "size": len(f.Data)}
		switch {
		case !artifacts.IsTextPageFile(f.Path) || !utf8.Valid(f.Data):
			row["binary"] = true
		case len(f.Data) > artifactPageFileBudgetBytes || total+len(f.Data) > artifactPageTotalBudgetBytes:
			row["text"] = nil
			row["text_omitted"] = true
			row["text_length"] = utf8.RuneCount(f.Data)
		default:
			total += len(f.Data)
			row["text"] = string(f.Data)
		}
		files = append(files, row)
	}
	out["files"] = files
	if a.Page != nil {
		out["kind"], out["entry"] = a.Page.Kind, a.Page.Entry
		out["queries_used"], out["parameters_used"], out["capabilities"] = nonNil(a.Page.Queries), nonNil(a.Page.Params), nonNil(a.Page.Capabilities)
	}
	return out
}

func pageEntry(a *artifacts.Artifact) string {
	if a.Page != nil {
		return a.Page.Entry
	}
	return artifacts.DefaultEntry
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// pageStoredLoader reads the stored page once, only when a keep or a carried
// asset needs it, and only with the rev of the artifact the agent read.
type pageStoredLoader struct {
	ctx    context.Context
	store  artifacts.Store
	name   string
	rev    string
	stored *artifacts.Artifact
}

func (l *pageStoredLoader) load() (*artifacts.Artifact, error) {
	if l.stored != nil {
		return l.stored, nil
	}
	if l.rev == "" {
		return nil, errors.New("keeping a file or a stored asset needs the rev of the artifact you read (get_artifact)")
	}
	cur, err := l.store.Get(l.ctx, l.name)
	if err != nil || cur.Manifest.View != artifacts.ViewHTML {
		return nil, fmt.Errorf("the stored artifact %q must exist with view: html", l.name)
	}
	l.stored = cur
	return cur, nil
}

// pageFilesFromParams builds the file set a write stores from html_files
// (path -> text, or path -> {"keep": true} for a file get_artifact returned
// without its text). Binary assets already stored are carried over untouched:
// the agent can neither read nor write them. A write that sends a view,
// view_source or keep_view_source is refused: they belong to the other kinds.
func (h *Handler) pageFilesFromParams(ctx context.Context, store artifacts.Store, name, rev string, params map[string]any) ([]artifacts.PageFile, error) {
	for _, k := range []string{"view", "view_source", "keep_view_source"} {
		if _, has := params[k]; has {
			return nil, fmt.Errorf("a manifest with view: html takes html_files, not %s", k)
		}
	}
	raw, ok := params["html_files"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil, errors.New("html_files must be an object of {path: text} (path relative to html/, such as \"index.html.tmpl\" or \"assets/style.css\"), with at least the entry template")
	}
	if len(raw) > artifacts.MaxPageFiles {
		return nil, fmt.Errorf("html_files has more than %d files", artifacts.MaxPageFiles)
	}
	loader := &pageStoredLoader{ctx: ctx, store: store, name: name, rev: rev}
	paths := make([]string, 0, len(raw))
	for p := range raw {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]artifacts.PageFile, 0, len(raw))
	for _, p := range paths {
		f, err := pageFileFromValue(loader, p, raw[p])
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return carryBinaryAssets(loader, out), nil
}

// pageFileFromValue is one html_files entry: a text, or {"keep": true}.
func pageFileFromValue(loader *pageStoredLoader, p string, v any) (artifacts.PageFile, error) {
	if err := artifacts.CheckPageFilePath(p); err != nil {
		return artifacts.PageFile{}, err
	}
	if !artifacts.IsTextPageFile(p) {
		return artifacts.PageFile{}, fmt.Errorf("html/%s is a binary asset: the agent writes text files only (binary assets arrive by git or a bundle)", p)
	}
	switch x := v.(type) {
	case string:
		if len(x) > artifacts.MaxPageFileBytes {
			return artifacts.PageFile{}, fmt.Errorf("html/%s is over the %d byte limit", p, artifacts.MaxPageFileBytes)
		}
		return artifacts.PageFile{Path: p, Data: []byte(x)}, nil
	case map[string]any:
		if keep, _ := x["keep"].(bool); !keep || len(x) != 1 {
			break
		}
		cur, err := loader.load()
		if err != nil {
			return artifacts.PageFile{}, err
		}
		f, ok := findPageFile(cur.Files, p)
		if !ok {
			return artifacts.PageFile{}, fmt.Errorf("keep: the stored page has no file html/%s", p)
		}
		return f, nil
	}
	return artifacts.PageFile{}, fmt.Errorf("html_files[%q] must be the file's text or {\"keep\": true}", p)
}

// carryBinaryAssets adds the stored binary (or non-UTF-8) files the write did
// not name, when it carries the rev of a stored page. Without a rev, or when
// the stored artifact is not a page, there is nothing to carry (the store's
// compare-and-write decides about the rev).
func carryBinaryAssets(loader *pageStoredLoader, out []artifacts.PageFile) []artifacts.PageFile {
	if loader.rev == "" {
		return out
	}
	cur, err := loader.load()
	if err != nil {
		return out
	}
	sent := map[string]bool{}
	for _, f := range out {
		sent[f.Path] = true
	}
	for _, f := range cur.Files {
		if (!artifacts.IsTextPageFile(f.Path) || !utf8.Valid(f.Data)) && !sent[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

func findPageFile(files []artifacts.PageFile, p string) (artifacts.PageFile, bool) {
	for _, f := range files {
		if f.Path == p {
			return f, true
		}
	}
	return artifacts.PageFile{}, false
}

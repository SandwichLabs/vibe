package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// maxBindingStatements is the most statements an exported *App method (a Wails
// binding) may have, counted recursively: every statement at any depth,
// closures included, so nesting the logic one level down (an if, a for, a
// func literal) does not hide it. A binding validates nothing and decides
// nothing: it takes the lock, calls internal/, shapes the result and emits
// events. Anything longer is logic that belongs in internal/ (#54, P1).
const maxBindingStatements = 25

// forbiddenPackages lists what an exported binding may not touch itself: file
// and process I/O and raw database/sql. Reaching the disk or a database goes
// through an internal/ call, so the same rule serves the CLI. Keys are import
// paths; a nil set forbids every use of the package, a non-nil set only those
// functions (path/filepath is mostly pure string work).
var forbiddenPackages = map[string]map[string]bool{
	"os":            nil,
	"io/ioutil":     nil,
	"io/fs":         nil,
	"os/exec":       nil,
	"database/sql":  nil,
	"path/filepath": {"Walk": true, "WalkDir": true, "Glob": true, "EvalSymlinks": true, "Abs": true},
}

// forbiddenFuncPrefixes names the I/O helpers of packages that are otherwise
// fine to call: a binding may use filestore.Slug, but not filestore.Write*,
// which are file writes in a wrapper.
var forbiddenFuncPrefixes = map[string][]string{
	"github.com/SandwichLabs/vibe/internal/filestore": {"Write", "Remove", "Sweep", "Read", "List", "Resolve", "StatStamp"},
}

// forbiddenDBMethods are the *sql.DB / *sql.Tx entry points: a binding that
// calls one is running SQL itself. (Session methods such as sess.Exec are not
// listed: they are internal/ calls.) Anything that needs one of these goes
// through an internal/ function that takes the db.
var forbiddenDBMethods = map[string]bool{
	"Query": true, "QueryContext": true, "QueryRow": true, "QueryRowContext": true,
	"Exec": true, "ExecContext": true, "Prepare": true, "PrepareContext": true, "Begin": true, "BeginTx": true,
}

// bindingWaivers are exported *App methods allowed to break a rule, with the
// reason. Keep it empty if you can: the point of the test is that it stays so.
var bindingWaivers = map[string]string{}

// TestExportedAppMethodsAreThinBindings is the blocking guard for the thin
// binding rule: every exported method of *App in a non-test studio file has at
// most maxBindingStatements statements (nested ones counted) and does no file,
// process or raw SQL work itself, and no studio file dot-imports a package or
// aliases an I/O function in a package-level var. Private helpers may do adapter plumbing (events,
// streaming, shaping), but the rules they apply must live in internal/.
func TestExportedAppMethodsAreThinBindings(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	var problems []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		require.NoError(t, err)
		imports := importNames(af)
		problems = append(problems, fileProblems(f, af, imports)...)
		for _, d := range af.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && isBinding(fd) {
				problems = append(problems, bindingProblems(f, fd, imports)...)
			}
		}
	}
	sort.Strings(problems)
	require.Empty(t, problems, "exported App methods must be thin bindings")
}

// importNames maps each import's local name to its path.
func importNames(af *ast.File) map[string]string {
	imports := map[string]string{}
	for _, im := range af.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := filepath.Base(p)
		if im.Name != nil {
			name = im.Name.Name
		}
		imports[name] = p
	}
	return imports
}

// isBinding reports whether fd is an exported method of *App that is not waived.
func isBinding(fd *ast.FuncDecl) bool {
	if fd.Recv == nil || fd.Body == nil || !fd.Name.IsExported() {
		return false
	}
	if _, waived := bindingWaivers[fd.Name.Name]; waived {
		return false
	}
	e := fd.Recv.List[0].Type
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "App"
}

// bindingProblems lists what breaks the thin-binding rule in fd.
func bindingProblems(file string, fd *ast.FuncDecl, imports map[string]string) []string {
	var out []string
	name := fd.Name.Name
	if n := countStatements(fd.Body); n > maxBindingStatements {
		out = append(out, fmt.Sprintf("%s: App.%s has %d statements counting nested ones (max %d)", file, name, n, maxBindingStatements))
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if m, ok := call.Fun.(*ast.SelectorExpr); ok && forbiddenDBMethods[m.Sel.Name] {
				out = append(out, fmt.Sprintf("%s: App.%s calls .%s (SQL belongs behind an internal/ call)", file, name, m.Sel.Name))
			}
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if what := forbiddenUse(sel, imports); what != "" {
			out = append(out, fmt.Sprintf("%s: App.%s uses %s (file, process or SQL work belongs in internal/)", file, name, what))
		}
		return true
	})
	return out
}

// countStatements counts every statement under body at any depth, closures
// included. Blocks, empty statements and case clauses are structure, not
// statements.
func countStatements(body *ast.BlockStmt) int {
	n := 0
	ast.Inspect(body, func(x ast.Node) bool {
		if s, ok := x.(ast.Stmt); ok {
			switch s.(type) {
			case *ast.BlockStmt, *ast.EmptyStmt, *ast.CaseClause, *ast.CommClause:
			default:
				n++
			}
		}
		return true
	})
	return n
}

// forbiddenUse names pkg.Func when sel is a use of a forbidden package, else "".
func forbiddenUse(sel *ast.SelectorExpr, imports map[string]string) string {
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Obj != nil { // a local variable that shadows the package name
		return ""
	}
	path := imports[id.Name]
	for _, prefix := range forbiddenFuncPrefixes[path] {
		if strings.HasPrefix(sel.Sel.Name, prefix) {
			return id.Name + "." + sel.Sel.Name
		}
	}
	funcs, forbidden := forbiddenPackages[path]
	if !forbidden || (funcs != nil && !funcs[sel.Sel.Name]) {
		return ""
	}
	return id.Name + "." + sel.Sel.Name
}

// fileProblems closes the ways to dodge the per-method check from outside the
// method: a dot import (os.ReadFile becomes a bare ReadFile that no selector
// check sees) and a package-level alias of an I/O function (var readIt =
// os.ReadFile, then App.X calls readIt).
func fileProblems(file string, af *ast.File, imports map[string]string) []string {
	var out []string
	for _, im := range af.Imports {
		if im.Name != nil && im.Name.Name == "." {
			out = append(out, fmt.Sprintf("%s: dot import of %s (import it by name so I/O stays visible)", file, im.Path.Value))
		}
	}
	for _, d := range af.Decls {
		out = append(out, declAliasProblems(file, d, imports)...)
	}
	return out
}

// declAliasProblems reports I/O functions aliased by one package-level
// declaration: a var, or the body of init().
func declAliasProblems(file string, d ast.Decl, imports map[string]string) []string {
	var root ast.Node
	switch d := d.(type) {
	case *ast.GenDecl:
		if d.Tok == token.VAR {
			root = d
		}
	case *ast.FuncDecl:
		// init() can assign an I/O function to a package-level func var.
		if d.Recv == nil && d.Name.Name == "init" && d.Body != nil {
			root = d.Body
		}
	}
	if root == nil {
		return nil
	}
	var out []string
	_, inInit := root.(*ast.BlockStmt)
	ast.Inspect(root, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && inInit {
			// In init() only an assignment of the function value itself
			// (f = os.ReadFile) is an alias; calling os.Setenv there is fine.
			for _, rhs := range as.Rhs {
				if sel, ok := rhs.(*ast.SelectorExpr); ok {
					if what := forbiddenUse(sel, imports); what != "" {
						out = append(out, fmt.Sprintf("%s: init() aliases %s (an alias of an I/O function dodges the binding check)", file, what))
					}
				}
			}
			return true
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && !inInit {
			if what := forbiddenUse(sel, imports); what != "" {
				out = append(out, fmt.Sprintf("%s: package-level var or init() uses %s (an alias of an I/O function dodges the binding check)", file, what))
			}
		}
		return true
	})
	return out
}

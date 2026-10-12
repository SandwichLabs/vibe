package mcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMCPToolsComeFromTheAgentsRegistry is the C3 guard for the MCP side
// ("one tool surface", CLAUDE.md). In internal/mcp and internal/hub:
//   - no mcp.Tool composite literal;
//   - every mcp.NewTool (anywhere in a file, package-level vars included) is
//     named from an agents.ToolDef parameter of its function;
//   - every AddTool/AddTools/SetTools call sits inside a range over
//     agents.ListTools*, and each argument is derived from the ranged value.
//
// The fantasy half is the ast-grep rule go-fantasy-tool-outside-agents.
func TestMCPToolsComeFromTheAgentsRegistry(t *testing.T) {
	registrations := 0
	for _, dir := range []string{".", "../hub"} {
		fset := token.NewFileSet()
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err)
			for _, decl := range f.Decls {
				fn, _ := decl.(*ast.FuncDecl)
				registrations += checkToolDecl(t, fset, decl, fn)
			}
		}
	}
	require.NotZero(t, registrations, "found no MCP tool registration: the guard is looking in the wrong place")
}

func checkToolDecl(t *testing.T, fset *token.FileSet, decl ast.Decl, fn *ast.FuncDecl) int {
	t.Helper()
	defParams := toolDefParams(fn)
	seen := 0
	ast.Inspect(decl, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if sel, ok := x.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Tool" && isIdent(sel.X, "mcp") {
				require.Fail(t, "mcp.Tool literal", "%s: build MCP tools from an agents.ToolDef with mcp.NewTool(td.Name, ...)", fset.Position(x.Pos()))
			}
		case *ast.RangeStmt:
			if fn != nil && rangesOverRegistry(x) {
				seen += checkRegistryLoop(t, fset, x)
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pos := fset.Position(x.Pos())
			if isIdent(sel.X, "mcp") && sel.Sel.Name == "NewTool" {
				seen++
				require.NotEmpty(t, x.Args, "%s: mcp.NewTool without a name", pos)
				name, ok := x.Args[0].(*ast.SelectorExpr)
				require.True(t, ok && name.Sel.Name == "Name" && defParams[identName(name.X)],
					"%s: mcp.NewTool must be named from an agents.ToolDef parameter (td.Name), not a literal", pos)
			}
		}
		return true
	})
	checkNoLooseRegistration(t, fset, decl)
	return seen
}

// toolDefParams names fn's parameters typed agents.ToolDef.
func toolDefParams(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	if fn == nil || fn.Type.Params == nil {
		return out
	}
	for _, p := range fn.Type.Params.List {
		if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "ToolDef" && isIdent(sel.X, "agents") {
			for _, n := range p.Names {
				out[n.Name] = true
			}
		}
	}
	return out
}

// checkNoLooseRegistration fails on a registration call outside a registry loop.
func checkNoLooseRegistration(t *testing.T, fset *token.FileSet, decl ast.Decl) {
	t.Helper()
	ast.Inspect(decl, func(n ast.Node) bool {
		if rs, ok := n.(*ast.RangeStmt); ok && rangesOverRegistry(rs) {
			return false // checked by checkRegistryLoop
		}
		if call, ok := n.(*ast.CallExpr); ok && isRegisterCall(call) {
			name := "registration"
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				name = sel.Sel.Name
			}
			require.Fail(t, "tool registration outside the registry", "%s: %s must be inside a range over agents.ListTools*", fset.Position(call.Pos()), name)
		}
		return true
	})
}

func isRegisterCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && (sel.Sel.Name == "AddTool" || sel.Sel.Name == "AddTools" || sel.Sel.Name == "SetTools")
}

func rangesOverRegistry(rs *ast.RangeStmt) bool {
	call, ok := rs.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && isIdent(sel.X, "agents") && strings.HasPrefix(sel.Sel.Name, "ListTools")
}

// checkRegistryLoop requires every registration argument inside the loop to be
// derived from the ranged value (directly, or via a variable assigned from it).
func checkRegistryLoop(t *testing.T, fset *token.FileSet, rs *ast.RangeStmt) int {
	t.Helper()
	derived := map[string]bool{identName(rs.Value): true}
	mentions := func(e ast.Node) bool {
		found := false
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && derived[id.Name] {
				found = true
			}
			return !found
		})
		return found
	}
	n := 0
	ast.Inspect(rs.Body, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.AssignStmt:
			for _, rhs := range x.Rhs {
				if mentions(rhs) {
					for _, l := range x.Lhs {
						derived[identName(l)] = true
					}
				}
			}
		case *ast.CallExpr:
			if isRegisterCall(x) {
				n++
				for _, a := range x.Args {
					require.True(t, mentions(a), "%s: argument of a tool registration is not derived from the ranged agents.ToolDef", fset.Position(a.Pos()))
				}
			}
		}
		return true
	})
	return n
}

func isIdent(e ast.Expr, name string) bool { return identName(e) == name }

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

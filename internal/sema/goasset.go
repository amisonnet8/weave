package sema

import (
	"fmt"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// weave_spec.md §15's Go asset declarations (`gotype`/`gofunc`, §16's
// `gomethod`) are evaluated entirely at compile time — the declared
// names (`GoFile`, `goOpen`) are never ordinary dynamic Weave values,
// so unlike every other assignment `X = gotype(...)` and `Y =
// gofunc(...)` generate no VAR/SET/CALL at all (see
// internal/codegen/goasset.go). checkStmt's *ast.AssignStmt case
// recognizes this shape (RHS is a call to the reserved names `gotype`/
// `gofunc`) before falling through to the ordinary dynamic-value path.
//
// gotype/gofunc/gomethod are reserved words (weave_spec.md §13) but —
// like every other builtin name — lex as plain Ident and are resolved
// here, not by the lexer (lexer.go's doc comment on structural vs.
// builtin keywords). Unlike ordinary builtins (print, has, ...) they
// are NOT added to builtinNames: they may only appear in the exact
// declaration shapes checkGoTypeDecl/checkGoFuncDecl recognize, never
// as an ordinary call — checkExpr's default case rejects any other use
// with a clear error instead of the confusing "undefined name" a plain
// reserved-word check would give.

// GoTypeInfo is what `X = gotype("?pkg.Type", { weaveName:
// gomethod("GoName"), ... })` declares. GoName is the "?pkg.Type"
// string exactly as written — already shaped like an AMIVM callname
// token, and one `?`→`^` swap away from a type token (see
// internal/codegen/goasset.go, which is what actually emits IR).
// Methods maps each Weave-visible member name to the real Go method
// name gomethod names.
type GoTypeInfo struct {
	GoName  string
	Methods map[string]string
}

// GoFuncInfo is what `Y = gofunc("?pkg.Func", protoRef)` declares.
// GoName is the "?pkg.Func" string exactly as written — already the
// exact AMIVM callname shape a call to Y compiles straight to
// (weave_spec.md §16: no dynamic dispatch at all). Proto names the
// GoTypeInfo (by its own declared Weave name) a struct/pointer result
// should carry, or "" if the function returns a plain scalar needing
// no method table.
type GoFuncInfo struct {
	GoName string
	Proto  string
}

func goAssetReservedName(name string) (string, bool) {
	switch name {
	case "gotype":
		return "reserved for Go type declarations (weave_spec.md §15.1)", true
	case "gofunc":
		return "reserved for Go function declarations (weave_spec.md §15.2)", true
	case "gomethod":
		return "reserved for use inside a gotype(...) member list (weave_spec.md §15.1)", true
	}
	return "", false
}

// checkGoDecl recognizes `name = gotype(...)` / `name = gofunc(...)`
// and handles them as compile-time declarations, returning (true, err)
// if it did. checkStmt's *ast.AssignStmt case calls this before its
// ordinary dynamic-value handling.
func (c *checker) checkGoDecl(name string, call *ast.CallExpr, line int) (bool, error) {
	callee, ok := call.Callee.(*ast.Ident)
	if !ok {
		return false, nil
	}
	switch callee.Name {
	case "gotype":
		return true, c.checkGoTypeDecl(name, call, line)
	case "gofunc":
		return true, c.checkGoFuncDecl(name, call, line)
	default:
		return false, nil
	}
}

// checkGoTypeDecl checks `X = gotype("?pkg.Type", { weaveName:
// gomethod("GoName"), ... })` (weave_spec.md §15.1).
func (c *checker) checkGoTypeDecl(name string, call *ast.CallExpr, line int) error {
	if why, ok := reservedName(name); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", line, name, why)
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("line %d: gotype(...) takes exactly two arguments (a Go type name, then a member table), got %d", call.Line, len(call.Args))
	}
	goName, ok := call.Args[0].(*ast.StringLit)
	if !ok {
		return fmt.Errorf("line %d: gotype(...)'s first argument must be a string literal like \"?pkg.Type\"", call.Line)
	}
	if !strings.HasPrefix(goName.Value, "?") {
		return fmt.Errorf("line %d: gotype(...)'s Go type name must start with '?' (weave_spec.md §15.1's own \"?os.File\" shape), got %q", call.Line, goName.Value)
	}
	members, ok := call.Args[1].(*ast.ObjectLit)
	if !ok {
		return fmt.Errorf("line %d: gotype(...)'s second argument must be an object literal of gomethod(...) entries", call.Line)
	}

	methods := map[string]string{}
	for _, field := range members.Fields {
		mcall, ok := field.Value.(*ast.CallExpr)
		if !ok {
			return fmt.Errorf("line %d: gotype(...) member %q must be gomethod(\"GoMethodName\")", call.Line, field.Name)
		}
		mcallee, ok := mcall.Callee.(*ast.Ident)
		if !ok || mcallee.Name != "gomethod" {
			return fmt.Errorf("line %d: gotype(...) member %q must be gomethod(\"GoMethodName\")", call.Line, field.Name)
		}
		if len(mcall.Args) != 1 {
			return fmt.Errorf("line %d: gomethod(...) takes exactly one argument, got %d", mcall.Line, len(mcall.Args))
		}
		goMethodName, ok := mcall.Args[0].(*ast.StringLit)
		if !ok {
			return fmt.Errorf("line %d: gomethod(...)'s argument must be a string literal", mcall.Line)
		}
		methods[field.Name] = goMethodName.Value
	}

	if c.goTypes == nil {
		c.goTypes = map[string]*GoTypeInfo{}
	}
	c.goTypes[name] = &GoTypeInfo{GoName: goName.Value, Methods: methods}
	return nil
}

// checkGoFuncDecl checks `Y = gofunc("?pkg.Func", protoRef)`
// (weave_spec.md §15.2). protoRef is either `nil` (the function returns
// a plain scalar) or an identifier naming a gotype declared earlier.
func (c *checker) checkGoFuncDecl(name string, call *ast.CallExpr, line int) error {
	if why, ok := reservedName(name); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", line, name, why)
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("line %d: gofunc(...) takes exactly two arguments (a Go function name, then a return-value prototype), got %d", call.Line, len(call.Args))
	}
	goName, ok := call.Args[0].(*ast.StringLit)
	if !ok {
		return fmt.Errorf("line %d: gofunc(...)'s first argument must be a string literal like \"?pkg.Func\"", call.Line)
	}
	if !strings.HasPrefix(goName.Value, "?") {
		return fmt.Errorf("line %d: gofunc(...)'s Go function name must start with '?' (weave_spec.md §15.2's own \"?os.Open\" shape), got %q", call.Line, goName.Value)
	}

	proto := ""
	switch p := call.Args[1].(type) {
	case *ast.NilLit:
		// scalar return, no method table
	case *ast.Ident:
		if c.goTypes[p.Name] == nil {
			return fmt.Errorf("line %d: %q is not a gotype(...) declared earlier", p.Line, p.Name)
		}
		proto = p.Name
	default:
		return fmt.Errorf("line %d: gofunc(...)'s second argument must be nil or a gotype(...) name", call.Line)
	}

	if c.goFuncs == nil {
		c.goFuncs = map[string]*GoFuncInfo{}
	}
	c.goFuncs[name] = &GoFuncInfo{GoName: goName.Value, Proto: proto}
	return nil
}

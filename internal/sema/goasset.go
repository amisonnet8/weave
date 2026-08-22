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
// string exactly as written (optionally "?*pkg.Type" for a pointer
// receiver, weave_spec.md §15.1) — already shaped like an AMIVM
// callname token, and one `?`→`^` swap away from a type token (see
// internal/codegen/goasset.go, which is what actually emits IR).
// Methods maps each Weave-visible member name to its GoMethodInfo.
type GoTypeInfo struct {
	GoName  string
	Methods map[string]*GoMethodInfo
}

// GoMethodInfo is one `gomethod("GoName")` (untyped — ReturnType == "",
// dispatched dynamically via weavert.CallGoMethod at every call site,
// weave_spec.md §16) or `gomethod("GoName", "?retType", "?paramType1",
// ...)` (typed — ReturnType != "", dispatched via a fully native
// ASSERT+FNTYPE+FGET+CALL sequence with no reflect at all; see
// internal/codegen/goasset.go's genNativeGoMethodCall). The typed form
// is opt-in per method: declaring a signature is what turns a dynamic
// runtime type mismatch into an immediate, Weave-flavored error instead
// of relying on reflect's own (sometimes confusing) failure modes — the
// safety/maintainability benefit is the point, not raw speed (see
// CLAUDE.md's design-decision note on this feature).
type GoMethodInfo struct {
	GoName     string
	ReturnType string // "" means untyped/reflect-dispatched
	ParamTypes []string
}

// GoFuncInfo is what `Y = gofunc("?pkg.Func", protoRef)` (optionally
// followed by `, "?paramType1", "?paramType2", ...`, weave_spec.md
// §15.2) declares. GoName is the "?pkg.Func" string exactly as written —
// already the exact AMIVM callname shape a call to Y compiles straight
// to (weave_spec.md §16: no dynamic dispatch at all). Proto names the
// GoTypeInfo (by its own declared Weave name) a struct/pointer result
// should carry, or "" if the function returns a plain scalar needing no
// method table. ParamTypes is nil when no parameter types were declared
// (args passed to weavert.CallGoFunc's reflect-based Convert, as
// before); non-nil (possibly empty, for a niladic function) once
// declared, each argument is ASSERTed to its concrete type and the call
// itself becomes a direct native CALL — no reflect for the call step,
// though the return value still passes through weavert.NormalizeGoValue
// (a plain type switch, not reflect) since the return type itself isn't
// separately declared (see the design-decision note referenced above
// for why gofunc doesn't need one, unlike gomethod).
type GoFuncInfo struct {
	GoName     string
	Proto      string
	ParamTypes []string
}

func goAssetReservedName(name string) (string, bool) {
	switch name {
	case "gotype":
		return "reserved for Go type declarations (weave_spec.md §15.1)", true
	case "gofunc":
		return "reserved for Go function declarations (weave_spec.md §15.2)", true
	case "gomethod":
		return "reserved for use inside a gotype(...) member list (weave_spec.md §15.1)", true
	case "shape":
		return "reserved for shape declarations (weave_spec.md §4.3)", true
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

	methods := map[string]*GoMethodInfo{}
	for _, field := range members.Fields {
		mcall, ok := field.Value.(*ast.CallExpr)
		if !ok {
			return fmt.Errorf("line %d: gotype(...) member %q must be gomethod(\"GoMethodName\")", call.Line, field.Name)
		}
		mcallee, ok := mcall.Callee.(*ast.Ident)
		if !ok || mcallee.Name != "gomethod" {
			return fmt.Errorf("line %d: gotype(...) member %q must be gomethod(\"GoMethodName\")", call.Line, field.Name)
		}
		info, err := checkGoMethodArgs(mcall)
		if err != nil {
			return err
		}
		methods[field.Name] = info
	}

	if c.goTypes == nil {
		c.goTypes = map[string]*GoTypeInfo{}
	}
	c.goTypes[name] = &GoTypeInfo{GoName: goName.Value, Methods: methods}
	return nil
}

// checkGoMethodArgs validates `gomethod("GoName")` (untyped — reflect
// dispatch) or `gomethod("GoName", "?retType", "?paramType1", ...)`
// (typed — native ASSERT-based dispatch, weave_spec.md §15.1/§16). See
// GoMethodInfo's own doc comment for what each shape compiles to.
func checkGoMethodArgs(mcall *ast.CallExpr) (*GoMethodInfo, error) {
	if len(mcall.Args) == 0 {
		return nil, fmt.Errorf("line %d: gomethod(...) takes at least one argument, got 0", mcall.Line)
	}
	goMethodName, ok := mcall.Args[0].(*ast.StringLit)
	if !ok {
		return nil, fmt.Errorf("line %d: gomethod(...)'s first argument must be a string literal (the Go method name)", mcall.Line)
	}
	if len(mcall.Args) == 1 {
		return &GoMethodInfo{GoName: goMethodName.Value}, nil
	}
	returnType, err := goTypeArg(mcall.Args[1], mcall.Line, "gomethod(...)'s return type")
	if err != nil {
		return nil, err
	}
	paramTypes, err := goTypeArgs(mcall.Args[2:], mcall.Line, "gomethod(...)'s parameter type")
	if err != nil {
		return nil, err
	}
	return &GoMethodInfo{GoName: goMethodName.Value, ReturnType: returnType, ParamTypes: paramTypes}, nil
}

// goTypeArg validates one `"?pkg.Type"`-shaped string literal argument
// (what, doesn't matter — describes it in error messages). line is used
// in the error message when expr isn't even a StringLit (so there's no
// literal of its own to report a line from).
func goTypeArg(expr ast.Expr, line int, what string) (string, error) {
	lit, ok := expr.(*ast.StringLit)
	if !ok {
		return "", fmt.Errorf("line %d: %s must be a string literal like \"?int\"", line, what)
	}
	if !strings.HasPrefix(lit.Value, "?") {
		return "", fmt.Errorf("line %d: %s must start with '?' (e.g. \"?int\", \"?*strings.Reader\"), got %q", line, what, lit.Value)
	}
	return lit.Value, nil
}

func goTypeArgs(exprs []ast.Expr, line int, what string) ([]string, error) {
	types := make([]string, len(exprs))
	for i, e := range exprs {
		t, err := goTypeArg(e, line, what)
		if err != nil {
			return nil, err
		}
		types[i] = t
	}
	return types, nil
}

// checkGoFuncDecl checks `Y = gofunc("?pkg.Func", protoRef)` or `Y =
// gofunc("?pkg.Func", protoRef, "?paramType1", "?paramType2", ...)`
// (weave_spec.md §15.2). protoRef is either `nil` (the function returns
// a plain scalar) or an identifier naming a gotype declared earlier.
// Declaring parameter types (independent of protoRef) opts every call
// into native ASSERT-based argument checking (see GoFuncInfo's own doc
// comment for why gofunc, unlike gomethod, needs no separate return-type
// declaration to do this).
func (c *checker) checkGoFuncDecl(name string, call *ast.CallExpr, line int) error {
	if why, ok := reservedName(name); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", line, name, why)
	}
	if len(call.Args) < 2 {
		return fmt.Errorf("line %d: gofunc(...) takes at least two arguments (a Go function name, then a return-value prototype), got %d", call.Line, len(call.Args))
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

	var paramTypes []string
	if len(call.Args) > 2 {
		pt, err := goTypeArgs(call.Args[2:], call.Line, "gofunc(...)'s parameter type")
		if err != nil {
			return err
		}
		paramTypes = pt
	}

	if c.goFuncs == nil {
		c.goFuncs = map[string]*GoFuncInfo{}
	}
	c.goFuncs[name] = &GoFuncInfo{GoName: goName.Value, Proto: proto, ParamTypes: paramTypes}
	return nil
}

// trackGoStaticVar records name as statically Go-typed (weave_spec.md
// §16) when value is a direct call to a gofunc(...)-declared name whose
// own proto is a known gotype — e.g. `f = goOpen(path)`, matching §17's
// `self.file = goOpen(path)` pattern but for a plain variable (Step 4
// scope-narrows to this case — see CLAUDE.md's 後半 Step 4 "確定した
// 設計判断" for why an object property's static type isn't tracked).
// Any other assignment (including one whose RHS merely happens to be
// some other call) clears a stale entry: reassigning `f` to an
// ordinary dynamic value must make `f.whatever()` fall back to normal
// prototype-chain dispatch, not a leftover static resolution.
func (c *checker) trackGoStaticVar(name string, value ast.Expr) {
	if c.goStaticVars == nil {
		c.goStaticVars = map[string]string{}
	}
	if call, ok := value.(*ast.CallExpr); ok {
		if callee, ok := call.Callee.(*ast.Ident); ok {
			if info := c.goFuncs[callee.Name]; info != nil && info.Proto != "" {
				c.goStaticVars[name] = info.Proto
				return
			}
		}
	}
	delete(c.goStaticVars, name)
}

// checkGoMethodCall checks a call whose callee is a property access
// (`f.Method(args...)`). If Obj is a statically Go-typed variable
// (trackGoStaticVar), the property name must be one gotype(...) actually
// declared via gomethod(...) — an error here catches a typo/nonexistent
// method at compile time, instead of a confusing reflect panic from
// weavert.CallGoMethod at run time (internal/codegen/goasset.go emits
// the matching static-dispatch IR; see its own doc comment). If that
// method was declared with an explicit signature (GoMethodInfo.ReturnType
// != ""), the argument *count* is also checked here at compile time —
// each argument's *type* is checked at runtime via ASSERT (codegen), the
// one thing sema can't do without evaluating the program. Otherwise this
// is an ordinary dynamic method call, and only Obj itself needs checking
// (weave_spec.md §9's self-injection sugar; the property name is just a
// map key, not something sema validates for a plain object).
func (c *checker) checkGoMethodCall(prop *ast.PropExpr, args []ast.Expr, sc *scope) error {
	if id, ok := prop.Obj.(*ast.Ident); ok {
		if typeName, ok := c.goStaticVars[id.Name]; ok {
			info := c.goTypes[typeName]
			method, ok := info.Methods[prop.Prop]
			if !ok {
				return fmt.Errorf("line %d: %s has no gomethod-declared member %q", prop.Line, typeName, prop.Prop)
			}
			if method.ReturnType != "" && len(args) != len(method.ParamTypes) {
				return fmt.Errorf("line %d: %s.%s expects %d argument(s), got %d", prop.Line, typeName, prop.Prop, len(method.ParamTypes), len(args))
			}
			return nil
		}
	}
	return c.checkExpr(prop.Obj, sc)
}

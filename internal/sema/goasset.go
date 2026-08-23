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
//
// Every Go function/method call — typed or not — now always returns a
// Weave list (weave_spec.md §15.2's "常にlist" rule, a deliberate
// from-scratch redesign, not an incremental patch: this project's users
// are entirely internal, so there was no reason to preserve the older
// "scalar for 1 return value" behavior once it stopped being the best
// available design — see CLAUDE.md's 開発の進め方 7). `goReturns(...)`/
// `goParams(...)` (§15.4) replace the old single return-type argument,
// the old separate `proto` argument, and the old `goError(...)` wrapper
// all at once — see GoReturnSpec/GoMethodInfo/GoFuncInfo below.

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

// GoReturnSpec describes one declared Go return-value position inside a
// `goReturns(...)` list (weave_spec.md §15.4). Every typed gomethod(...)/
// gofunc(...) call's actual Go return values are collected — in order —
// into a Weave list (weave_spec.md §15.2), and GoReturnSpec is what lets
// the native (ASSERT/FNTYPE/CALL) dispatch path know each position's
// concrete Go type at compile time.
//
// Proto is "" for a plain scalar position ("?int", "?error", ...); when
// non-empty, this position is a struct/pointer value bound to an earlier
// gotype(...) declaration by that name (Type is then that gotype's own
// GoName) — the same "proto" concept weave_spec.md §15.1 has always had,
// just generalized from "the function's single return value" to "any
// position in its returns list". A proto-bound position's *runtime*
// value is still just the raw Go value itself (no wrapper object) —
// the proto binding is purely a compile-time fact propagated through
// goListShapes/goStaticVars (see trackGoAssetResult) so that a later
// `at(result, i)` extraction, assigned to its own variable, can still
// resolve `.Method()` calls on it natively.
type GoReturnSpec struct {
	Type  string // "?pkg.Type"-shaped Go type token; always set
	Proto string // "" for a plain value; else a gotype(...)-declared name
}

// GoMethodInfo is one `gomethod("GoName")` (untyped — dispatched at
// every call site via weavert.CallGoMethodList, reflection-based,
// returning a Weave list of every actual Go return value) or
// `gomethod("GoName", goReturns(...), goParams(...))` (typed — dispatched
// via a fully native ASSERT+FNTYPE+FGET+CALL sequence, no reflect at all;
// see internal/codegen/goasset.go's genNativeGoMethodCall). The typed
// form is opt-in and all-or-nothing per method: Returns and ParamTypes
// are either both nil (untyped) or both non-nil (typed — each may still
// be an empty, non-nil slice, e.g. a niladic void method is
// `gomethod("Reset", goReturns(), goParams())`). Declaring a signature is
// what turns a dynamic runtime type mismatch into an immediate,
// Weave-flavored error instead of relying on reflect's own (sometimes
// confusing) failure modes — the safety/maintainability benefit is the
// point, not raw speed (CLAUDE.md's design-decision note on this
// feature).
type GoMethodInfo struct {
	GoName     string
	Returns    []GoReturnSpec // nil = untyped/reflect-dispatched
	ParamTypes []string       // nil = untyped/reflect-dispatched
}

// GoFuncInfo is what `Y = gofunc("?pkg.Func")` (untyped) or `Y =
// gofunc("?pkg.Func", goReturns(...), goParams(...))` (typed) declares
// (weave_spec.md §15.2/§15.4) — GoName is the "?pkg.Func" string exactly
// as written, already the exact AMIVM callname shape a call to Y
// compiles straight to when typed (weave_spec.md §16: no dynamic
// dispatch at all). Same all-or-nothing shape and meaning for
// Returns/ParamTypes as GoMethodInfo.
type GoFuncInfo struct {
	GoName     string
	Returns    []GoReturnSpec
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
	case "goReturns":
		return "reserved for use in a gomethod(...)/gofunc(...) return-value position (weave_spec.md §15.4)", true
	case "goParams":
		return "reserved for use in a gomethod(...)/gofunc(...) parameter-type position (weave_spec.md §15.4)", true
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
		info, err := c.checkGoMethodArgs(mcall)
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
// dispatch) or `gomethod("GoName", goReturns(...), goParams(...))`
// (typed — native dispatch, all-or-nothing, weave_spec.md §15.1/§15.4).
// See GoMethodInfo's own doc comment for what each shape compiles to.
func (c *checker) checkGoMethodArgs(mcall *ast.CallExpr) (*GoMethodInfo, error) {
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
	if len(mcall.Args) != 3 {
		return nil, fmt.Errorf("line %d: gomethod(...) takes either just a name, or exactly three arguments (name, goReturns(...), goParams(...)), got %d", mcall.Line, len(mcall.Args))
	}
	returns, err := c.goReturnsArg(mcall.Args[1], mcall.Line)
	if err != nil {
		return nil, err
	}
	params, err := goParamsArg(mcall.Args[2], mcall.Line)
	if err != nil {
		return nil, err
	}
	return &GoMethodInfo{GoName: goMethodName.Value, Returns: returns, ParamTypes: params}, nil
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

// goReturnsArg validates `goReturns(type1, type2, ...)` (weave_spec.md
// §15.4) — each element is either a "?pkg.Type" string (a plain return
// value) or an Ident naming an earlier gotype(...) declaration (a
// struct/pointer return value, statically bound to that prototype —
// GoReturnSpec.Proto). 0 or more elements are allowed (a Go function
// with no return values declares `goReturns()`).
func (c *checker) goReturnsArg(expr ast.Expr, line int) ([]GoReturnSpec, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, fmt.Errorf("line %d: expected goReturns(...)", line)
	}
	callee, ok := call.Callee.(*ast.Ident)
	if !ok || callee.Name != "goReturns" {
		return nil, fmt.Errorf("line %d: expected goReturns(...)", line)
	}
	specs := make([]GoReturnSpec, len(call.Args))
	for i, a := range call.Args {
		switch v := a.(type) {
		case *ast.StringLit:
			if !strings.HasPrefix(v.Value, "?") {
				return nil, fmt.Errorf("line %d: goReturns(...)'s argument %d must start with '?' (e.g. \"?int\"), got %q", call.Line, i+1, v.Value)
			}
			specs[i] = GoReturnSpec{Type: v.Value}
		case *ast.Ident:
			info := c.goTypes[v.Name]
			if info == nil {
				return nil, fmt.Errorf("line %d: %q is not a gotype(...) declared earlier", v.Line, v.Name)
			}
			specs[i] = GoReturnSpec{Type: info.GoName, Proto: v.Name}
		default:
			return nil, fmt.Errorf("line %d: goReturns(...)'s argument %d must be a string literal or a gotype(...) name", call.Line, i+1)
		}
	}
	return specs, nil
}

// goParamsArg validates `goParams(type1, type2, ...)` (weave_spec.md
// §15.4) — a plain list of "?pkg.Type" strings, one per declared
// parameter (0 or more, for a niladic call).
func goParamsArg(expr ast.Expr, line int) ([]string, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, fmt.Errorf("line %d: expected goParams(...)", line)
	}
	callee, ok := call.Callee.(*ast.Ident)
	if !ok || callee.Name != "goParams" {
		return nil, fmt.Errorf("line %d: expected goParams(...)", line)
	}
	return goTypeArgs(call.Args, call.Line, "goParams(...)'s parameter type")
}

// checkGoFuncDecl checks `Y = gofunc("?pkg.Func")` (untyped) or `Y =
// gofunc("?pkg.Func", goReturns(...), goParams(...))` (typed, all-or-
// nothing — weave_spec.md §15.2/§15.4). See GoFuncInfo's own doc
// comment for what each shape compiles to.
func (c *checker) checkGoFuncDecl(name string, call *ast.CallExpr, line int) error {
	if why, ok := reservedName(name); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", line, name, why)
	}
	if len(call.Args) == 0 {
		return fmt.Errorf("line %d: gofunc(...) takes at least one argument (a Go function name), got 0", call.Line)
	}
	goName, ok := call.Args[0].(*ast.StringLit)
	if !ok {
		return fmt.Errorf("line %d: gofunc(...)'s first argument must be a string literal like \"?pkg.Func\"", call.Line)
	}
	if !strings.HasPrefix(goName.Value, "?") {
		return fmt.Errorf("line %d: gofunc(...)'s Go function name must start with '?' (weave_spec.md §15.2's own \"?os.Open\" shape), got %q", call.Line, goName.Value)
	}

	if c.goFuncs == nil {
		c.goFuncs = map[string]*GoFuncInfo{}
	}
	if len(call.Args) == 1 {
		c.goFuncs[name] = &GoFuncInfo{GoName: goName.Value}
		return nil
	}
	if len(call.Args) != 3 {
		return fmt.Errorf("line %d: gofunc(...) takes either just a name, or exactly three arguments (name, goReturns(...), goParams(...)), got %d", call.Line, len(call.Args))
	}
	returns, err := c.goReturnsArg(call.Args[1], call.Line)
	if err != nil {
		return err
	}
	params, err := goParamsArg(call.Args[2], call.Line)
	if err != nil {
		return err
	}
	c.goFuncs[name] = &GoFuncInfo{GoName: goName.Value, Returns: returns, ParamTypes: params}
	return nil
}

// trackGoAssetResult records what kind of Go-asset-shaped value `name`
// now holds, based on value's shape — mirrors codegen's own copy
// (internal/codegen/goasset.go's trackGoAssetResult, kept independently
// per this project's established "sema/codegen never share state"
// pattern). Every Go function/method call now always returns a Weave
// list (GoMethodInfo's own doc comment), so static tracking works in two
// tiers:
//
//   - goListShapes[name]: name was assigned directly from a typed
//     (Returns != nil) gofunc/gomethod call — records, per list
//     position, which gotype (if any) that position is statically bound
//     to ("" = plain scalar).
//   - goStaticVars[name]: name was assigned from `at(listVar, i)` where
//     listVar is itself a tracked list shape and position i is
//     proto-bound (trackAtResult) — name now holds that single Go
//     struct/pointer value directly, the same single-variable static
//     typing checkGoMethodCall has always worked from.
//
// Any other assignment clears both tables for name: reassigning to an
// ordinary dynamic value must make later `.Method()`/`at()` uses of it
// fall back to ordinary dynamic handling, never a stale static fact.
func (c *checker) trackGoAssetResult(name string, value ast.Expr) {
	if c.goStaticVars == nil {
		c.goStaticVars = map[string]string{}
	}
	if c.goListShapes == nil {
		c.goListShapes = map[string][]string{}
	}
	delete(c.goStaticVars, name)
	delete(c.goListShapes, name)

	call, ok := value.(*ast.CallExpr)
	if !ok {
		return
	}
	switch callee := call.Callee.(type) {
	case *ast.Ident:
		if callee.Name == "at" {
			c.trackAtResult(name, call)
			return
		}
		if info := c.goFuncs[callee.Name]; info != nil && info.Returns != nil {
			c.goListShapes[name] = protoShape(info.Returns)
		}
	case *ast.PropExpr:
		// obj.Method(...) where obj is itself a statically Go-typed
		// variable (weave_spec.md §9's self-injection sugar doesn't apply
		// to Go-asset method calls — checkGoMethodCall resolves Method
		// directly against the static type's own table).
		if id, ok := callee.Obj.(*ast.Ident); ok {
			if typeName, ok := c.goStaticVars[id.Name]; ok {
				if typeInfo := c.goTypes[typeName]; typeInfo != nil {
					if m := typeInfo.Methods[callee.Prop]; m != nil && m.Returns != nil {
						c.goListShapes[name] = protoShape(m.Returns)
					}
				}
			}
		}
	}
}

// trackAtResult handles `name = at(listVar, i)`: if listVar is a
// tracked list shape and position i (a literal, non-negative index) is
// proto-bound, name becomes statically Go-typed to that proto — see
// trackGoAssetResult's own doc comment. Any index expression other than
// a literal number (a variable, an arithmetic expression, ...) simply
// isn't tracked — this is a compile-time-only convenience, not a full
// data-flow analysis, so `at(...)`'s ordinary runtime behavior (reading
// whatever's actually there) is unaffected either way.
func (c *checker) trackAtResult(name string, call *ast.CallExpr) {
	if len(call.Args) != 2 {
		return
	}
	listID, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return
	}
	shape, ok := c.goListShapes[listID.Name]
	if !ok {
		return
	}
	idx, ok := call.Args[1].(*ast.NumberLit)
	if !ok {
		return
	}
	i := int(idx.Value)
	if i < 0 || i >= len(shape) || shape[i] == "" {
		return
	}
	c.goStaticVars[name] = shape[i]
}

// protoShape extracts the ordered proto-name list a GoReturnSpec slice
// implies (see GoReturnSpec.Proto) — shared by both branches of
// trackGoAssetResult.
func protoShape(returns []GoReturnSpec) []string {
	shape := make([]string, len(returns))
	for i, r := range returns {
		shape[i] = r.Proto
	}
	return shape
}

// checkGoMethodCall checks a call whose callee is a property access
// (`f.Method(args...)`). If Obj is a statically Go-typed variable
// (trackGoAssetResult), the property name must be one gotype(...)
// actually declared via gomethod(...) — an error here catches a
// typo/nonexistent method at compile time, instead of a confusing
// reflect panic from weavert.CallGoMethodList at run time
// (internal/codegen/goasset.go emits the matching static-dispatch IR;
// see its own doc comment). If that method was declared with an
// explicit signature (GoMethodInfo.ParamTypes != nil), the argument
// *count* is also checked here at compile time — each argument's *type*
// is checked at runtime via ASSERT (codegen), the one thing sema can't
// do without evaluating the program. Otherwise this is an ordinary
// dynamic method call, and only Obj itself needs checking (weave_spec.md
// §9's self-injection sugar; the property name is just a map key, not
// something sema validates for a plain object).
func (c *checker) checkGoMethodCall(prop *ast.PropExpr, args []ast.Expr, sc *scope) error {
	if id, ok := prop.Obj.(*ast.Ident); ok {
		if typeName, ok := c.goStaticVars[id.Name]; ok {
			info := c.goTypes[typeName]
			method, ok := info.Methods[prop.Prop]
			if !ok {
				return fmt.Errorf("line %d: %s has no gomethod-declared member %q", prop.Line, typeName, prop.Prop)
			}
			if method.ParamTypes != nil && len(args) != len(method.ParamTypes) {
				return fmt.Errorf("line %d: %s.%s expects %d argument(s), got %d", prop.Line, typeName, prop.Prop, len(method.ParamTypes), len(args))
			}
			return nil
		}
	}
	return c.checkExpr(prop.Obj, sc)
}

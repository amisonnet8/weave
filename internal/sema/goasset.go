package sema

import (
	"fmt"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// weave_spec.md §15's Go asset declarations (`gotype`/`gofunc`, §15.1's
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
// Every Go function/method call always returns a Weave list
// (weave_spec.md §15.2's "常にlist" rule, a deliberate from-scratch
// redesign, not an incremental patch: this project's users are entirely
// internal, so there was no reason to preserve the older "scalar for 1
// return value" behavior once it stopped being the best available
// design — see CLAUDE.md's 開発の進め方 7). Every call is dispatched via
// reflection (weavert.CallGoFuncList/CallGoMethodList) — there is no
// separate typed/native dispatch path any more (weave_spec.md §15.4's
// `goReturns(...)`/`goParams(...)` type hints, and §4.3's `shape`/
// `checkShape`, were both removed as half-baked; see CLAUDE.md's
// design-decision note on the removal). The only thing declared ahead
// of time is, optionally, which earlier gotype(...) a call's returned
// list holds at position 0 (Proto below) — enough to let a later
// `.Method()` call on that extracted value resolve statically, without
// needing to know anything about argument or return *types*.

// GoTypeInfo is what `X = gotype("?pkg.Type", { weaveName:
// gomethod("GoName"), ... })` declares. GoName is the "?pkg.Type"
// string exactly as written (optionally "?*pkg.Type" for a pointer
// receiver, weave_spec.md §15.1) — already shaped like an AMIVM
// callname token (see internal/codegen/goasset.go, which is what
// actually emits IR). Methods maps each Weave-visible member name to
// its GoMethodInfo.
type GoTypeInfo struct {
	GoName  string
	Methods map[string]*GoMethodInfo
}

// GoMethodInfo is one `gomethod("GoName")` or `gomethod("GoName",
// ProtoIdent)` declaration (weave_spec.md §15.1) — always dispatched at
// the call site via weavert.CallGoMethodList (reflection-based),
// returning a Weave list of every actual Go return value
// (weave_spec.md §15.2). Proto is "" ordinarily; when non-empty, it
// names an earlier gotype(...) declaration that position 0 of the
// returned list is statically bound to (see trackGoAssetResult) — the
// only piece of static information Weave tracks about a Go asset call's
// result, just enough to resolve a later `.Method()` chained off
// `at(result, 0)` at compile time instead of leaving it to a dynamic
// dispatch that doesn't otherwise exist for raw Go values.
type GoMethodInfo struct {
	GoName string
	Proto  string
}

// GoFuncInfo is what `Y = gofunc("?pkg.Func")` or `Y = gofunc("?pkg.Func",
// ProtoIdent)` declares (weave_spec.md §15.2) — GoName is the "?pkg.Func"
// string exactly as written, already the exact AMIVM callname/value
// shape a call to Y compiles straight to (weave_spec.md §16: the
// function reference itself is resolved statically, though the call
// still goes through reflection — see weavert.CallGoFuncList's own doc
// comment). Proto has the same meaning as GoMethodInfo.Proto.
type GoFuncInfo struct {
	GoName string
	Proto  string
}

// GoVarInfo is what `X = govar("?pkg.Var")` declares (weave_spec.md
// §15.4) — read-only, live access to a Go package-level variable (or
// constant; nothing at this string-token level distinguishes the two,
// and read access behaves identically either way). GoName is the
// "?pkg.Var" string exactly as written — already a valid AMIVM `value`
// operand (internal/codegen/goasset.go's genGoVarDecl embeds it directly
// as a CALL argument), unlike GoFuncInfo/GoMethodInfo there is no typed
// vs. untyped distinction: reading a package variable is always a plain
// Go expression (no method-name string to resolve, so no reflect is
// ever needed either way — see genGoVarRead's own doc comment).
type GoVarInfo struct {
	GoName string
}

func goAssetReservedName(name string) (string, bool) {
	switch name {
	case "gotype":
		return "reserved for Go type declarations (weave_spec.md §15.1)", true
	case "gofunc":
		return "reserved for Go function declarations (weave_spec.md §15.2)", true
	case "gomethod":
		return "reserved for use inside a gotype(...) member list (weave_spec.md §15.1)", true
	case "govar":
		return "reserved for Go package-level variable declarations (weave_spec.md §15.4)", true
	}
	return "", false
}

// checkGoDecl recognizes `name = gotype(...)` / `name = gofunc(...)` /
// `name = govar(...)` and handles them as compile-time declarations,
// returning (true, err) if it did. checkStmt's *ast.AssignStmt case
// calls this before its ordinary dynamic-value handling.
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
	case "govar":
		return true, c.checkGoVarDecl(name, call, line)
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

// checkGoMethodArgs validates `gomethod("GoName")` or `gomethod("GoName",
// ProtoIdent)` (weave_spec.md §15.1) — see GoMethodInfo's own doc
// comment for what each shape means.
func (c *checker) checkGoMethodArgs(mcall *ast.CallExpr) (*GoMethodInfo, error) {
	if len(mcall.Args) == 0 || len(mcall.Args) > 2 {
		return nil, fmt.Errorf("line %d: gomethod(...) takes a Go method name, and optionally a gotype(...) name for its position-0 return value, got %d argument(s)", mcall.Line, len(mcall.Args))
	}
	goMethodName, ok := mcall.Args[0].(*ast.StringLit)
	if !ok {
		return nil, fmt.Errorf("line %d: gomethod(...)'s first argument must be a string literal (the Go method name)", mcall.Line)
	}
	if len(mcall.Args) == 1 {
		return &GoMethodInfo{GoName: goMethodName.Value}, nil
	}
	proto, err := c.protoArg(mcall.Args[1], mcall.Line)
	if err != nil {
		return nil, err
	}
	return &GoMethodInfo{GoName: goMethodName.Value, Proto: proto}, nil
}

// protoArg validates a gofunc(...)/gomethod(...) declaration's optional
// second argument — an Ident naming an earlier gotype(...) declaration,
// binding position 0 of the call's returned list to that prototype
// (weave_spec.md §15.1/§15.2). line is the enclosing gofunc(...)/
// gomethod(...) call's own line, used when expr itself isn't even an
// Ident (so there's no line of its own to report).
func (c *checker) protoArg(expr ast.Expr, line int) (string, error) {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("line %d: expected a gotype(...) name naming the returned value's prototype", line)
	}
	if c.goTypes[id.Name] == nil {
		return "", fmt.Errorf("line %d: %q is not a gotype(...) declared earlier", id.Line, id.Name)
	}
	return id.Name, nil
}

// checkGoFuncDecl checks `Y = gofunc("?pkg.Func")` or `Y =
// gofunc("?pkg.Func", ProtoIdent)` (weave_spec.md §15.2). See
// GoFuncInfo's own doc comment for what each shape means.
func (c *checker) checkGoFuncDecl(name string, call *ast.CallExpr, line int) error {
	if why, ok := reservedName(name); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", line, name, why)
	}
	if len(call.Args) == 0 || len(call.Args) > 2 {
		return fmt.Errorf("line %d: gofunc(...) takes a Go function name, and optionally a gotype(...) name for its position-0 return value, got %d argument(s)", call.Line, len(call.Args))
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
	proto, err := c.protoArg(call.Args[1], call.Line)
	if err != nil {
		return err
	}
	c.goFuncs[name] = &GoFuncInfo{GoName: goName.Value, Proto: proto}
	return nil
}

// checkGoVarDecl checks `X = govar("?pkg.Var")` (weave_spec.md §15.4) —
// always exactly one argument.
func (c *checker) checkGoVarDecl(name string, call *ast.CallExpr, line int) error {
	if why, ok := reservedName(name); ok {
		return fmt.Errorf("line %d: %q is a reserved name (%s)", line, name, why)
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("line %d: govar(...) takes exactly one argument (a Go variable name), got %d", call.Line, len(call.Args))
	}
	goName, ok := call.Args[0].(*ast.StringLit)
	if !ok {
		return fmt.Errorf("line %d: govar(...)'s argument must be a string literal like \"?pkg.Var\"", call.Line)
	}
	if !strings.HasPrefix(goName.Value, "?") {
		return fmt.Errorf("line %d: govar(...)'s Go variable name must start with '?' (weave_spec.md §15.4's own \"?os.Stdout\" shape), got %q", call.Line, goName.Value)
	}

	if c.goVars == nil {
		c.goVars = map[string]*GoVarInfo{}
	}
	c.goVars[name] = &GoVarInfo{GoName: goName.Value}
	return nil
}

// trackGoAssetResult records what kind of Go-asset-shaped value `name`
// now holds, based on value's shape — mirrors codegen's own copy
// (internal/codegen/goasset.go's trackGoAssetResult, kept independently
// per this project's established "sema/codegen never share state"
// pattern). Every Go function/method call always returns a Weave list
// (GoMethodInfo's own doc comment), so static tracking works in two
// tiers:
//
//   - goListProto[name]: name was assigned directly from a proto-bound
//     (Proto != "") gofunc/gomethod call — records which gotype position
//     0 of the returned list is statically bound to.
//   - goStaticVars[name]: name was assigned from `at(listVar, 0)` where
//     listVar is itself a tracked, proto-bound list — name now holds
//     that single Go struct/pointer value directly, the same
//     single-variable static typing checkGoMethodCall has always worked
//     from.
//
// Any other assignment clears both tables for name: reassigning to an
// ordinary dynamic value must make later `.Method()`/`at()` uses of it
// fall back to ordinary dynamic handling, never a stale static fact.
func (c *checker) trackGoAssetResult(name string, value ast.Expr) {
	if c.goStaticVars == nil {
		c.goStaticVars = map[string]string{}
	}
	if c.goListProto == nil {
		c.goListProto = map[string]string{}
	}
	delete(c.goStaticVars, name)
	delete(c.goListProto, name)

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
		if info := c.goFuncs[callee.Name]; info != nil && info.Proto != "" {
			c.goListProto[name] = info.Proto
		}
	case *ast.PropExpr:
		// obj.Method(...) where obj is itself a statically Go-typed
		// variable (weave_spec.md §9's self-injection sugar doesn't apply
		// to Go-asset method calls — checkGoMethodCall resolves Method
		// directly against the static type's own table).
		if id, ok := callee.Obj.(*ast.Ident); ok {
			if typeName, ok := c.goStaticVars[id.Name]; ok {
				if typeInfo := c.goTypes[typeName]; typeInfo != nil {
					if m := typeInfo.Methods[callee.Prop]; m != nil && m.Proto != "" {
						c.goListProto[name] = m.Proto
					}
				}
			}
		}
	}
}

// trackAtResult handles `name = at(listVar, 0)`: if listVar is a
// tracked, proto-bound list and the index is the literal 0, name becomes
// statically Go-typed to that proto — see trackGoAssetResult's own doc
// comment. Position 0 is the only position ever tracked (weave_spec.md
// §15.2's Go convention of "primary value first" — a proto binding only
// ever applies to the function/method's own single struct/pointer return
// value). Any other index, or a non-literal index expression (a
// variable, an arithmetic expression, ...), simply isn't tracked — this
// is a compile-time-only convenience, not a full data-flow analysis, so
// `at(...)`'s ordinary runtime behavior (reading whatever's actually
// there) is unaffected either way.
func (c *checker) trackAtResult(name string, call *ast.CallExpr) {
	if len(call.Args) != 2 {
		return
	}
	listID, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return
	}
	proto, ok := c.goListProto[listID.Name]
	if !ok {
		return
	}
	idx, ok := call.Args[1].(*ast.NumberLit)
	if !ok || idx.Value != 0 {
		return
	}
	c.goStaticVars[name] = proto
}

// checkGoMethodCall checks a call whose callee is a property access
// (`f.Method(args...)`). If Obj is a statically Go-typed variable
// (trackGoAssetResult), the property name must be one gotype(...)
// actually declared via gomethod(...) — an error here catches a
// typo/nonexistent method at compile time, instead of a confusing
// reflect panic from weavert.CallGoMethodList at run time
// (internal/codegen/goasset.go emits the matching static-dispatch IR;
// see its own doc comment). Otherwise this is an ordinary dynamic method
// call, and only Obj itself needs checking (weave_spec.md §9's
// self-injection sugar; the property name is just a map key, not
// something sema validates for a plain object).
func (c *checker) checkGoMethodCall(prop *ast.PropExpr, sc *scope) error {
	if id, ok := prop.Obj.(*ast.Ident); ok {
		if typeName, ok := c.goStaticVars[id.Name]; ok {
			info := c.goTypes[typeName]
			if _, ok := info.Methods[prop.Prop]; !ok {
				return fmt.Errorf("line %d: %s has no gomethod-declared member %q", prop.Line, typeName, prop.Prop)
			}
			return nil
		}
	}
	return c.checkExpr(prop.Obj, sc)
}

package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// GoTypeInfo/GoMethodInfo/GoFuncInfo mirror sema's own (internal/sema/
// goasset.go) — a separate copy per the established pattern of codegen
// re-deriving its own view of the AST rather than consuming sema's
// internal state (see CLAUDE.md's "確定した設計判断" on manually-synced
// tables, e.g. builtinNames/reservedName). See sema's copies for the
// full reasoning behind the typed vs. untyped shapes.
type GoTypeInfo struct {
	GoName  string
	Methods map[string]*GoMethodInfo
}

type GoMethodInfo struct {
	GoName     string
	ReturnType string // "" means untyped/reflect-dispatched
	ParamTypes []string
}

type GoFuncInfo struct {
	GoName     string
	Proto      string
	ParamTypes []string // nil means untyped/reflect-dispatched
}

// genGoDecl mirrors sema's own recognition of `name = gotype(...)` /
// `name = gofunc(...)` (sema/goasset.go's checkGoDecl) — codegen has
// already been told by sema.Check that the file is valid, so this just
// re-derives the same symbol tables to know how to lower later
// references. Neither declaration form emits any IR itself (weave_spec.md
// §16: these are compile-time-only, never a runtime value) — genAssignStmt
// calls this before its ordinary dynamic-value handling and, if it
// reports true, skips VAR/SET entirely for this statement.
func genGoDecl(fg *funcGen, name string, call *ast.CallExpr) (bool, error) {
	callee, ok := call.Callee.(*ast.Ident)
	if !ok {
		return false, nil
	}
	switch callee.Name {
	case "gotype":
		genGoTypeDecl(fg, name, call)
		return true, nil
	case "gofunc":
		genGoFuncDecl(fg, name, call)
		return true, nil
	default:
		return false, nil
	}
}

func genGoTypeDecl(fg *funcGen, name string, call *ast.CallExpr) {
	goName := call.Args[0].(*ast.StringLit).Value
	members := call.Args[1].(*ast.ObjectLit)
	methods := map[string]*GoMethodInfo{}
	for _, field := range members.Fields {
		mcall := field.Value.(*ast.CallExpr)
		methods[field.Name] = goMethodInfoFromArgs(mcall.Args)
	}
	if fg.ctx.goTypes == nil {
		fg.ctx.goTypes = map[string]*GoTypeInfo{}
	}
	fg.ctx.goTypes[name] = &GoTypeInfo{GoName: goName, Methods: methods}
}

// goMethodInfoFromArgs re-derives one gomethod(...) call's info — sema
// has already validated the shape (checkGoMethodArgs), so this doesn't
// re-check anything, just extracts it.
func goMethodInfoFromArgs(args []ast.Expr) *GoMethodInfo {
	goName := args[0].(*ast.StringLit).Value
	if len(args) == 1 {
		return &GoMethodInfo{GoName: goName}
	}
	returnType := args[1].(*ast.StringLit).Value
	paramTypes := make([]string, len(args)-2)
	for i, a := range args[2:] {
		paramTypes[i] = a.(*ast.StringLit).Value
	}
	return &GoMethodInfo{GoName: goName, ReturnType: returnType, ParamTypes: paramTypes}
}

func genGoFuncDecl(fg *funcGen, name string, call *ast.CallExpr) {
	goName := call.Args[0].(*ast.StringLit).Value
	proto := ""
	if id, ok := call.Args[1].(*ast.Ident); ok {
		proto = id.Name
	}
	var paramTypes []string
	if len(call.Args) > 2 {
		paramTypes = make([]string, len(call.Args)-2)
		for i, a := range call.Args[2:] {
			paramTypes[i] = a.(*ast.StringLit).Value
		}
	}
	if fg.ctx.goFuncs == nil {
		fg.ctx.goFuncs = map[string]*GoFuncInfo{}
	}
	fg.ctx.goFuncs[name] = &GoFuncInfo{GoName: goName, Proto: proto, ParamTypes: paramTypes}
}

// genGoFuncCall lowers a call to a gofunc(...)-declared name. Without
// declared parameter types (info.ParamTypes == nil) it routes through
// weavert.CallGoFunc exactly as before — see that function's own doc
// comment in weavert/goasset.go for why reflect is unavoidable there
// (an arbitrary `any` argument can't be passed to a specific concrete Go
// parameter type without either a type assertion or reflect.Convert).
//
// With declared parameter types, each argument is ASSERTed from `^any`
// down to its declared concrete type (weave_spec.md's "型を書けばASSERT
// でネイティブ・厳格に" rule — CLAUDE.md's design-decision note), and the
// real Go function is then called *directly* — no weavert.CallGoFunc, no
// reflect at all for the call itself. The result variable is declared
// `^any` and the raw call result is assigned straight into it (Go freely
// boxes any concrete return value into an `any`-typed variable via plain
// `=`, unlike the any->concrete direction ASSERT is needed for) — so,
// unlike genNativeGoMethodCall, there is no need to know the *return*
// type at all here: weavert.NormalizeGoValue (a plain type switch, not
// reflect) still handles the "was it actually a numeric Go type, box as
// float64" step generically. See GoFuncInfo's own doc comment for why
// this asymmetry with gomethod (which does need FNTYPE's return type) is
// real, not an oversight.
func genGoFuncCall(fg *funcGen, info *GoFuncInfo, call *ast.CallExpr) (string, error) {
	if info.ParamTypes == nil {
		argVals := []string{info.GoName}
		for _, arg := range call.Args {
			v, err := genExpr(fg, arg)
			if err != nil {
				return "", err
			}
			argVals = append(argVals, v)
		}
		tmp := fg.newTemp("^any")
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CallGoFunc%s\n", tmp, argSuffix(argVals))
		return tmp, nil
	}

	if len(call.Args) != len(info.ParamTypes) {
		return "", fmt.Errorf("line %d: %s expects %d argument(s), got %d", call.Line, info.GoName, len(info.ParamTypes), len(call.Args))
	}
	concreteArgs := make([]string, len(call.Args))
	for i, arg := range call.Args {
		v, err := genExpr(fg, arg)
		if err != nil {
			return "", err
		}
		desc := fmt.Sprintf("argument %d to %s", i+1, info.GoName)
		concreteArgs[i] = genAssertOrTypeError(fg, v, info.ParamTypes[i], desc)
	}

	raw := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t%s%s\n", raw, info.GoName, argSuffix(concreteArgs))
	result := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.NormalizeGoValue\t%s\n", result, raw)
	return result, nil
}

func argSuffix(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return "\t" + strings.Join(args, "\t")
}

// trackGoStaticVar mirrors sema's own tracking (internal/sema/goasset.go's
// trackGoStaticVar — see its doc comment for the full reasoning): name
// is statically Go-typed only when value is a direct call to a
// gofunc(...)-declared name whose own proto is a known gotype; any other
// assignment clears a stale entry.
func trackGoStaticVar(fg *funcGen, name string, value ast.Expr) {
	if fg.goStaticVars == nil {
		fg.goStaticVars = map[string]string{}
	}
	if call, ok := value.(*ast.CallExpr); ok {
		if callee, ok := call.Callee.(*ast.Ident); ok {
			if info := fg.ctx.goFuncs[callee.Name]; info != nil && info.Proto != "" {
				fg.goStaticVars[name] = info.Proto
				return
			}
		}
	}
	delete(fg.goStaticVars, name)
}

// genGoMethodCall lowers `f.Method(args...)` when f is a statically
// Go-typed variable (trackGoStaticVar) — bypassing the dynamic
// prototype-chain dispatch genMethodCall otherwise uses (weave_spec.md
// §16). The method name itself is resolved here, at compile time, from
// gotype(...)'s own declared table — never looked up dynamically.
//
// Without a declared signature (weave_spec.md §15.1's plain
// `gomethod("Name")`), the call still goes through weavert.CallGoMethod
// (reflect) exactly as before: f's *generated* Go code only ever sees it
// as `any`, so "static resolution" without a signature means the method
// *name* is known, not that the Go call itself can skip reflect.
//
// With a declared signature (genNativeGoMethodCall), the call becomes
// fully native — ASSERT extracts f's concrete receiver type, FNTYPE+FGET
// extract the method as a real Go method value, and CALL invokes it
// directly. No reflect anywhere in this path.
//
// The bool result tells genGeneralCall whether this applied at all —
// prop.Obj not being a tracked static variable is the ordinary case (an
// ordinary Weave object), not an error.
func genGoMethodCall(fg *funcGen, prop *ast.PropExpr, args []ast.Expr) (bool, string, error) {
	id, ok := prop.Obj.(*ast.Ident)
	if !ok {
		return false, "", nil
	}
	typeName, ok := fg.goStaticVars[id.Name]
	if !ok {
		return false, "", nil
	}
	typeInfo := fg.ctx.goTypes[typeName]
	methodInfo := typeInfo.Methods[prop.Prop]

	objVal, err := genExpr(fg, prop.Obj)
	if err != nil {
		return true, "", err
	}
	argVals := make([]string, len(args))
	for i, arg := range args {
		v, err := genExpr(fg, arg)
		if err != nil {
			return true, "", err
		}
		argVals[i] = v
	}

	if methodInfo.ReturnType == "" {
		tmp := fg.newTemp("^any")
		callArgs := append([]string{objVal, strconv.Quote(methodInfo.GoName)}, argVals...)
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CallGoMethod\t%s\n", tmp, strings.Join(callArgs, "\t"))
		return true, tmp, nil
	}

	result, err := genNativeGoMethodCall(fg, typeInfo, methodInfo, objVal, argVals, prop.Line)
	return true, result, err
}

// genNativeGoMethodCall lowers a typed gomethod(...) call to a fully
// native ASSERT+FNTYPE+FGET+CALL sequence — see genGoMethodCall's doc
// comment. Every step down to the final CALL only ever deals in
// concretely-typed AMIVM values (never `^any`), matching amivm's own
// method-call pattern (amivm_instruction_spec.md §8) exactly.
func genNativeGoMethodCall(fg *funcGen, typeInfo *GoTypeInfo, methodInfo *GoMethodInfo, objVal string, argVals []string, line int) (string, error) {
	if len(argVals) != len(methodInfo.ParamTypes) {
		return "", fmt.Errorf("line %d: %s.%s expects %d argument(s), got %d", line, typeInfo.GoName, methodInfo.GoName, len(methodInfo.ParamTypes), len(argVals))
	}

	recv := genAssertOrTypeError(fg, objVal, typeInfo.GoName, "method receiver")

	concreteArgs := make([]string, len(argVals))
	for i, av := range argVals {
		desc := fmt.Sprintf("argument %d to %s.%s", i+1, typeInfo.GoName, methodInfo.GoName)
		concreteArgs[i] = genAssertOrTypeError(fg, av, methodInfo.ParamTypes[i], desc)
	}

	paramTypeTokens := make([]string, len(methodInfo.ParamTypes))
	for i, pt := range methodInfo.ParamTypes {
		paramTypeTokens[i] = goTypeToken(pt)
	}
	retType := goTypeToken(methodInfo.ReturnType)
	fnType := fg.ctx.newGoFnType(paramTypeTokens, retType)

	methodVal := fg.newTemp(fnType)
	fmt.Fprintf(&fg.body, "\tFGET\t%s\t%s\t>%s\n", methodVal, recv, methodInfo.GoName)

	raw := fg.newTemp(retType)
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t%s%s\n", raw, methodVal, argSuffix(concreteArgs))

	result := fg.newTemp("^any")
	if isNumericGoType(retType) {
		casted := fg.newTemp("^float64")
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?float64\t%s\n", casted, raw)
		fmt.Fprintf(&fg.body, "\tSET\t%s\t%s\n", result, casted)
	} else {
		fmt.Fprintf(&fg.body, "\tSET\t%s\t%s\n", result, raw)
	}
	return result, nil
}

// genAssertOrTypeError ASSERTs anyVal (an `^any` value token) down to
// goRef's concrete AMIVM type (goRef is a "?pkg.Type"-shaped string, as
// written in a gotype(...)/gomethod(...)/gofunc(...) declaration —
// goTypeToken converts it to the `^`-prefixed IR form), using the
// comma-ok form so a mismatch never produces a raw Go "interface
// conversion" panic — instead it calls weavert.TypeError with desc (a
// human-readable location, e.g. "argument 1 to strings.Reader.Len") for
// a clear, Weave-flavored message. Returns the token of the now
// concretely-typed value, valid to use directly as a native CALL/FGET
// argument.
func genAssertOrTypeError(fg *funcGen, anyVal, goRef, desc string) string {
	anyVal = ensureVariable(fg, anyVal)
	concreteType := goTypeToken(goRef)
	concrete := fg.newTemp(concreteType)
	ok := fg.newTemp("^bool")
	fmt.Fprintf(&fg.body, "\tASSERT\t%s\t%s\t%s\t%s\n", concrete, ok, anyVal, concreteType)
	notOk := fg.newTemp("^bool")
	fmt.Fprintf(&fg.body, "\tNOT\t%s\t%s\n", notOk, ok)
	fmt.Fprintf(&fg.body, "\tIF\t%s\n", notOk)
	fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.TypeError\t%s\t%s\t%s\n", strconv.Quote(desc), strconv.Quote(concreteType), anyVal)
	fg.body.WriteString("\tENDIF\n")
	return concrete
}

// ensureVariable materializes val (an AMIVM `value` token, possibly a
// literal) into a fresh `^any` variable if it isn't one already —
// ASSERT's `variable` operand category (amivm_spec.md §5) accepts only
// $N/&N/%xxx/@xxx/@xxx.yyy references, never a literal directly, unlike
// most other operand categories (which accept both). genExpr only ever
// returns a non-variable token for the four literal AST kinds (number/
// string/bool/nil) — every other expression already resolves to a %-temp
// or a %/@-prefixed reference.
func ensureVariable(fg *funcGen, val string) string {
	if val != "" {
		switch val[0] {
		case '$', '&', '%', '@':
			return val
		}
	}
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tSET\t%s\t%s\n", tmp, val)
	return tmp
}

// goTypeToken converts a "?pkg.Type"/"?*pkg.Type"-shaped declaration
// string (gotype/gomethod/gofunc's own argument shape, weave_spec.md
// §15.1) into the `^`-prefixed AMIVM type token it names — the two
// prefixes line up with amivm's own type category (amivm_spec.md §7)
// one swap away: `?strings.Reader` -> `^strings.Reader`, `?*os.File` ->
// `^*os.File`.
func goTypeToken(goRef string) string {
	return "^" + strings.TrimPrefix(goRef, "?")
}

// numericGoTypeNames mirrors weavert.NormalizeGoValue's own switch
// (weavert/goasset.go) — every Go numeric kind that needs boxing as
// float64 to match weave_spec.md §2's "numbers are always float64"
// rule. float64 itself is deliberately absent: it's already the target
// representation, so no cast is needed for it.
var numericGoTypeNames = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true,
}

// isNumericGoType reports whether typeToken (a `^`-prefixed AMIVM type
// token, e.g. "^int" or "^*int") names one of the Go numeric kinds
// genNativeGoMethodCall must cast to float64 before boxing into `^any`.
func isNumericGoType(typeToken string) bool {
	return numericGoTypeNames[strings.TrimPrefix(typeToken, "^")]
}

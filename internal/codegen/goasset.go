package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// GoTypeInfo/GoReturnSpec/GoMethodInfo/GoFuncInfo mirror sema's own
// (internal/sema/goasset.go) — a separate copy per the established
// pattern of codegen re-deriving its own view of the AST rather than
// consuming sema's internal state (see CLAUDE.md's "確定した設計判断" on
// manually-synced tables, e.g. builtinNames/reservedName). See sema's
// copies for the full reasoning behind the typed vs. untyped shapes and
// the "every Go call always returns a list" design.
type GoTypeInfo struct {
	GoName  string
	Methods map[string]*GoMethodInfo
}

type GoReturnSpec struct {
	Type  string
	Proto string
}

type GoMethodInfo struct {
	GoName     string
	Returns    []GoReturnSpec // nil = untyped/reflect-dispatched
	ParamTypes []string       // nil = untyped/reflect-dispatched
}

type GoFuncInfo struct {
	GoName     string
	Returns    []GoReturnSpec
	ParamTypes []string
}

// GoVarInfo mirrors sema's own (internal/sema/goasset.go's GoVarInfo) —
// see its doc comment for why there's no typed/untyped split here.
type GoVarInfo struct {
	GoName string
}

// genGoDecl mirrors sema's own recognition of `name = gotype(...)` /
// `name = gofunc(...)` / `name = govar(...)` (sema/goasset.go's
// checkGoDecl) — codegen has already been told by sema.Check that the
// file is valid, so this just re-derives the same symbol tables to know
// how to lower later references. None of the three declaration forms
// emits any IR itself (weave_spec.md §16: these are compile-time-only,
// never a runtime value) — genAssignStmt calls this before its ordinary
// dynamic-value handling and, if it reports true, skips VAR/SET entirely
// for this statement.
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
	case "govar":
		genGoVarDecl(fg, name, call)
		return true, nil
	default:
		return false, nil
	}
}

// genGoVarDecl re-derives one `X = govar("?pkg.Var")` declaration
// (weave_spec.md §15.5) already validated by sema (checkGoVarDecl) —
// just records GoName for genExpr's *ast.Ident case (below) to read live
// on every later reference to X.
func genGoVarDecl(fg *funcGen, name string, call *ast.CallExpr) {
	goName := call.Args[0].(*ast.StringLit).Value
	if fg.ctx.goVars == nil {
		fg.ctx.goVars = map[string]*GoVarInfo{}
	}
	fg.ctx.goVars[name] = &GoVarInfo{GoName: goName}
}

// genGoVarRead lowers a read of a govar(...)-declared name (weave_spec.md
// §15.5) to a single native CALL: info.GoName ("?pkg.Var") is already a
// valid AMIVM `value` operand — the exact same "a bare ?pkg.xxx token is
// just a raw Go expression, embeddable as a first-class value" trick
// genGoFuncCall's own doc comment established for passing a *function*
// value into weavert.CallGoFuncList, except here the raw expression IS
// the whole read: no reflect is needed at all (there's no method-name
// string to resolve dynamically the way CallGoMethodList needs — the
// identifier itself is fully static). Every reference re-emits its own
// CALL rather than caching a result anywhere (weave_spec.md §15.5's
// "live" semantics: each read reflects pkg.Var's actual value at the
// moment this line executes, not a value snapshotted once at
// declaration — see CLAUDE.md's design-decision note on why this was
// the deliberately chosen behavior over a one-time snapshot).
// weavert.NormalizeGoValue is reused as-is (no new weavert helper
// needed) purely for the same numeric/[]byte normalization every other
// Go-asset boundary already applies.
func genGoVarRead(fg *funcGen, info *GoVarInfo) string {
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.NormalizeGoValue\t%s\n", tmp, info.GoName)
	return tmp
}

func genGoTypeDecl(fg *funcGen, name string, call *ast.CallExpr) {
	goName := call.Args[0].(*ast.StringLit).Value
	members := call.Args[1].(*ast.ObjectLit)
	methods := map[string]*GoMethodInfo{}
	for _, field := range members.Fields {
		mcall := field.Value.(*ast.CallExpr)
		methods[field.Name] = goMethodInfoFromArgs(fg, mcall.Args)
	}
	if fg.ctx.goTypes == nil {
		fg.ctx.goTypes = map[string]*GoTypeInfo{}
	}
	fg.ctx.goTypes[name] = &GoTypeInfo{GoName: goName, Methods: methods}
}

// goMethodInfoFromArgs re-derives one gomethod(...) call's info — sema
// has already validated the shape (checkGoMethodArgs), so this doesn't
// re-check anything, just extracts it.
func goMethodInfoFromArgs(fg *funcGen, args []ast.Expr) *GoMethodInfo {
	goName := args[0].(*ast.StringLit).Value
	if len(args) == 1 {
		return &GoMethodInfo{GoName: goName}
	}
	returns := goReturnsArgValue(fg, args[1])
	params := goParamsArgValue(args[2])
	return &GoMethodInfo{GoName: goName, Returns: returns, ParamTypes: params}
}

// goReturnsArgValue re-derives a `goReturns(...)` call already validated
// by sema — each element is either a "?pkg.Type" string literal, or an
// Ident naming an earlier gotype(...) declaration (fg.ctx.goTypes).
func goReturnsArgValue(fg *funcGen, expr ast.Expr) []GoReturnSpec {
	call := expr.(*ast.CallExpr)
	specs := make([]GoReturnSpec, len(call.Args))
	for i, a := range call.Args {
		switch v := a.(type) {
		case *ast.StringLit:
			specs[i] = GoReturnSpec{Type: v.Value}
		case *ast.Ident:
			info := fg.ctx.goTypes[v.Name]
			specs[i] = GoReturnSpec{Type: info.GoName, Proto: v.Name}
		}
	}
	return specs
}

// goParamsArgValue re-derives a `goParams(...)` call already validated
// by sema — a plain list of "?pkg.Type" string literals.
func goParamsArgValue(expr ast.Expr) []string {
	call := expr.(*ast.CallExpr)
	params := make([]string, len(call.Args))
	for i, a := range call.Args {
		params[i] = a.(*ast.StringLit).Value
	}
	return params
}

func genGoFuncDecl(fg *funcGen, name string, call *ast.CallExpr) {
	goName := call.Args[0].(*ast.StringLit).Value
	if fg.ctx.goFuncs == nil {
		fg.ctx.goFuncs = map[string]*GoFuncInfo{}
	}
	if len(call.Args) == 1 {
		fg.ctx.goFuncs[name] = &GoFuncInfo{GoName: goName}
		return
	}
	returns := goReturnsArgValue(fg, call.Args[1])
	params := goParamsArgValue(call.Args[2])
	fg.ctx.goFuncs[name] = &GoFuncInfo{GoName: goName, Returns: returns, ParamTypes: params}
}

// genGoFuncCall lowers a call to a gofunc(...)-declared name. Without a
// declared signature (info.ParamTypes == nil) it routes through
// weavert.CallGoFuncList — see that function's own doc comment in
// weavert/goasset.go for why reflect is unavoidable there (an arbitrary
// `any` argument can't be passed to a specific concrete Go parameter
// type without either a type assertion or reflect.Convert) — which
// always returns a Weave list of every actual Go return value
// (weave_spec.md §15.2's "常にlist" rule).
//
// With a declared signature, each argument is ASSERTed from `^any` down
// to its declared concrete type (weave_spec.md's "型を書けばASSERTで
// ネイティブ・厳格に" rule — CLAUDE.md's design-decision note), and the
// real Go function is called *directly* via a native multi-target CALL
// (one target per declared return position, matching info.Returns'
// length exactly — including zero) — no weavert.CallGoFuncList, no
// reflect at all. Each raw result is then boxed into a Weave list via
// genBoxList, exactly like the untyped path's result, just built
// natively instead of via reflection.
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
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CallGoFuncList%s\n", tmp, argSuffix(argVals))
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

	raws := make([]string, len(info.Returns))
	for i, r := range info.Returns {
		raws[i] = fg.newTemp(goTypeToken(fg, r.Type))
	}
	fmt.Fprintf(&fg.body, "\tCALL\t%s:\t%s%s\n", callTargetsPrefix(raws), info.GoName, argSuffix(concreteArgs))
	return genBoxList(fg, raws, info.Returns), nil
}

// argSuffix formats zero or more trailing operands for a CALL/FNTYPE
// line — "" when empty (so a niladic call's line has no dangling tab),
// else a leading tab followed by each operand, tab-separated.
func argSuffix(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return "\t" + strings.Join(args, "\t")
}

// callTargetsPrefix formats zero or more result-token operands for a
// native CALL (amivm's `CALL multi1 multi2 ... : callname ...` shape —
// the same category ASSERT's own multi1/multi2 uses), including the
// trailing tab that must separate the last target from the `:`
// separator — or "" when there are no return values at all (matching
// CLAUDE.md's "CALLの結果省略は本当に省略する" convention: an empty
// section, not an empty-string operand).
func callTargetsPrefix(targets []string) string {
	if len(targets) == 0 {
		return ""
	}
	return strings.Join(targets, "\t") + "\t"
}

// genBoxList builds a Weave list object (weave_spec.md §3 — the same
// weavert.NewObject+ObjSet shape genListCall uses for a literal
// list(...), since every Go-asset call result is exactly that kind of
// object too) from a slice of raw, natively-typed CALL results, boxing
// each into `^any` via genNativeReturnValue first (numeric/[]byte
// conversion where needed). This is the typed path's landing point for
// weave_spec.md §15.2's "every Go call always returns a list" rule —
// the untyped path's landing point is weavert.CallGoFuncList/
// CallGoMethodList building the equivalent list directly in Go via
// reflection.
func genBoxList(fg *funcGen, raws []string, returns []GoReturnSpec) string {
	obj := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.NewObject\n", obj)
	for i, raw := range returns {
		boxed := genNativeReturnValue(fg, raws[i], raw.Type)
		fmt.Fprintf(&fg.body, "\tCALL\t:\t?weavert.ObjSet\t%s\t%s\t%s\n", obj, quoteKey(strconv.Itoa(i)), boxed)
	}
	return obj
}

// trackGoAssetResult mirrors sema's own tracking (internal/sema/goasset.go's
// trackGoAssetResult — see its doc comment for the full reasoning): name
// is statically Go-typed (fg.goStaticVars) when value is `at(listVar, i)`
// extracting a proto-bound position from a tracked list shape, or holds
// a tracked list shape itself (fg.goListShapes) when value is a direct
// call to a typed gofunc(...)/gomethod(...). Any other assignment clears
// both.
func trackGoAssetResult(fg *funcGen, name string, value ast.Expr) {
	if fg.goStaticVars == nil {
		fg.goStaticVars = map[string]string{}
	}
	if fg.goListShapes == nil {
		fg.goListShapes = map[string][]string{}
	}
	delete(fg.goStaticVars, name)
	delete(fg.goListShapes, name)

	call, ok := value.(*ast.CallExpr)
	if !ok {
		return
	}
	switch callee := call.Callee.(type) {
	case *ast.Ident:
		if callee.Name == "at" {
			trackAtResult(fg, name, call)
			return
		}
		if info := fg.ctx.goFuncs[callee.Name]; info != nil && info.Returns != nil {
			fg.goListShapes[name] = protoShape(info.Returns)
		}
	case *ast.PropExpr:
		if id, ok := callee.Obj.(*ast.Ident); ok {
			if typeName, ok := fg.goStaticVars[id.Name]; ok {
				if typeInfo := fg.ctx.goTypes[typeName]; typeInfo != nil {
					if m := typeInfo.Methods[callee.Prop]; m != nil && m.Returns != nil {
						fg.goListShapes[name] = protoShape(m.Returns)
					}
				}
			}
		}
	}
}

// trackAtResult handles `name = at(listVar, i)` — see sema's identical
// counterpart for the full reasoning; kept independently per this
// project's "sema/codegen never share state" pattern.
func trackAtResult(fg *funcGen, name string, call *ast.CallExpr) {
	if len(call.Args) != 2 {
		return
	}
	listID, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return
	}
	shape, ok := fg.goListShapes[listID.Name]
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
	fg.goStaticVars[name] = shape[i]
}

func protoShape(returns []GoReturnSpec) []string {
	shape := make([]string, len(returns))
	for i, r := range returns {
		shape[i] = r.Proto
	}
	return shape
}

// genGoMethodCall lowers `f.Method(args...)` when f is a statically
// Go-typed variable (trackGoAssetResult) — bypassing the dynamic
// prototype-chain dispatch genMethodCall otherwise uses (weave_spec.md
// §16). The method name itself is resolved here, at compile time, from
// gotype(...)'s own declared table — never looked up dynamically.
//
// Without a declared signature (weave_spec.md §15.1's plain
// `gomethod("Name")`), the call still goes through
// weavert.CallGoMethodList (reflect) exactly as before: f's *generated*
// Go code only ever sees it as `any`, so "static resolution" without a
// signature means the method *name* is known, not that the Go call
// itself can skip reflect. It still always returns a Weave list
// (weave_spec.md §15.2).
//
// With a declared signature (genNativeGoMethodCall), the call becomes
// fully native — ASSERT extracts f's concrete receiver type, METHOD
// extracts the method as a real Go method value, and a native multi-
// target CALL invokes it directly. No reflect anywhere in this path.
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

	if methodInfo.ParamTypes == nil {
		tmp := fg.newTemp("^any")
		callArgs := append([]string{objVal, strconv.Quote(methodInfo.GoName)}, argVals...)
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CallGoMethodList\t%s\n", tmp, strings.Join(callArgs, "\t"))
		return true, tmp, nil
	}

	result, err := genNativeGoMethodCall(fg, typeInfo, methodInfo, objVal, argVals, prop.Line)
	return true, result, err
}

// genNativeGoMethodCall lowers a typed gomethod(...) call to a fully
// native ASSERT+METHOD+CALL sequence — see genGoMethodCall's doc
// comment. Every step down to the final CALL only ever deals in
// concretely-typed AMIVM values (never `^any`), matching amivm's own
// method-call pattern exactly.
//
// METHOD (amivm_spec.md §4.21, `local := variable.method`) replaced an
// earlier FNTYPE+FGET design that turned out to be fundamentally
// incompatible with any slice-shaped parameter/return position: FGET's
// `single1 = variable.field` is a plain assignment into a *pre-declared*
// VAR, which requires Go's own function-type *identity* (not mere
// assignability) between the VAR's FNTYPE-declared type and the real
// method's actual type — and a named stand-in like `^GoBytes` (needed
// because AMIVM's type1 has no literal slice-type-token form at all) is
// never identical to the real unnamed `[]byte`, only assignable to it,
// so the FGET assignment itself failed to compile (confirmed by direct
// probe). METHOD's `:=` sidesteps this entirely: the method value's own
// type is inferred straight from the real method, so there's no
// pre-declared type for it to conflict with. concreteArgs (passed into
// the CALL below) and raws[] (its results' targets) can still freely
// use named stand-in types (goTypeToken) same as always — passing an
// argument and assigning a return value are both governed by
// assignability, never identity, so naming never causes a problem there
// (only *composing a function type*, which METHOD no longer does, ever
// required identity).
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

	methodVal := fg.newUndeclaredTemp()
	fmt.Fprintf(&fg.body, "\tMETHOD\t%s\t%s\t<%s\n", methodVal, recv, methodInfo.GoName)

	raws := make([]string, len(methodInfo.Returns))
	for i, r := range methodInfo.Returns {
		raws[i] = fg.newTemp(goTypeToken(fg, r.Type))
	}
	fmt.Fprintf(&fg.body, "\tCALL\t%s:\t%s%s\n", callTargetsPrefix(raws), methodVal, argSuffix(concreteArgs))
	return genBoxList(fg, raws, methodInfo.Returns), nil
}

// genNativeReturnValue boxes a concretely-typed native call result (raw,
// declared via goTypeToken(fg, goRef) — so already a named stand-in
// type like `^GoBytes`/`^GoSlice_int`, not a bare `^[]byte`/`^[]int`,
// whenever goRef is a slice hint) into a fresh `^any` value, converting
// it first when needed — used by genBoxList for each position of a
// typed gofunc(...)/gomethod(...) call's result list. goRef is the
// original "?pkg.Type"-shaped declaration string (not yet tokenized) —
// both branches below need to recognize the original hint string by
// name, which goTypeToken's own output (the named stand-in type) no
// longer spells out.
//
// A general (non-byte) "?[]T" slice hint is handled first, and
// differently from every other conversion: weavert.XsToList (slices.go)
// already returns `^any` directly, so its CALL result *is* the final
// result — no intermediate concretely-typed "casted" temp the way
// nativeReturnConversion's other cases need (goTypeToken would make no
// sense applied to a callname like "?weavert.IntsToList" anyway, since
// that's not a "?pkg.Type"-shaped string at all).
func genNativeReturnValue(fg *funcGen, raw, goRef string) string {
	result := fg.newTemp("^any")
	if elem, ok := sliceElem(goRef); ok {
		if suffix, supported := sliceElemGoFuncSuffix[elem]; supported {
			fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.%sToList\t%s\n", result, suffix, raw)
			return result
		}
	}
	if conv := nativeReturnConversion(goRef); conv != "" {
		casted := fg.newTemp(goTypeToken(fg, conv))
		fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t%s\t%s\n", casted, conv, raw)
		fmt.Fprintf(&fg.body, "\tSET\t%s\t%s\n", result, casted)
	} else {
		fmt.Fprintf(&fg.body, "\tSET\t%s\t%s\n", result, raw)
	}
	return result
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
//
// "?[]byte" is a special case, handled entirely separately
// (genAssertByteSliceParam): the Weave-side value arriving here is never
// actually a []byte to begin with (Weave has no slice concept at all —
// weave_spec.md §2), so an ordinary ASSERT against `^GoBytes` would
// always fail. See that function's own doc comment. Every other
// supported "?[]T" slice hint has the same problem for the same reason
// (the Weave-side value is always a list Object, never a real []T) —
// see genAssertGeneralSliceParam.
func genAssertOrTypeError(fg *funcGen, anyVal, goRef, desc string) string {
	if goRef == "?[]byte" || goRef == "?[]uint8" {
		return genAssertByteSliceParam(fg, anyVal, desc)
	}
	if elem, ok := sliceElem(goRef); ok {
		if suffix, supported := sliceElemGoFuncSuffix[elem]; supported {
			return genAssertGeneralSliceParam(fg, anyVal, elem, suffix)
		}
	}
	anyVal = ensureVariable(fg, anyVal)
	concreteType := goTypeToken(fg, goRef)
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

// genAssertByteSliceParam handles a declared "?[]byte" parameter/receiver
// type (weave_spec.md §15.4): every "?[]byte" *return* value is already
// converted to a Weave string on the way out (nativeReturnConversion,
// mirroring weavert.NormalizeGoValue's identical untyped-path behavior),
// so a Weave value destined for a []byte-typed Go parameter is always
// actually a string underneath, not a []byte — asserting it directly as
// `^GoBytes` would therefore always fail (the dynamic type genuinely is
// string). Reversing that conversion takes two steps instead of
// genAssertOrTypeError's usual one: ASSERT the value down to the
// `^string` it actually is (an ordinary TypeError if it isn't — same
// clear-error guarantee as any other declared type), then a native Go
// conversion (`GoBytes(s)`, valid because GoBytes' underlying type is
// []byte — same direction as string-to-[]byte conversions generally)
// produces the concrete byte-slice-shaped value the real Go call needs.
func genAssertByteSliceParam(fg *funcGen, anyVal, desc string) string {
	strVal := genAssertOrTypeError(fg, anyVal, "?string", desc)
	bytesType := fg.ctx.goBytesType()
	casted := fg.newTemp(bytesType)
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?%s\t%s\n", casted, strings.TrimPrefix(bytesType, "^"), strVal)
	return casted
}

// genAssertGeneralSliceParam handles a declared "?[]T" parameter type
// for T other than byte/uint8 (weave_spec.md §15.4) — the general
// counterpart to genAssertByteSliceParam. Unlike that one-step native
// cast, converting a Weave list into an arbitrary []T needs real
// per-element validation (any element could be the wrong Weave type),
// which isn't a single native CALL/CAST the way string<->[]byte is —
// weavert.ListToXs (one per supported T, slices.go) does this
// validation loop in ordinary Go (a type assertion per element,
// panicking with a clear message on the first mismatch — no reflect).
// Its return value is already the concrete, unnamed []T the real Go
// function needs; the result here is declared with goTypeToken's own
// named `^GoSlice_T` stand-in only because AMIVM has no literal slice
// type token to declare the VAR with — assigning an unnamed []T into
// it, and later passing it as an argument, both go through Go's
// ordinary assignability rules (not a type switch), so the naming
// doesn't matter here the way it does for weavert.XsToList's own
// return-side callers (goTypeToken's own doc comment). anyVal is passed
// to ListToXs as-is (no ensureVariable needed): unlike ASSERT's
// `variable`-only operand category, a plain CALL argument accepts a
// literal value token directly.
func genAssertGeneralSliceParam(fg *funcGen, anyVal, elem, suffix string) string {
	sliceType := fg.ctx.goSliceType(elem)
	casted := fg.newTemp(sliceType)
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.ListTo%s\t%s\n", casted, suffix, anyVal)
	return casted
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

// sliceElemGoFuncSuffix maps a supported "?[]T" element type name (T,
// with the "?[]" prefix already stripped) to the Go-identifier-safe
// suffix used to name its dedicated weavert conversion functions —
// "int" -> "Ints" gives weavert.IntsToList (return side) and
// weavert.ListToInts (parameter side). byte/uint8 are deliberately
// absent: that element type keeps its own, older →string conversion
// (goTypeToken/nativeReturnConversion's own "?[]byte" special case)
// rather than becoming a list of numbers, since Weave's only textual
// value is already a string (weave_spec.md §2) and NormalizeGoValue's
// untyped path has always made that same choice. Every other slice
// element type Weave supports a hint for is listed here — sema's own
// copy (internal/sema/goasset.go's validateSliceHint) must be kept in
// sync manually, matching this project's established pattern for every
// other sema/codegen table (builtinNames/reservedName/numericGoTypeNames).
var sliceElemGoFuncSuffix = map[string]string{
	"int":     "Ints",
	"int8":    "Int8s",
	"int16":   "Int16s",
	"int32":   "Int32s",
	"int64":   "Int64s",
	"uint":    "Uints",
	"uint16":  "Uint16s",
	"uint32":  "Uint32s",
	"uint64":  "Uint64s",
	"float32": "Float32s",
	"float64": "Float64s",
	"string":  "Strings",
	"bool":    "Bools",
}

// sliceElem extracts T from a "?[]T"-shaped type hint string, reporting
// false for anything else (a plain scalar hint, a pointer/struct hint,
// ...).
func sliceElem(goRef string) (string, bool) {
	if !strings.HasPrefix(goRef, "?[]") {
		return "", false
	}
	return strings.TrimPrefix(goRef, "?[]"), true
}

// goTypeToken converts a "?pkg.Type"/"?*pkg.Type"-shaped declaration
// string (gotype/gomethod/gofunc's own argument shape, weave_spec.md
// §15.1) into the `^`-prefixed AMIVM type token it names — the two
// prefixes line up with amivm's own type category (amivm_spec.md §7)
// one swap away: `?strings.Reader` -> `^strings.Reader`, `?*os.File` ->
// `^*os.File`.
//
// "?[]byte" and every other supported "?[]T" slice type hint
// (weave_spec.md §15.4) are a special case: naively applying the same
// swap would produce an unnamed slice literal like `^[]byte`/`^[]int`,
// but AMIVM's `type1` operand category has no slice-literal form at
// all — only named types, declared ahead of time via SLTYPE (confirmed
// by direct probe: amivm rejects a bare `^[]byte` type operand
// outright). So instead this returns one shared named type per element
// type — `^GoBytes` (codegenCtx.goBytesType) for byte/uint8, or
// `^GoSlice_T` (codegenCtx.goSliceType) for every other supported T,
// each backed by its own single top-level `SLTYPE` declaration — Go's
// assignability rules make a named type with underlying type []T
// interchangeable with a literal []T at every boundary that matters
// here (an unnamed []T return value assigns straight into a
// `^GoSlice_T`-typed CALL target, and a `^GoSlice_T`-typed argument
// passes straight into a Go parameter declared as plain `[]T`,
// confirmed by direct probe both ways) — so nothing downstream of this
// function needs to know the substitution happened. This is also why
// weavert's own []T<->list conversion functions (slices.go's
// IntsToList/ListToInts and friends) are written to take/return a
// *concretely typed* []T parameter rather than `any`: passing a
// `^GoSlice_int`-typed argument into a `func([]int) any` parameter is
// just ordinary assignability (naming doesn't matter), whereas a Go
// type *switch* (`case []int:`) would require the value's *exact*
// dynamic type and would never match a named stand-in — see slices.go's
// own doc comment for the full reasoning (the same distinction that
// motivated introducing amivm's own METHOD instruction, one level up,
// for method-value extraction — genNativeGoMethodCall's own doc
// comment).
func goTypeToken(fg *funcGen, goRef string) string {
	if goRef == "?[]byte" || goRef == "?[]uint8" {
		return fg.ctx.goBytesType()
	}
	if elem, ok := sliceElem(goRef); ok {
		if _, supported := sliceElemGoFuncSuffix[elem]; supported {
			return fg.ctx.goSliceType(elem)
		}
	}
	return "^" + strings.TrimPrefix(goRef, "?")
}

// numericGoTypeNames mirrors weavert.NormalizeGoValue's own switch
// (weavert/goasset.go) — every Go numeric kind that needs boxing as
// float64 to match weave_spec.md §2's "numbers are always float64"
// rule. float64 itself is deliberately absent: it's already the target
// representation, so no cast is needed for it. "byte"/"rune" are included
// even though NormalizeGoValue's own type-switch doesn't name them
// separately (they're identical types to uint8/int32, so Go's switch
// already matches them there) — but a declared gomethod/gofunc return
// type is a literal type token this file emits verbatim into an AMIVM
// type operand, so it needs its own explicit lookup entry here.
var numericGoTypeNames = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "byte": true, "rune": true,
}

// nativeReturnConversion reports the CALL-cast callname (e.g. "?float64")
// needed to box a native call's return value (goRef, the original
// "?pkg.Type"-shaped declaration string — not yet run through
// goTypeToken, since "?[]byte" needs to be recognized here by name
// before it becomes the unrelated-looking `^GoBytes` token) into the
// representation Weave code expects, or "" when the raw value can be
// boxed into `^any` as-is. Mirrors weavert.NormalizeGoValue's own switch
// (weavert/goasset.go, the untyped/reflect path's equivalent step) but
// has to happen as an explicit native CALL cast here since there's no
// reflect available on this path at all — see genNativeReturnValue.
// "?[]byte" converts to `?string` (Go's own `string(b)` conversion,
// valid directly on a `^GoBytes`-typed value too — goTypeToken's own doc
// comment explains why the named/unnamed distinction doesn't matter
// here), exactly matching NormalizeGoValue's identical choice on the
// untyped path.
func nativeReturnConversion(goRef string) string {
	bare := strings.TrimPrefix(goRef, "?")
	if numericGoTypeNames[bare] {
		return "?float64"
	}
	if bare == "[]byte" {
		return "?string"
	}
	return ""
}

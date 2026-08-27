package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// GoTypeInfo/GoMethodInfo/GoFuncInfo mirror sema's own
// (internal/sema/goasset.go) — a separate copy per the established
// pattern of codegen re-deriving its own view of the AST rather than
// consuming sema's internal state (see CLAUDE.md's "確定した設計判断" on
// manually-synced tables, e.g. builtinNames/reservedName). See sema's
// copies for the full reasoning behind the "every Go call always
// returns a list, dispatched via reflection" design and the Proto
// field's meaning.
type GoTypeInfo struct {
	GoName  string
	Methods map[string]*GoMethodInfo
}

type GoMethodInfo struct {
	GoName string
	Proto  string
}

type GoFuncInfo struct {
	GoName string
	Proto  string
}

// GoVarInfo mirrors sema's own (internal/sema/goasset.go's GoVarInfo).
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
// (weave_spec.md §15.4) already validated by sema (checkGoVarDecl) —
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
// §15.4) to a single native CALL: info.GoName ("?pkg.Var") is already a
// valid AMIVM `value` operand — the exact same "a bare ?pkg.xxx token is
// just a raw Go expression, embeddable as a first-class value" trick
// genGoFuncCall's own doc comment established for passing a *function*
// value into weavert.CallGoFuncList, except here the raw expression IS
// the whole read: no reflect is needed at all (there's no method-name
// string to resolve dynamically the way CallGoMethodList needs — the
// identifier itself is fully static). Every reference re-emits its own
// CALL rather than caching a result anywhere (weave_spec.md §15.4's
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
	return &GoMethodInfo{GoName: goName, Proto: args[1].(*ast.Ident).Name}
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
	fg.ctx.goFuncs[name] = &GoFuncInfo{GoName: goName, Proto: call.Args[1].(*ast.Ident).Name}
}

// genGoFuncCall lowers a call to a gofunc(...)-declared name via
// weavert.CallGoFuncList — see that function's own doc comment in
// weavert/goasset.go for why reflect is unavoidable (an arbitrary `any`
// argument can't be passed to a specific concrete Go parameter type
// without either a type assertion or reflect.Convert). Always returns a
// Weave list of every actual Go return value (weave_spec.md §15.2's
// "常にlist" rule) — info.GoName ("?pkg.Func") is itself a valid AMIVM
// `value` operand (a raw Go function reference), passed straight through
// as CallGoFuncList's first argument.
func genGoFuncCall(fg *funcGen, info *GoFuncInfo, call *ast.CallExpr) (string, error) {
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

// argSuffix formats zero or more trailing operands for a CALL line — ""
// when empty (so a niladic call's line has no dangling tab), else a
// leading tab followed by each operand, tab-separated.
func argSuffix(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return "\t" + strings.Join(args, "\t")
}

// trackGoAssetResult mirrors sema's own tracking (internal/sema/goasset.go's
// trackGoAssetResult — see its doc comment for the full reasoning): name
// is statically Go-typed (fg.goStaticVars) when value is `at(listVar, 0)`
// extracting a proto-bound list's position 0, or holds a tracked
// proto-bound list itself (fg.goListProto) when value is a direct call
// to a proto-bound gofunc(...)/gomethod(...). Any other assignment
// clears both.
func trackGoAssetResult(fg *funcGen, name string, value ast.Expr) {
	if fg.goStaticVars == nil {
		fg.goStaticVars = map[string]string{}
	}
	if fg.goListProto == nil {
		fg.goListProto = map[string]string{}
	}
	delete(fg.goStaticVars, name)
	delete(fg.goListProto, name)

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
		if info := fg.ctx.goFuncs[callee.Name]; info != nil && info.Proto != "" {
			fg.goListProto[name] = info.Proto
		}
	case *ast.PropExpr:
		if id, ok := callee.Obj.(*ast.Ident); ok {
			if typeName, ok := fg.goStaticVars[id.Name]; ok {
				if typeInfo := fg.ctx.goTypes[typeName]; typeInfo != nil {
					if m := typeInfo.Methods[callee.Prop]; m != nil && m.Proto != "" {
						fg.goListProto[name] = m.Proto
					}
				}
			}
		}
	}
}

// trackAtResult handles `name = at(listVar, 0)` — see sema's identical
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
	proto, ok := fg.goListProto[listID.Name]
	if !ok {
		return
	}
	idx, ok := call.Args[1].(*ast.NumberLit)
	if !ok || idx.Value != 0 {
		return
	}
	fg.goStaticVars[name] = proto
}

// genGoMethodCall lowers `f.Method(args...)` when f is a statically
// Go-typed variable (trackGoAssetResult) — bypassing the dynamic
// prototype-chain dispatch genMethodCall otherwise uses (weave_spec.md
// §16). The method name itself is resolved here, at compile time, from
// gotype(...)'s own declared table (never looked up dynamically), but
// the Go call itself always goes through weavert.CallGoMethodList
// (reflect): f's *generated* Go code only ever sees it as `any`, so
// "static resolution" here means the method *name* is known at compile
// time, not that the Go call itself can skip reflect. Always returns a
// Weave list (weave_spec.md §15.2).
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
	methodInfo := fg.ctx.goTypes[typeName].Methods[prop.Prop]

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

	tmp := fg.newTemp("^any")
	callArgs := append([]string{objVal, strconv.Quote(methodInfo.GoName)}, argVals...)
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CallGoMethodList\t%s\n", tmp, strings.Join(callArgs, "\t"))
	return true, tmp, nil
}

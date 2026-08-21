package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amisonnet8/weave/internal/ast"
)

// GoTypeInfo/GoFuncInfo mirror sema's own (internal/sema/goasset.go) —
// a separate copy per the established pattern of codegen re-deriving
// its own view of the AST rather than consuming sema's internal state
// (see CLAUDE.md's "確定した設計判断" on manually-synced tables, e.g.
// builtinNames/reservedName).
type GoTypeInfo struct {
	GoName  string
	Methods map[string]string
}

type GoFuncInfo struct {
	GoName string
	Proto  string
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
	methods := map[string]string{}
	for _, field := range members.Fields {
		mcall := field.Value.(*ast.CallExpr)
		methods[field.Name] = mcall.Args[0].(*ast.StringLit).Value
	}
	if fg.ctx.goTypes == nil {
		fg.ctx.goTypes = map[string]*GoTypeInfo{}
	}
	fg.ctx.goTypes[name] = &GoTypeInfo{GoName: goName, Methods: methods}
}

func genGoFuncDecl(fg *funcGen, name string, call *ast.CallExpr) {
	goName := call.Args[0].(*ast.StringLit).Value
	proto := ""
	if id, ok := call.Args[1].(*ast.Ident); ok {
		proto = id.Name
	}
	if fg.ctx.goFuncs == nil {
		fg.ctx.goFuncs = map[string]*GoFuncInfo{}
	}
	fg.ctx.goFuncs[name] = &GoFuncInfo{GoName: goName, Proto: proto}
}

// genGoFuncCall lowers a call to a gofunc(...)-declared name directly to
// its real Go function, bypassing weavert.Call's dynamic dispatch
// entirely (weave_spec.md §16: "動的なプロトタイプ検索を一切経由せず
// ...ネイティブ命令の速さ・単純さ" for gofunc/gomethod). Arguments are
// passed positionally, matching the wrapped Go function's own native
// arity — not curried the way an ordinary Weave call is, since the
// whole point of gofunc is to mirror the real Go signature rather than
// force it through Weave's 1-argument convention.
//
// Scope limitation: this assumes the wrapped Go function returns
// exactly one value. weave_spec.md never addresses multi-value Go
// returns (§17's own os.Open example actually returns (*File, error) in
// real Go, which this can't express) — deferred to when Step 5's
// integration sample actually needs it; see CLAUDE.md's 後半 Step 3
// "確定した設計判断".
//
// The raw result is routed through weavert.NormalizeGoValue — a Go
// asset function can return a native numeric type (int, uint32, ...),
// which weave_spec.md §15.2 says should just work as an ordinary Weave
// number; without normalizing it to float64 (Weave's own unified
// number representation, CLAUDE.md's Step 2 "確定した設計判断"), it
// would silently break the first Weave-native operation it touched
// (caught by CallGoMethod's own doc comment — same underlying issue,
// discovered via examples/gomethods.weave).
func genGoFuncCall(fg *funcGen, info *GoFuncInfo, call *ast.CallExpr) (string, error) {
	var argVals []string
	for _, arg := range call.Args {
		v, err := genExpr(fg, arg)
		if err != nil {
			return "", err
		}
		argVals = append(argVals, v)
	}
	raw := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t%s%s\n", raw, info.GoName, argSuffix(argVals))
	tmp := fg.newTemp("^any")
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.NormalizeGoValue\t%s\n", tmp, raw)
	return tmp, nil
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

// genGoMethodCall lowers `f.Method(args...)` directly to
// weavert.CallGoMethod when f is a statically Go-typed variable
// (trackGoStaticVar) — bypassing the dynamic prototype-chain dispatch
// genMethodCall otherwise uses (weave_spec.md §16: "gomethodで宣言した
// メソッド呼び出しも...動的なプロトタイプ検索を経由せず"). The method
// name itself is resolved here, at compile time, from gotype(...)'s own
// declared table — never looked up dynamically. Reflection is still
// needed at the actual call site despite that: f's *generated* Go code
// only ever sees it as `any` (the same reason every other Go-asset/
// closure/actor value routes through weavert — see weavert/goasset.go's
// CallGoMethod doc comment for the full reasoning), so "static
// resolution" here means the method *name*, not the Go call itself.
//
// The bool result tells genGeneralCall whether this applied at all —
// prop.Obj not being a tracked static variable is the ordinary case
// (an ordinary Weave object), not an error.
func genGoMethodCall(fg *funcGen, prop *ast.PropExpr, args []ast.Expr) (bool, string, error) {
	id, ok := prop.Obj.(*ast.Ident)
	if !ok {
		return false, "", nil
	}
	typeName, ok := fg.goStaticVars[id.Name]
	if !ok {
		return false, "", nil
	}
	info := fg.ctx.goTypes[typeName]
	goMethodName := info.Methods[prop.Prop]

	objVal, err := genExpr(fg, prop.Obj)
	if err != nil {
		return true, "", err
	}
	var argVals []string
	for _, arg := range args {
		v, err := genExpr(fg, arg)
		if err != nil {
			return true, "", err
		}
		argVals = append(argVals, v)
	}

	tmp := fg.newTemp("^any")
	callArgs := append([]string{objVal, strconv.Quote(goMethodName)}, argVals...)
	fmt.Fprintf(&fg.body, "\tCALL\t%s\t:\t?weavert.CallGoMethod\t%s\n", tmp, strings.Join(callArgs, "\t"))
	return true, tmp, nil
}
